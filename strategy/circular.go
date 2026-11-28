package strategy

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
)

// Circular selects among ordered native TCP splits. The pinned engine owns
// detection, per-connection state, and rotation; the compiler owns its scope.
type Circular struct {
	Failures uint8    `json:"failures"`
	Splits   []uint16 `json:"splits"`
}

// CircularReplyPacketLimit is the reply window the packet backend must admit
// for a selected TCP flow. Variant actions never see that direction.
const CircularReplyPacketLimit = 32

const circularVariantRange = "x"

const nativeActionCircular = "circular" // Index 0 is the orchestrator; 1..N are its ordered split variants.

func circularObserveRange() string { return "-n" + strconv.Itoa(CircularReplyPacketLimit) }

func (c *Circular) valid() bool {
	if c.Failures == 0 || len(c.Splits) < 2 || len(c.Splits) > maxSplitPosition {
		return false
	}
	for i, position := range c.Splits {
		if position == 0 || position > maxSplitPosition || slices.Contains(c.Splits[:i], position) {
			return false
		}
	}
	return true
}

func (c *Circular) addVariant(input string) bool {
	position, ok := strings.CutPrefix(input, "multisplit:pos=")
	if !ok || len(c.Splits) >= maxSplitPosition {
		return false
	}
	position, index, ok := strings.Cut(position, ":strategy=")
	n, err := strconv.ParseUint(position, 10, 16)
	if !ok || err != nil || index != strconv.Itoa(len(c.Splits)+1) {
		return false
	}
	c.Splits = append(c.Splits, uint16(n))
	return true
}

// Limit native host records independently for each compiled binding/profile.
// Only the immutable upstream circular action chooses variants. The helper
// bounds storage and never reads a file or forwards a packet.
const circularHostKeyLua = `local slots = {};
function solovey_circular_hostkey(packet)
  local host = standard_hostkey(packet);
  if host and #host > 253 then host = host_ip(packet); end;
  if not host then return nil; end;
  local key = packet.arg.key;
  local bucket = autostate and autostate[key];
  if bucket and bucket[host] then return host; end;
  local ring = slots[key];
  if not ring then ring = { next = 1 }; slots[key] = ring; end;
  if bucket and ring[ring.next] then bucket[ring[ring.next]] = nil; end;
  ring[ring.next] = host;
  ring.next = (ring.next % 128) + 1;
  return host;
end`

func (p Profile) circularArgs(slot string, trace bool) []string {
	args := []string{"--filter-l7=" + payloadProtocol(p.Payload), "--in-range=" + circularObserveRange()}
	args = append(args, p.Circular.orchestrator(slot), "--in-range="+circularVariantRange, "--payload="+string(p.Payload))
	for i := range p.Circular.Splits {
		args = p.Circular.appendSplit(args, i+1, slot, trace)
	}
	return args
}

func (c *Circular) orchestrator(slot string) string {
	orchestrator := fmt.Sprintf("--lua-desync=circular:fails=%d", c.Failures)
	if slot != "" {
		orchestrator += ":key=solovey_" + slot + ":hostkey=solovey_circular_hostkey"
	}
	return orchestrator
}

func (c *Circular) appendSplit(args []string, variant int, slot string, trace bool) []string {
	args = append(args, fmt.Sprintf("--lua-desync=multisplit:pos=%d:strategy=%d", c.Splits[variant-1], variant))
	if trace {
		args = append(args, fmt.Sprintf("--lua-desync=solovey_profile_match:slot=%s:strategy=%d", slot, variant))
	}
	return args
}

// Circular consumes the remainder of the native plan and runs only the
// actions whose literal strategy tag matches the selected variant.
func circularStrategyTag(args string) (int, bool) {
	tag := 0
	for args != "" {
		i := 0
		for i < len(args) && args[i] != ':' {
			if args[i] == '\\' && i+1 < len(args) && args[i+1] == ':' {
				i++
			}
			i++
		}
		key, value, _ := strings.Cut(args[:i], "=")
		if key == "strategy" {
			n, err := strconv.Atoi(value)
			if tag != 0 || err != nil || n < 1 {
				return 0, false
			}
			tag = n
		}
		args = args[min(i+1, len(args)):]
	}
	return tag, tag != 0
}

func (p Profile) validCircularOrder() bool {
	if p.Protocol != intent.PacketProtocolTCP || p.Payload != "" || p.Pass || p.SplitAt != 0 ||
		p.QUICRepeats != 0 || p.OutRange != "" || len(p.HTTPActions) != 0 || len(p.LuaActions) == 0 || !p.Circular.valid() {
		return false
	}
	nextSplit, luaIndex, started := 1, 0, false
	for _, ref := range p.ActionOrder {
		switch ref.Kind {
		case nativeActionCircular:
			if ref.Index == 0 {
				if started || ref.Payload != "" {
					return false
				}
				started = true
				continue
			}
			if !started || ref.Index != nextSplit || (ref.Payload != TLSClientHello && ref.Payload != HTTPRequest) ||
				payloadProtocol(ref.Payload) != p.L7Filter {
				return false
			}
			nextSplit++
		case nativeActionManaged:
			if ref.Index != luaIndex || ref.Payload != "" || luaIndex >= len(p.LuaActions) || !p.LuaActions[luaIndex].valid() {
				return false
			}
			if started {
				if tag, ok := circularStrategyTag(p.LuaActions[luaIndex].Arguments); !ok || tag > len(p.Circular.Splits) {
					return false
				}
			}
			luaIndex++
		default:
			return false
		}
	}
	return nextSplit == len(p.Circular.Splits)+1 && luaIndex == len(p.LuaActions)
}

func (p Profile) appendOrderedCircular(args []string, entries []string, slot string, trace bool) []string {
	args = append(args, "--filter-l7="+p.L7Filter)
	payload, span := Payload("all"), ""
	for _, ref := range p.ActionOrder {
		nextPayload, nextSpan := ref.Payload, ""
		if ref.Kind == nativeActionManaged {
			nextPayload, nextSpan = p.LuaActions[ref.Index].Payload, p.LuaActions[ref.Index].OutRange
		}
		if nextPayload == "" {
			nextPayload = "all"
		}
		if nextPayload != payload {
			args = append(args, "--payload="+string(nextPayload))
			payload = nextPayload
		}
		args = appendOutRange(args, span, nextSpan)
		span = nextSpan
		if ref.Kind == nativeActionManaged {
			action := p.LuaActions[ref.Index]
			name := action.Function
			if entries != nil {
				name = entries[ref.Index]
			}
			args = append(args, action.argument(name))
		} else if ref.Index == 0 {
			args = append(args, "--in-range="+circularObserveRange(), p.Circular.orchestrator(slot), "--in-range="+circularVariantRange)
		} else {
			args = p.Circular.appendSplit(args, ref.Index, slot, trace)
		}
	}
	return args
}
