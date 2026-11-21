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
	sealed, err := value.Verified()
	if err != nil {
		return nil, err
	}
	if queue == 0 || mark == 0 || loopMark == 0 || loopMark&(loopMark-1) != 0 || mark&loopMark != 0 ||
		!path.IsAbs(engineRoot) || path.Clean(engineRoot) != engineRoot || len(engineRoot) > 512 || strings.ContainsAny(engineRoot, "\x00\r\n") {
		return nil, errors.New("invalid Zapret engine root or Network Planner allocation")
	}
	if sealed.NeedsReturn {
		return nil, errors.New("Zapret return path is not qualified")
	}
	args := []string{fmt.Sprintf("--qnum=%d", queue), fmt.Sprintf("--fwmark=0x%x", loopMark)}
	if len(sealed.Assets) != 0 {
		args = append(args, "--lua-init=@"+path.Join(engineRoot, "lua/zapret-lib.lua"),
			"--lua-init=@"+path.Join(engineRoot, "lua/zapret-antidpi.lua"))
	}
	for i, profile := range sealed.Profiles {
		if i > 0 {
			args = append(args, "--new="+profile.ID)
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
	return args, nil
}

func payloadProtocol(value Payload) string {
	if value == TLSClientHello {
		return "tls"
	}
	return "http"
}
