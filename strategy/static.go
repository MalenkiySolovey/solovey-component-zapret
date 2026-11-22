// Package strategy owns the static, ordered native Zapret2 profile model.
package strategy

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-host/outbounds"
)

const Schema = "solovey.zapret-strategy/v1"

type AssetRef string

const (
	Library AssetRef = "upstream:zapret-lib"
	Actions AssetRef = "upstream:zapret-antidpi"
)

type Payload string

const (
	TLSClientHello Payload = "tls_client_hello"
	HTTPRequest    Payload = "http_req"
)

// An empty SplitAt gives a passive native profile. A nonzero SplitAt selects
// the restricted upstream TCP segmentation operation for one known payload.
type Profile struct {
	ID       string                `json:"id"`
	Protocol intent.PacketProtocol `json:"protocol"`
	Ports    []uint16              `json:"ports"`
	Payload  Payload               `json:"payload,omitempty"`
	SplitAt  uint16                `json:"splitAt,omitempty"`
}

type Strategy struct {
	Schema      string     `json:"schema"`
	ID          string     `json:"id"`
	Revision    string     `json:"revision"`
	NeedsReturn bool       `json:"needsReturn,omitempty"`
	Assets      []AssetRef `json:"assets,omitempty"`
	Profiles    []Profile  `json:"profiles"` // selection order is significant
}

type Scope struct {
	TCP, UDP    []uint16
	NeedsReturn bool
}

var ErrInvalidStrategy = errors.New("invalid static Zapret strategy")

// Seal validates and returns a new content-revisioned Strategy. It copies
// caller-owned slices and normalizes sets without reordering profiles or Lua.
func Seal(value Strategy) (Strategy, error) {
	if value.Schema != Schema || !outbounds.ValidZapretStrategyRef(value.ID) || len(value.Profiles) == 0 || len(value.Profiles) > 16 ||
		(len(value.Assets) != 0 && !slices.Equal(value.Assets, []AssetRef{Library, Actions})) {
		return Strategy{}, ErrInvalidStrategy
	}
	value.Assets = slices.Clone(value.Assets)
	value.Profiles = slices.Clone(value.Profiles)
	seen := map[string]bool{}
	for i, profile := range value.Profiles {
		if !outbounds.ValidZapretStrategyRef(profile.ID) || seen[profile.ID] || len(profile.Ports) == 0 || len(profile.Ports) > 32 ||
			(profile.Protocol != intent.PacketProtocolTCP && profile.Protocol != intent.PacketProtocolUDP) {
			return Strategy{}, ErrInvalidStrategy
		}
		seen[profile.ID] = true
		profile.Ports = slices.Clone(profile.Ports)
		slices.Sort(profile.Ports)
		if profile.Ports[0] == 0 || len(slices.Compact(slices.Clone(profile.Ports))) != len(profile.Ports) {
			return Strategy{}, ErrInvalidStrategy
		}
		if profile.SplitAt == 0 && profile.Payload != "" || profile.SplitAt != 0 &&
			(profile.Protocol != intent.PacketProtocolTCP || profile.SplitAt > 128 ||
				(profile.Payload != TLSClientHello && profile.Payload != HTTPRequest) || len(value.Assets) != 2) {
			return Strategy{}, ErrInvalidStrategy
		}
		value.Profiles[i] = profile
	}
	scope := value.scope()
	if len(scope.TCP) > 64 || len(scope.UDP) > 64 {
		return Strategy{}, ErrInvalidStrategy
	}
	value.Revision = ""
	data, err := json.Marshal(value)
	if err != nil {
		return Strategy{}, ErrInvalidStrategy
	}
	value.Revision = fmt.Sprintf("%x", sha256.Sum256(data))
	return value, nil
}

func (value Strategy) Verified() (Strategy, error) {
	sealed, err := Seal(value)
	if err != nil || sealed.Revision != value.Revision {
		return Strategy{}, ErrInvalidStrategy
	}
	return sealed, nil
}

func (value Strategy) PacketScope() (Scope, error) {
	sealed, err := value.Verified()
	if err != nil {
		return Scope{}, err
	}
	return sealed.scope(), nil
}

func (value Strategy) scope() Scope {
	result := Scope{NeedsReturn: value.NeedsReturn}
	for _, profile := range value.Profiles {
		if profile.Protocol == intent.PacketProtocolTCP {
			result.TCP = append(result.TCP, profile.Ports...)
		} else {
			result.UDP = append(result.UDP, profile.Ports...)
		}
	}
	slices.Sort(result.TCP)
	slices.Sort(result.UDP)
	result.TCP = slices.Compact(result.TCP)
	result.UDP = slices.Compact(result.UDP)
	return result
}

// Compile returns argv, not a shell command or an execution grant. Queue and
// marks are output of the Network Planner, never editable Strategy values.
func (value Strategy) Compile(engineRoot string, queue uint16, mark, loopMark uint32) ([]string, error) {
	return CompileBindings(engineRoot, queue, loopMark, []MarkedStrategy{{Strategy: value, QueueMark: mark}})
}

// MarkedStrategy pairs a strategy revision with its exact, planner-owned
// NFQUEUE input mark. A strategy may be compiled for distinct marks.
type MarkedStrategy struct {
	Strategy  Strategy
	QueueMark uint32
}

// CompileBindings compiles one listener into disjoint native profile groups.
// Global Lua setup is performed once; each profile filters its group's mark.
func CompileBindings(engineRoot string, queue uint16, loopMark uint32, bindings []MarkedStrategy) ([]string, error) {
	if queue == 0 || loopMark == 0 || loopMark&(loopMark-1) != 0 || len(bindings) == 0 || len(bindings) > 3 ||
		!path.IsAbs(engineRoot) || path.Clean(engineRoot) != engineRoot || len(engineRoot) > 512 || strings.ContainsAny(engineRoot, "\x00\r\n") {
		return nil, errors.New("invalid Zapret engine root or Network Planner allocation")
	}
	verified := make([]Strategy, len(bindings))
	seen := make(map[uint32]bool, len(bindings))
	needsLua := false
	for i, item := range bindings {
		if item.QueueMark == 0 || item.QueueMark&loopMark != 0 || seen[item.QueueMark] {
			return nil, errors.New("invalid or repeated Zapret queue mark")
		}
		seen[item.QueueMark] = true
		sealed, err := item.Strategy.Verified()
		if err != nil {
			return nil, err
		}
		if sealed.NeedsReturn {
			return nil, errors.New("Zapret return path is not qualified")
		}
		verified[i] = sealed
		needsLua = needsLua || len(sealed.Assets) != 0
	}
	args := []string{fmt.Sprintf("--qnum=%d", queue), fmt.Sprintf("--fwmark=0x%x", loopMark)}
	if needsLua {
		args = append(args, "--lua-init=@"+path.Join(engineRoot, "lua/zapret-lib.lua"),
			"--lua-init=@"+path.Join(engineRoot, "lua/zapret-antidpi.lua"))
	}
	for i, sealed := range verified {
		args = appendProfiles(args, sealed.Profiles, bindings[i].QueueMark, i, len(bindings) > 1)
	}
	return args, nil
}

func appendProfiles(args []string, profiles []Profile, mark uint32, binding int, groupNames bool) []string {
	for i, profile := range profiles {
		name := profile.ID
		if groupNames {
			name = fmt.Sprintf("b%d-p%d", binding+1, i+1)
		}
		if i > 0 || binding > 0 {
			args = append(args, "--new="+name)
		} else if groupNames {
			args = append(args, "--name="+name)
		}
		ports := make([]string, 0, len(profile.Ports))
		for _, port := range profile.Ports {
			ports = append(ports, strconv.Itoa(int(port)))
		}
		args = append(args, fmt.Sprintf("--filter-mark=0x%x/0xffffffff", mark),
			"--filter-"+string(profile.Protocol)+"="+strings.Join(ports, ","))
		if profile.SplitAt != 0 {
			args = append(args, "--filter-l7="+payloadProtocol(profile.Payload), "--payload="+string(profile.Payload),
				fmt.Sprintf("--lua-desync=multisplit:pos=%d", profile.SplitAt))
		}
	}
	return args
}

func payloadProtocol(value Payload) string {
	if value == TLSClientHello {
		return "tls"
	}
	return "http"
}
