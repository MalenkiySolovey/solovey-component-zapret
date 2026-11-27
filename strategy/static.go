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
	"strings"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-host/outbounds"
)

const Schema = "solovey.zapret-strategy/v1"

type AssetRef string

const (
	Library         AssetRef = "upstream:zapret-lib"
	Actions         AssetRef = "upstream:zapret-antidpi"
	Automation      AssetRef = "upstream:zapret-auto"
	DefaultQUICBlob AssetRef = "upstream:fake-default-quic"
)

const defaultQUICBlobName = "fake_default_quic"

const maxSplitPosition = 128

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

// A profile matches one packet scope and applies a bounded native action.
type Profile struct {
	ID          string                `json:"id"`
	Protocol    intent.PacketProtocol `json:"protocol"`
	Ports       []uint16              `json:"ports"`
	UDPPorts    []uint16              `json:"udpPorts,omitempty"` // Additional filter in a combined TCP/UDP native profile.
	PortRanges  []intent.PortInterval `json:"portRanges,omitempty"`
	UDPRanges   []intent.PortInterval `json:"udpPortRanges,omitempty"`
	Payload     Payload               `json:"payload,omitempty"`
	SplitAt     uint16                `json:"splitAt,omitempty"`
	QUICRepeats uint8                 `json:"quicRepeats,omitempty"`
	QUICBlobRef string                `json:"quicBlobRef,omitempty"`
	Family      AddressFamily         `json:"family,omitempty"` // Empty matches either admitted family.
	Lists       *NativeLists          `json:"lists,omitempty"`
	Circular    *Circular             `json:"circular,omitempty"`
	HTTPActions []HTTPAction          `json:"httpActions,omitempty"`
	OutRange    string                `json:"outRange,omitempty"` // Outgoing filter for a single split, QUIC or pass action.
	L7Filter    string                `json:"l7Filter,omitempty"` // Independent L7 filter on a passive profile.
	Pass        bool                  `json:"pass,omitempty"`     // The original engine's explicit no-op Lua action.
	LuaActions  []LuaAction           `json:"luaActions,omitempty"`
	// ActionOrder is populated only when a profile mixes the legacy built-in
	// action slots with managed or typed Lua actions. Keeping the old fields for
	// single-kind profiles preserves their sealed revisions and wire shape.
	ActionOrder []NativeActionRef `json:"actionOrder,omitempty"`
}

type Strategy struct {
	Schema      string            `json:"schema"`
	ID          string            `json:"id"`
	Revision    string            `json:"revision"`
	NeedsReturn bool              `json:"needsReturn,omitempty"`
	Assets      []AssetRef        `json:"assets,omitempty"`
	Profiles    []Profile         `json:"profiles"` // selection order is significant
	BlobData    map[string]string `json:"blobData,omitempty"`
	ManagedLua  []LuaReference    `json:"managedLua,omitempty"`
}

type Scope struct {
	TCP, UDP             []uint16
	ReturnTCP            []uint16
	TCPRanges, UDPRanges []intent.PortInterval
	ReturnTCPRanges      []intent.PortInterval
	NeedsReturn          bool
}

var ErrInvalidStrategy = errors.New("invalid static Zapret strategy")

var errReturnObservationUnavailable = errors.New("Zapret return-path observation is not supported by the installed profile")

// Seal validates and returns a new content-revisioned Strategy. It copies
// caller-owned slices and normalizes sets without reordering profiles or Lua.
func Seal(value Strategy) (Strategy, error) {
	if value.Schema != Schema || !outbounds.ValidZapretStrategyRef(value.ID) || len(value.Profiles) == 0 || len(value.Assets) > 3 {
		return Strategy{}, ErrInvalidStrategy
	}
	value.Assets = slices.Clone(value.Assets)
	value.Profiles = slices.Clone(value.Profiles)
	value.BlobData = maps.Clone(value.BlobData)
	value.ManagedLua = slices.Clone(value.ManagedLua)
	if !validLuaReferences(value.ManagedLua) {
		return Strategy{}, fmt.Errorf("%w: invalid managed Lua selection", ErrInvalidStrategy)
	}
	seen := map[string]bool{}
	needsReturn := false
	usesManagedLua := false
	for i, profile := range value.Profiles {
		if !outbounds.ValidZapretStrategyRef(profile.ID) || seen[profile.ID] || !hasPorts(profile.Ports, profile.PortRanges) ||
			(profile.Protocol != intent.PacketProtocolTCP && profile.Protocol != intent.PacketProtocolUDP) ||
			(hasPorts(profile.UDPPorts, profile.UDPRanges) && profile.Protocol != intent.PacketProtocolTCP) {
			return Strategy{}, ErrInvalidStrategy
		}
		seen[profile.ID] = true
		var err error
		if profile.Ports, profile.PortRanges, err = sealPorts(profile.Ports, profile.PortRanges); err != nil {
			return Strategy{}, fmt.Errorf("%w: profile %q: %w", ErrInvalidStrategy, profile.ID, err)
		}
		if profile.UDPPorts, profile.UDPRanges, err = sealPorts(profile.UDPPorts, profile.UDPRanges); err != nil {
			return Strategy{}, fmt.Errorf("%w: profile %q UDP: %w", ErrInvalidStrategy, profile.ID, err)
		}
		if profile.Circular != nil && hasPorts(profile.UDPPorts, profile.UDPRanges) {
			return Strategy{}, fmt.Errorf("%w: profile %q: Circular is only qualified with a TCP-only filter and its admitted return path", ErrInvalidStrategy, profile.ID)
		}
		if profile.Family != "" && profile.Family != IPv4 && profile.Family != IPv6 || !profile.validAction() {
			return Strategy{}, ErrInvalidStrategy
		}
		if profile.Lists != nil {
			lists, err := profile.Lists.seal()
			if err != nil {
				return Strategy{}, fmt.Errorf("%w: invalid native lists in profile %q", err, profile.ID)
			}
			profile.Lists = lists
		}
		if profile.Circular != nil {
			cfg := *profile.Circular
			cfg.Splits = slices.Clone(cfg.Splits)
			profile.Circular = &cfg
			needsReturn = true
		}
		profile.HTTPActions = slices.Clone(profile.HTTPActions)
		profile.LuaActions = slices.Clone(profile.LuaActions)
		profile.ActionOrder = slices.Clone(profile.ActionOrder)
		usesManagedLua = usesManagedLua || len(profile.LuaActions) != 0
		if err := profile.sealOutRanges(); err != nil {
			return Strategy{}, fmt.Errorf("%w: profile %q: %w", ErrInvalidStrategy, profile.ID, err)
		}
		value.Profiles[i] = profile
	}
	if usesManagedLua != (len(value.ManagedLua) != 0) {
		return Strategy{}, fmt.Errorf("%w: managed Lua actions and selected dependencies are both required", ErrInvalidStrategy)
	}
	wantAssets := nativeAssets(value.Profiles)
	// Older sealed passive profiles could carry the two unused base assets.
	if !slices.Equal(value.Assets, wantAssets) && !(len(wantAssets) == 0 && slices.Equal(value.Assets, []AssetRef{Library, Actions})) {
		return Strategy{}, ErrInvalidStrategy
	}
	if value.NeedsReturn && !needsReturn {
		return Strategy{}, errReturnObservationUnavailable
	}
	value.NeedsReturn = needsReturn
	if err := value.validateBlobs(); err != nil {
		return Strategy{}, err
	}
	value.Revision = ""
	data, err := json.Marshal(value)
	if err != nil {
		return Strategy{}, ErrInvalidStrategy
	}
	value.Revision = fmt.Sprintf("%x", sha256.Sum256(data))
	return value, nil
}

func (p Profile) portsOn(protocol intent.PacketProtocol) ([]uint16, []intent.PortInterval) {
	if p.Protocol == protocol {
		return p.Ports, p.PortRanges
	}
	if protocol == intent.PacketProtocolUDP {
		return p.UDPPorts, p.UDPRanges
	}
	return nil, nil
}

func (p Profile) hasAction() bool {
	return p.SplitAt != 0 || p.QUICRepeats != 0 || p.Circular != nil || len(p.HTTPActions) != 0 || p.Pass || len(p.LuaActions) != 0 || len(p.ActionOrder) != 0
}

func nativeAssets(profiles []Profile) []AssetRef {
	var actions bool
	for _, p := range profiles {
		if p.Circular != nil {
			return []AssetRef{Library, Actions, Automation}
		}
		actions = actions || p.hasAction()
	}
	if actions {
		return []AssetRef{Library, Actions}
	}
	return nil
}

func (p Profile) validAction() bool {
	if p.QUICBlobRef != "" && (p.QUICRepeats == 0 || !outbounds.ValidZapretStrategyRef(p.QUICBlobRef)) {
		return false
	}
	if p.L7Filter != "" && (p.hasAction() && !p.Pass && len(p.LuaActions) == 0 || !validNativeSelector(p.L7Filter, nativeL7Names)) {
		return false
	}
	if len(p.ActionOrder) != 0 {
		return p.validOrderedAction()
	}
	switch {
	case len(p.LuaActions) != 0:
		return p.Payload == "" && p.OutRange == "" && !p.Pass && p.SplitAt == 0 && p.QUICRepeats == 0 &&
			p.Circular == nil && len(p.HTTPActions) == 0 && !slices.ContainsFunc(p.LuaActions, func(a LuaAction) bool { return !a.valid() })
	case p.Pass:
		return p.SplitAt == 0 && p.QUICRepeats == 0 && p.Circular == nil && len(p.HTTPActions) == 0 &&
			(p.Payload == "" || validNativeSelector(string(p.Payload), nativePayloadNames))
	case len(p.HTTPActions) != 0:
		return p.Protocol == intent.PacketProtocolTCP && p.Payload == HTTPRequest && p.SplitAt == 0 && p.QUICRepeats == 0 &&
			p.Circular == nil && validHTTPActions(p.HTTPActions)
	case p.Circular != nil:
		return p.Protocol == intent.PacketProtocolTCP && p.SplitAt == 0 && p.QUICRepeats == 0 &&
			(p.Payload == TLSClientHello || p.Payload == HTTPRequest) && p.Circular.valid()
	case p.SplitAt != 0:
		return p.Protocol == intent.PacketProtocolTCP && p.SplitAt <= maxSplitPosition && p.QUICRepeats == 0 &&
			(p.Payload == TLSClientHello || p.Payload == HTTPRequest)
	case p.QUICRepeats != 0:
		return hasPorts(p.portsOn(intent.PacketProtocolUDP)) && p.QUICRepeats <= 4 && p.Payload == QUICInitial
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
	var result Scope
	for _, profile := range value.Profiles {
		if family != "" && profile.Family != "" && profile.Family != family {
			continue
		}
		tcp, tcpRanges := profile.portsOn(intent.PacketProtocolTCP)
		udp, udpRanges := profile.portsOn(intent.PacketProtocolUDP)
		result.TCP = append(result.TCP, tcp...)
		result.UDP = append(result.UDP, udp...)
		result.TCPRanges = append(result.TCPRanges, tcpRanges...)
		result.UDPRanges = append(result.UDPRanges, udpRanges...)
		if profile.Circular != nil {
			result.ReturnTCP = append(result.ReturnTCP, tcp...)
			result.ReturnTCPRanges = append(result.ReturnTCPRanges, tcpRanges...)
		}
	}
	result.TCP, result.TCPRanges = unionPorts(result.TCP, result.TCPRanges)
	result.UDP, result.UDPRanges = unionPorts(result.UDP, result.UDPRanges)
	result.ReturnTCP, result.ReturnTCPRanges = unionPorts(result.ReturnTCP, result.ReturnTCPRanges)
	result.NeedsReturn = hasPorts(result.ReturnTCP, result.ReturnTCPRanges)
	return result
}

// HandlesQUIC requires the built-in action independent of destination lists
// and packet positions. A profile that shadows it may not run that action.
func (value Strategy) HandlesQUIC(family AddressFamily, port uint16) bool {
	for _, p := range value.Profiles {
		ports, ranges := p.portsOn(intent.PacketProtocolUDP)
		if (p.Family == "" || p.Family == family) && containsPort(port, ports, ranges) {
			// Typed actions emit an L7 filter for their payload. A mixed
			// TCP/TLS or TCP/HTTP action will not match a QUIC packet.
			if p.Pass || !p.hasAction() {
				if !passiveMayShadowQUIC(p.L7Filter) {
					continue
				}
			} else if p.Payload != QUICInitial {
				continue
			}
			if p.QUICRepeats == 0 || p.QUICBlobRef != "" || p.OutRange != "" {
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
	args, _, err := compileBindings(engineRoot, queue, loopMark, bindings, false, false)
	return args, err
}

// CompileBindingsWithNativeTrace emits one best-effort observation per matched
// native profile. Connect the fresh trace to this engine's private stdout only.
func CompileBindingsWithNativeTrace(engineRoot string, queue uint16, loopMark uint32, bindings []MarkedStrategy) ([]string, *NativeTrace, error) {
	return compileBindings(engineRoot, queue, loopMark, bindings, true, false)
}

// CompileBindingsForReturnPath is for a backend that has admitted every
// binding's exact return scope. Ordinary callers must use CompileBindings.
func CompileBindingsForReturnPath(engineRoot string, queue uint16, loopMark uint32, bindings []MarkedStrategy, trace bool) ([]string, *NativeTrace, error) {
	return compileBindings(engineRoot, queue, loopMark, bindings, trace, true)
}

func compileBindings(engineRoot string, queue uint16, loopMark uint32, bindings []MarkedStrategy, trace, returnPath bool) ([]string, *NativeTrace, error) {
	budget, err := currentEngineArgBudget()
	if err != nil {
		return nil, nil, err
	}
	return compileBindingsWithin(budget, engineRoot, queue, loopMark, bindings, trace, returnPath)
}

func compileBindingsWithin(budget engineArgBudget, engineRoot string, queue uint16, loopMark uint32, bindings []MarkedStrategy, trace, returnPath bool) ([]string, *NativeTrace, error) {
	return compileBindingsWithLuaWithin(budget, engineRoot, queue, loopMark, bindings, trace, returnPath, nil)
}

func compileBindingsWithLuaWithin(budget engineArgBudget, engineRoot string, queue uint16, loopMark uint32, bindings []MarkedStrategy,
	trace, returnPath bool, lua *luaBindings,
) ([]string, *NativeTrace, error) {
	if queue == 0 || loopMark == 0 || loopMark&(loopMark-1) != 0 || len(bindings) == 0 ||
		!path.IsAbs(engineRoot) || path.Clean(engineRoot) != engineRoot || len(engineRoot) > 512 || strings.ContainsAny(engineRoot, "\x00\r\n") {
		return nil, nil, errors.New("invalid Zapret engine root or Network Planner allocation")
	}
	if len(bindings) > budget.total/minimumEngineProfileBytes {
		return nil, nil, budget.exceedsTotal((budget.total/minimumEngineProfileBytes + 1) * minimumEngineProfileBytes)
	}
	verified := make([]Strategy, len(bindings))
	seen := make(map[uint32]bool, len(bindings))
	needsLua, needsCircular := false, false
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
		if sealed.NeedsReturn && !returnPath {
			return nil, nil, errReturnObservationUnavailable
		}
		if len(sealed.ManagedLua) != 0 && lua == nil {
			return nil, nil, errors.New("managed Zapret Lua requires an explicitly delegated installed Host")
		}
		minimum += len(sealed.Profiles) * minimumEngineProfileBytes
		if minimum > budget.total {
			return nil, nil, budget.exceedsTotal(minimum)
		}
		needsCircular = needsCircular || sealed.NeedsReturn
		verified[i] = sealed
		needsLua = needsLua || len(sealed.Assets) != 0
	}
	args := []string{fmt.Sprintf("--qnum=%d", queue), fmt.Sprintf("--fwmark=0x%x", loopMark)}
	if needsLua {
		args = append(args, "--lua-init=@"+path.Join(engineRoot, "lua/zapret-lib.lua"),
			"--lua-init=@"+path.Join(engineRoot, "lua/zapret-antidpi.lua"))
	}
	if needsCircular {
		args = append(args, "--lua-init=@"+path.Join(engineRoot, "lua/zapret-auto.lua"), "--lua-init="+circularHostKeyLua)
	}
	if trace {
		args = append(args, "--lua-init="+nativeTraceLua)
	}
	if lua != nil {
		args = append(args, "--lua-init=@"+lua.file)
	}
	used := 0
	if err := budget.charge(&used, args); err != nil {
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
				if err := budget.charge(&used, args[len(args)-1:]); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	for i, sealed := range verified {
		before := len(args)
		var entries []string
		if lua != nil {
			entries = lua.entries[i]
		}
		args = appendProfiles(args, sealed, bindings[i].QueueMark, i, len(bindings) > 1, names, trace, entries)
		if err := budget.charge(&used, args[before:]); err != nil {
			return nil, nil, err
		}
	}
	var observations *NativeTrace
	if trace {
		observations = newNativeTrace(verified)
	}
	return args, observations, nil
}

func appendProfiles(args []string, value Strategy, mark uint32, binding int, groupNames bool, names map[string]string, trace bool, entries []string) []string {
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
		args = profile.appendPortFilters(append(args, fmt.Sprintf("--filter-mark=0x%x/0xffffffff", mark)))
		if profile.Family != "" {
			args = append(args, "--filter-l3="+string(profile.Family))
		}
		args = profile.Lists.appendArgs(args)
		if profile.Circular != nil {
			args = append(args, profile.circularArgs(fmt.Sprintf("%d/%d", binding+1, i+1), trace)...)
			continue
		}
		var observation string
		if trace {
			observation = fmt.Sprintf("--lua-desync=solovey_profile_match:slot=%d/%d", binding+1, i+1)
		}
		if len(profile.ActionOrder) != 0 {
			args = profile.appendOrderedActions(args, names[value.BlobData[profile.QUICBlobRef]], entries, observation)
			entries = entries[len(profile.LuaActions):]
			continue
		}
		if count := len(profile.LuaActions); count != 0 {
			args = profile.appendManagedLuaActions(args, entries[:count], observation)
			entries = entries[count:]
			continue
		}
		args = profile.appendActions(args, names[value.BlobData[profile.QUICBlobRef]], observation)
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

func (p Profile) appendActions(args []string, blob, observation string) []string {
	if p.L7Filter != "" {
		args = append(args, "--filter-l7="+p.L7Filter)
	}
	if p.Pass {
		if p.Payload != "" {
			args = append(args, "--payload="+string(p.Payload))
		}
		args = append(appendOutRange(args, "", p.OutRange), "--lua-desync=pass")
	}
	if len(p.HTTPActions) != 0 {
		args = append(args, "--filter-l7=http", "--payload="+string(HTTPRequest))
		var current string
		for _, action := range p.HTTPActions {
			args = appendOutRange(args, current, action.OutRange)
			args = append(args, action.argument())
			current = action.OutRange
		}
		if observation != "" {
			args = append(args, observation)
			// Observe either range without changing action order or ordinals.
			if first := p.HTTPActions[0].OutRange; first != current {
				args = append(appendOutRange(args, current, first), observation)
			}
		}
		return args
	}
	var lua string
	switch {
	case p.SplitAt != 0:
		lua = fmt.Sprintf("multisplit:pos=%d", p.SplitAt)
	case p.QUICRepeats != 0:
		if blob == "" {
			blob = defaultQUICBlobName
		}
		lua = fmt.Sprintf("fake:blob=%s:repeats=%d", blob, p.QUICRepeats)
	}
	if lua != "" {
		args = append(args, "--filter-l7="+payloadProtocol(p.Payload), "--payload="+string(p.Payload))
		args = append(appendOutRange(args, "", p.OutRange), "--lua-desync="+lua)
	}
	if observation != "" {
		args = append(args, observation)
	}
	return args
}
