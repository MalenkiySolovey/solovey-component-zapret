package strategy

import (
	"slices"
	"strings"
	"testing"
)

func TestNativeTraceUsesOnlyCompilerSlots(t *testing.T) {
	bindings := []MarkedStrategy{{Strategy: fixture(t), QueueMark: 0x120000}, {Strategy: fixture(t), QueueMark: 0x130000}}
	plain, err := CompileBindings("/engine", 400, 0x40000000, bindings)
	if err != nil {
		t.Fatal(err)
	}
	traced, trace, err := CompileBindingsWithNativeTrace("/engine", 400, 0x40000000, bindings)
	if err != nil {
		t.Fatal(err)
	}
	var slots []string
	for _, arg := range traced {
		if slot, ok := strings.CutPrefix(arg, "--lua-desync=solovey_profile_match:slot="); ok {
			slots = append(slots, slot)
		}
	}
	if !slices.Equal(slots, []string{"1/1", "2/1"}) || !slices.Equal(plain, slices.DeleteFunc(traced, func(arg string) bool {
		return arg == "--lua-init="+nativeTraceLua || strings.HasPrefix(arg, "--lua-desync=solovey_profile_match:slot=")
	})) {
		t.Fatal("tracing changed the native rules or used an unbound observation slot")
	}
	if b, p, ok := parseNativeTrace(nativeTracePrefix + "3/16"); !ok || b != 3 || p != 16 {
		t.Fatal("lost a valid bounded observation")
	}
	for _, invalid := range []string{"0/1", "4/1", "1/0", "1/17", "1/01", "1/+2", "1/2\n", "1/2/3", "1/2 remote.example"} {
		if _, _, ok := parseNativeTrace(nativeTracePrefix + invalid); ok {
			t.Fatalf("accepted non-canonical trace record %q", invalid)
		}
	}
	for _, chunk := range []string{
		strings.Repeat("x", 65536), nativeTracePrefix + "2/1\n", // no suffix match on an oversized line
		nativeTracePrefix + "3/1\n" + nativeTracePrefix + "1/2\n", // not bound to this generation
		nativeTracePrefix[:8], nativeTracePrefix[8:] + "1/", "1\n" + nativeTracePrefix + "1/1\n",
	} {
		if n, err := trace.Write([]byte(chunk)); n != len(chunk) || err != nil {
			t.Fatalf("stdout sink failed: %d, %v", n, err)
		}
	}
	if got := trace.MatchedProfiles(0); !slices.Equal(got, []string{bindings[0].Strategy.Profiles[0].ID}) ||
		len(trace.MatchedProfiles(1)) != 0 || len(trace.MatchedProfiles(2)) != 0 {
		t.Fatalf("invalid profile observation set for this generation: %v", got)
	}
	_, next, err := CompileBindingsWithNativeTrace("/engine", 400, 0x40000000, bindings)
	if err != nil || len(next.MatchedProfiles(0)) != 0 {
		t.Fatal("a new engine trace inherited retired observations")
	}
}
