package strategy

import (
	"fmt"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
)

// NativeActionRef records the source order of actions that are otherwise kept
// in the legacy typed fields on Profile. Index refers to HTTPActions or
// LuaActions; a built-in action uses Payload to snapshot the --payload value
// that preceded it in the native source.
type NativeActionRef struct {
	Kind    string  `json:"kind"`
	Index   int     `json:"index,omitempty"`
	Payload Payload `json:"payload,omitempty"`
}

const (
	nativeActionBuiltin = "builtin"
	nativeActionHTTP    = "http"
	nativeActionManaged = "managed_lua"
)

func (p Profile) hasNonManagedAction() bool {
	return p.SplitAt != 0 || p.QUICRepeats != 0 || p.Circular != nil || len(p.HTTPActions) != 0 || p.Pass
}

func (p Profile) validOrderedAction() bool {
	if len(p.ActionOrder) < 2 || len(p.LuaActions) == 0 || p.Payload != "" || p.Circular != nil {
		return false
	}
	usedHTTP := make([]bool, len(p.HTTPActions))
	usedLua := make([]bool, len(p.LuaActions))
	builtins := 0
	for _, ref := range p.ActionOrder {
		switch ref.Kind {
		case nativeActionBuiltin:
			if builtins != 0 || !p.validOrderedBuiltin(ref.Payload) {
				return false
			}
			builtins++
		case nativeActionHTTP:
			if ref.Index < 0 || ref.Index >= len(p.HTTPActions) || usedHTTP[ref.Index] ||
				p.Protocol != intent.PacketProtocolTCP || p.OutRange != "" {
				return false
			}
			usedHTTP[ref.Index] = true
		case nativeActionManaged:
			if ref.Index < 0 || ref.Index >= len(p.LuaActions) || usedLua[ref.Index] || !p.LuaActions[ref.Index].valid() {
				return false
			}
			usedLua[ref.Index] = true
		default:
			return false
		}
	}
	if builtins == 0 && len(p.HTTPActions) == 0 || builtins != 0 && len(p.HTTPActions) != 0 {
		return false
	}
	for _, used := range usedHTTP {
		if !used {
			return false
		}
	}
	for _, used := range usedLua {
		if !used {
			return false
		}
	}
	return builtins != 0 || len(p.HTTPActions) != 0
}

func (p Profile) validOrderedBuiltin(payload Payload) bool {
	switch {
	case p.Pass:
		return p.SplitAt == 0 && p.QUICRepeats == 0 && p.Circular == nil &&
			len(p.HTTPActions) == 0 && (payload == "" || validNativeSelector(string(payload), nativePayloadNames))
	case p.SplitAt != 0:
		return p.Protocol == intent.PacketProtocolTCP && p.QUICRepeats == 0 && p.Circular == nil &&
			len(p.HTTPActions) == 0 && (payload == TLSClientHello || payload == HTTPRequest) && p.SplitAt <= maxSplitPosition
	case p.QUICRepeats != 0:
		return hasPorts(p.portsOn(intent.PacketProtocolUDP)) && p.QUICRepeats <= 4 &&
			p.Circular == nil && len(p.HTTPActions) == 0 && payload == QUICInitial
	default:
		return false
	}
}

func (p Profile) orderedL7Filter() string {
	if p.L7Filter != "" {
		return p.L7Filter
	}
	if len(p.HTTPActions) != 0 {
		return "http"
	}
	for _, ref := range p.ActionOrder {
		if ref.Kind == nativeActionBuiltin {
			if value := payloadProtocol(ref.Payload); value != "" {
				return value
			}
		}
	}
	return ""
}

// appendOrderedActions emits a mixed profile without changing the order in
// which nfqws2 snapshots payload and outgoing-range filters. The managed Lua
// entry names are supplied in LuaActions order by CompileManagedBindings.
func (p Profile) appendOrderedActions(args []string, blob string, entries []string, observation string) []string {
	if filter := p.orderedL7Filter(); filter != "" {
		args = append(args, "--filter-l7="+filter)
	}
	currentPayload, currentRange := Payload(""), ""
	luaIndex := 0
	observed := false
	for _, ref := range p.ActionOrder {
		var payload Payload
		var outRange string
		switch ref.Kind {
		case nativeActionBuiltin:
			payload, outRange = ref.Payload, p.OutRange
		case nativeActionHTTP:
			payload, outRange = HTTPRequest, p.HTTPActions[ref.Index].OutRange
		case nativeActionManaged:
			action := p.LuaActions[ref.Index]
			payload, outRange = action.Payload, action.OutRange
		}
		effectivePayload := payload
		if ref.Kind == nativeActionManaged && effectivePayload == "" {
			effectivePayload = "all"
		}
		if effectivePayload != "" && effectivePayload != currentPayload {
			args = append(args, "--payload="+string(effectivePayload))
			currentPayload = effectivePayload
		}
		args = appendOutRange(args, currentRange, outRange)
		currentRange = outRange
		switch ref.Kind {
		case nativeActionBuiltin:
			switch {
			case p.Pass:
				args = append(args, "--lua-desync=pass")
			case p.SplitAt != 0:
				args = append(args, fmt.Sprintf("--lua-desync=multisplit:pos=%d", p.SplitAt))
			case p.QUICRepeats != 0:
				if blob == "" {
					blob = defaultQUICBlobName
				}
				args = append(args, fmt.Sprintf("--lua-desync=fake:blob=%s:repeats=%d", blob, p.QUICRepeats))
			}
		case nativeActionHTTP:
			args = append(args, p.HTTPActions[ref.Index].argument())
		case nativeActionManaged:
			name := p.LuaActions[ref.Index].Function
			if luaIndex < len(entries) {
				name = entries[luaIndex]
			}
			args = append(args, p.LuaActions[ref.Index].argument(name))
			luaIndex++
		}
		if observation != "" && !observed {
			args = append(args, observation)
			observed = true
		}
	}
	return args
}
