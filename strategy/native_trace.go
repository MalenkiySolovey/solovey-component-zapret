package strategy

import (
	"bytes"
	"strconv"
	"strings"
	"sync"
)

const nativeTracePrefix = "SOLOVEY_ZAPRET_MATCH_V1 "
const maxTraceSlotLength = len("2147483647/2147483647")

// Only compiler-generated slots reach this action. It reads no packet data,
// returns no verdict, and writes at most once per slot per engine generation.
const nativeTraceLua = `local seen, output = {}, io.stdout;
local function emit(slot)
  output:write("` + nativeTracePrefix + `", slot, "\n");
  output:flush();
end;
function solovey_profile_match(_, packet)
  local slot = packet.arg and packet.arg.slot;
  if slot and not seen[slot] then
    seen[slot] = true;
    pcall(emit, slot);
  end;
end`

// NativeTrace is a bounded stdout sink for one engine generation. It keeps
// no engine messages, packet contents, or verdicts, only known profile IDs.
type NativeTrace struct {
	mu       sync.Mutex
	bindings [][]profileMatch
	line     [len(nativeTracePrefix) + maxTraceSlotLength]byte
	length   int // len(line)+1 means discard the oversized line until newline
}

type profileMatch struct {
	id      string
	matched bool
}

func newNativeTrace(verified []Strategy) *NativeTrace {
	trace := &NativeTrace{bindings: make([][]profileMatch, len(verified))}
	for i, binding := range verified {
		trace.bindings[i] = make([]profileMatch, len(binding.Profiles))
		for j, profile := range binding.Profiles {
			trace.bindings[i][j].id = profile.ID
		}
	}
	return trace
}

func (t *NativeTrace) Write(data []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	total := len(data)
	for len(data) != 0 {
		part, remaining, end := bytes.Cut(data, []byte{'\n'})
		if len(part) > len(t.line)-t.length {
			t.length = len(t.line) + 1
		} else {
			t.length += copy(t.line[t.length:], part)
		}
		if end {
			if t.length <= len(t.line) {
				b, p, ok := parseNativeTrace(string(t.line[:t.length]))
				if ok && b <= len(t.bindings) && p <= len(t.bindings[b-1]) {
					t.bindings[b-1][p-1].matched = true
				}
			}
			t.length = 0
		}
		data = remaining
	}
	return total, nil
}

// MatchedProfiles returns distinct observations for a zero-based binding, in
// native profile order. The caller receives a copy of the observed IDs.
func (t *NativeTrace) MatchedProfiles(binding int) []string {
	if binding < 0 || binding >= len(t.bindings) {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var result []string
	for _, profile := range t.bindings[binding] {
		if profile.matched {
			result = append(result, profile.id)
		}
	}
	return result
}

func parseNativeTrace(line string) (binding, profile int, ok bool) {
	slot, ok := strings.CutPrefix(line, nativeTracePrefix)
	if !ok || len(slot) < 3 || len(slot) > maxTraceSlotLength {
		return 0, 0, false
	}
	a, b, ok := strings.Cut(slot, "/")
	if !ok {
		return 0, 0, false
	}
	binding, first := strconv.Atoi(a)
	profile, second := strconv.Atoi(b)
	if first != nil || second != nil || binding < 1 || profile < 1 ||
		strconv.Itoa(binding) != a || strconv.Itoa(profile) != b {
		return 0, 0, false
	}
	return binding, profile, true
}
