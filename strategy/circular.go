package strategy

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
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
	orchestrator := fmt.Sprintf("--lua-desync=circular:fails=%d", p.Circular.Failures)
	if slot != "" {
		orchestrator += ":key=solovey_" + slot + ":hostkey=solovey_circular_hostkey"
	}
	args = append(args, orchestrator, "--in-range="+circularVariantRange, "--payload="+string(p.Payload))
	for i, position := range p.Circular.Splits {
		args = append(args, fmt.Sprintf("--lua-desync=multisplit:pos=%d:strategy=%d", position, i+1))
		if trace {
			args = append(args, fmt.Sprintf("--lua-desync=solovey_profile_match:slot=%s:strategy=%d", slot, i+1))
		}
	}
	return args
}
