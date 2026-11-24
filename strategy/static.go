// Package strategy owns the static, ordered native Zapret2 profile model.
package strategy

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	Library         AssetRef = "upstream:zapret-lib"
	Actions         AssetRef = "upstream:zapret-antidpi"
	DefaultQUICBlob AssetRef = "upstream:fake-default-quic"
)

const defaultQUICBlobName = "fake_default_quic"

const maxProfiles = 16

type Payload string

type AddressFamily string

const (
	IPv4 AddressFamily = "ipv4"
	IPv6 AddressFamily = "ipv6"
)

const (
	TLSClientHello Payload = "tls_client_hello"
	HTTPRequest    Payload = "http_req"
	QUICInitial    Payload = "quic_initial"
)

// A profile can observe packets without changing them, split a known TCP
// payload, or emit a bounded QUIC decoy alongside the original.
type Profile struct {
	ID          string                `json:"id"`
	Protocol    intent.PacketProtocol `json:"protocol"`
	Ports       []uint16              `json:"ports"`
	Payload     Payload               `json:"payload,omitempty"`
	SplitAt     uint16                `json:"splitAt,omitempty"`
	QUICRepeats uint8                 `json:"quicRepeats,omitempty"`
	QUICBlobRef string                `json:"quicBlobRef,omitempty"`
	Family      AddressFamily         `json:"family,omitempty"` // Empty matches either admitted family.
	Lists       *NativeLists          `json:"lists,omitempty"`
}

type Strategy struct {
	Schema      string            `json:"schema"`
	ID          string            `json:"id"`
	Revision    string            `json:"revision"`
	NeedsReturn bool              `json:"needsReturn,omitempty"`
	Assets      []AssetRef        `json:"assets,omitempty"`
	Profiles    []Profile         `json:"profiles"` // selection order is significant
	BlobData    map[string]string `json:"blobData,omitempty"`
}

type Scope struct {
	TCP, UDP    []uint16
	NeedsReturn bool
}

var ErrInvalidStrategy = errors.New("invalid static Zapret strategy")

var errReturnObservationUnavailable = errors.New("Zapret return-path observation is not supported by the installed profile")

// Seal validates and returns a new content-revisioned Strategy. It copies
// caller-owned slices and normalizes sets without reordering profiles or Lua.
func Seal(value Strategy) (Strategy, error) {
	if value.Schema != Schema || !outbounds.ValidZapretStrategyRef(value.ID) || len(value.Profiles) == 0 || len(value.Profiles) > maxProfiles ||
		(len(value.Assets) != 0 && !slices.Equal(value.Assets, []AssetRef{Library, Actions})) {
		return Strategy{}, ErrInvalidStrategy
	}
	value.Assets = slices.Clone(value.Assets)
	value.Profiles = slices.Clone(value.Profiles)
	value.BlobData = maps.Clone(value.BlobData)
	seen := map[string]bool{}
	listBytes := maxNativeListBytes
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
		if profile.Family != "" && profile.Family != IPv4 && profile.Family != IPv6 ||
			!profile.validAction() || profile.hasAction() && len(value.Assets) != 2 {
			return Strategy{}, ErrInvalidStrategy
		}
		if profile.Lists != nil {
			lists, err := profile.Lists.seal(&listBytes)
			if err != nil {
				return Strategy{}, fmt.Errorf("%w: invalid native lists in profile %q", err, profile.ID)
			}
			profile.Lists = lists
		}
		value.Profiles[i] = profile
	}
	if err := value.validateBlobs(); err != nil {
		return Strategy{}, err
	}
	scope := value.scope("")
	if len(scope.TCP) > 64 || len(scope.UDP) > 64 {
		return Strategy{}, ErrInvalidStrategy
	}
	value.Revision = ""
	data, err := json.Marshal(value)
	if err != nil {
		return Strategy{}, ErrInvalidStrategy
	}
	if len(value.BlobData) != 0 && len(data)+64 > maxBlobDefinitionBytes {
		return Strategy{}, fmt.Errorf("%w: native blob strategy exceeds the runtime definition budget", ErrInvalidStrategy)
	}
	value.Revision = fmt.Sprintf("%x", sha256.Sum256(data))
	return value, nil
}

func (p Profile) hasAction() bool { return p.SplitAt != 0 || p.QUICRepeats != 0 }

func (p Profile) validAction() bool {
	if p.QUICBlobRef != "" && (p.QUICRepeats == 0 || !outbounds.ValidZapretStrategyRef(p.QUICBlobRef)) {
		return false
	}
	switch {
	case p.SplitAt != 0:
		return p.Protocol == intent.PacketProtocolTCP && p.SplitAt <= 128 && p.QUICRepeats == 0 &&
			(p.Payload == TLSClientHello || p.Payload == HTTPRequest)
	case p.QUICRepeats != 0:
		return p.Protocol == intent.PacketProtocolUDP && p.QUICRepeats <= 4 && p.Payload == QUICInitial
	default:
		return p.Payload == ""
	}
}

func (value Strategy) Verified() (Strategy, error) {
	sealed, err := Seal(value)
	if err != nil || sealed.Revision != value.Revision {
		return Strategy{}, ErrInvalidStrategy
	}
	return sealed, nil
}

// BlobRefs reports the pinned built-in data used by the native actions.
func (value Strategy) BlobRefs() []AssetRef {
	for _, p := range value.Profiles {
		if p.QUICRepeats != 0 && p.QUICBlobRef == "" {
			return []AssetRef{DefaultQUICBlob}
		}
	}
	return nil
}

func (value Strategy) PacketScope() (Scope, error) {
	return value.PacketScopeForFamily("")
}

// PacketScopeForFamily translates ordered profiles into the exact port sets
// needed by the Planner. It does not add ports from the other family.
func (value Strategy) PacketScopeForFamily(family AddressFamily) (Scope, error) {
	if family != "" && family != IPv4 && family != IPv6 {
		return Scope{}, ErrInvalidStrategy
	}
	sealed, err := value.Verified()
	if err != nil {
		return Scope{}, err
	}
	return sealed.scope(family), nil
}

func (value Strategy) scope(family AddressFamily) Scope {
	result := Scope{NeedsReturn: value.NeedsReturn}
	for _, profile := range value.Profiles {
		if family != "" && profile.Family != "" && profile.Family != family {
			continue
		}
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

// HandlesQUIC requires coverage independent of destination list matches. A
// conditional action may fall through, and a passive profile may shadow it.
func (value Strategy) HandlesQUIC(family AddressFamily, port uint16) bool {
	for _, p := range value.Profiles {
		if p.Protocol == intent.PacketProtocolUDP && (p.Family == "" || p.Family == family) && slices.Contains(p.Ports, port) {
			if p.QUICRepeats == 0 || p.QUICBlobRef != "" {
				return false
			}
			if p.Lists == nil {
				return true
			}
		}
	}
	return false
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
	args, _, err := compileBindings(engineRoot, queue, loopMark, bindings, false)
	return args, err
}

// CompileBindingsWithNativeTrace emits one best-effort observation per matched
// native profile. Connect the fresh trace to this engine's private stdout only.
func CompileBindingsWithNativeTrace(engineRoot string, queue uint16, loopMark uint32, bindings []MarkedStrategy) ([]string, *NativeTrace, error) {
	return compileBindings(engineRoot, queue, loopMark, bindings, true)
}

func compileBindings(engineRoot string, queue uint16, loopMark uint32, bindings []MarkedStrategy, trace bool) ([]string, *NativeTrace, error) {
	if queue == 0 || loopMark == 0 || loopMark&(loopMark-1) != 0 || len(bindings) == 0 ||
		!path.IsAbs(engineRoot) || path.Clean(engineRoot) != engineRoot || len(engineRoot) > 512 || strings.ContainsAny(engineRoot, "\x00\r\n") {
		return nil, nil, errors.New("invalid Zapret engine root or Network Planner allocation")
	}
	if len(bindings) > engineArgumentBudget/minimumEngineProfileBytes {
		return nil, nil, argumentBudgetError((engineArgumentBudget/minimumEngineProfileBytes + 1) * minimumEngineProfileBytes)
	}
	verified := make([]Strategy, len(bindings))
	seen := make(map[uint32]bool, len(bindings))
	needsLua := false
	minimum := 0
	for i, item := range bindings {
		if item.QueueMark == 0 || item.QueueMark&loopMark != 0 || seen[item.QueueMark] {
			return nil, nil, errors.New("invalid or repeated Zapret queue mark")
		}
		seen[item.QueueMark] = true
		sealed, err := item.Strategy.Verified()
		if err != nil {
			return nil, nil, err
		}
		if sealed.NeedsReturn {
			return nil, nil, errReturnObservationUnavailable
		}
		minimum += len(sealed.Profiles) * minimumEngineProfileBytes
		if minimum > engineArgumentBudget {
			return nil, nil, argumentBudgetError(minimum)
		}
		verified[i] = sealed
		needsLua = needsLua || len(sealed.Assets) != 0
	}
	args := []string{fmt.Sprintf("--qnum=%d", queue), fmt.Sprintf("--fwmark=0x%x", loopMark)}
	if needsLua {
		args = append(args, "--lua-init=@"+path.Join(engineRoot, "lua/zapret-lib.lua"),
			"--lua-init=@"+path.Join(engineRoot, "lua/zapret-antidpi.lua"))
	}
	if trace {
		args = append(args, "--lua-init="+nativeTraceLua)
	}
	used := 0
	if err := chargeEngineArgs(&used, args); err != nil {
		return nil, nil, err
	}
	// Binary data is serialized as hex only. Engine-global Lua variables use
	// generated names, never catalog IDs or names supplied by the raw editor.
	names := map[string]string{}
	for _, value := range verified {
		for _, p := range value.Profiles {
			data := value.BlobData[p.QUICBlobRef]
			if data != "" && names[data] == "" {
				name := fmt.Sprintf("solovey_blob_%d", len(names)+1)
				names[data] = name
				args = append(args, "--blob="+name+":0x"+data)
				if err := chargeEngineArgs(&used, args[len(args)-1:]); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	for i, sealed := range verified {
		before := len(args)
		args = appendProfiles(args, sealed, bindings[i].QueueMark, i, len(bindings) > 1, names, trace)
		if err := chargeEngineArgs(&used, args[before:]); err != nil {
			return nil, nil, err
		}
	}
	var observations *NativeTrace
	if trace {
		observations = newNativeTrace(verified)
	}
	return args, observations, nil
}

func appendProfiles(args []string, value Strategy, mark uint32, binding int, groupNames bool, names map[string]string, trace bool) []string {
	for i, profile := range value.Profiles {
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
		if profile.Family != "" {
			args = append(args, "--filter-l3="+string(profile.Family))
		}
		args = profile.Lists.appendArgs(args)
		args = append(args, profile.actionArgs(names[value.BlobData[profile.QUICBlobRef]])...)
		if trace {
			args = append(args, fmt.Sprintf("--lua-desync=solovey_profile_match:slot=%d/%d", binding+1, i+1))
		}
	}
	return args
}

func payloadProtocol(value Payload) string {
	switch value {
	case TLSClientHello:
		return "tls"
	case HTTPRequest:
		return "http"
	case QUICInitial:
		return "quic"
	default:
		return ""
	}
}

func (p Profile) actionArgs(blob string) []string {
	var lua string
	switch {
	case p.SplitAt != 0:
		lua = fmt.Sprintf("multisplit:pos=%d", p.SplitAt)
	case p.QUICRepeats != 0:
		if blob == "" {
			blob = defaultQUICBlobName
		}
		lua = fmt.Sprintf("fake:blob=%s:repeats=%d", blob, p.QUICRepeats)
	default:
		return nil
	}
	return []string{"--filter-l7=" + payloadProtocol(p.Payload), "--payload=" + string(p.Payload), "--lua-desync=" + lua}
}
