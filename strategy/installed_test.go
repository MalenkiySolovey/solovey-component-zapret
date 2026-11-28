package strategy

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestInstalledBindingsMoveLargeListsOutOfArgv(t *testing.T) {
	value := fixture(t)
	domains := make([]string, 12000)
	for i := range domains {
		domains[i] = fmt.Sprintf("fixture-%05d.test", i)
	}
	value.Profiles[0].Lists = &NativeLists{
		HostList: domains, HostListExclude: []string{"unmatched.test"},
		IPSet: []string{"203.0.113.0/24"}, IPSetExclude: []string{"203.0.113.30/32"},
	}
	value, err := Seal(value)
	if err != nil {
		t.Fatal(err)
	}
	bindings := []MarkedStrategy{{Strategy: value, QueueMark: 0x120000}, {Strategy: value, QueueMark: 0x130000}}
	if _, err := CompileBindings("/engine", 401, 0x40000000, bindings); !errors.Is(err, ErrEngineArgumentBudget) {
		t.Fatalf("inline argv did not exercise the per-string boundary: %v", err)
	}
	got, err := CompileInstalledBindings("/engine", "/tmp/list-fixture", "", 401, 0x40000000, bindings, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{strings.Join(domains, "\n") + "\n", "unmatched.test\n", "203.0.113.0/24\n", "203.0.113.30/32\n"}
	if !slices.Equal(got.ListFiles, want) {
		t.Fatal("did not preserve the canonical include and exclude bytes exactly once")
	}
	if !slices.Contains(got.Args, "--hostlist=/tmp/list-fixture/list-0") ||
		!slices.Contains(got.Args, "--hostlist-exclude=/tmp/list-fixture/list-1") ||
		!slices.Contains(got.Args, "--ipset=/tmp/list-fixture/list-2") ||
		!slices.Contains(got.Args, "--ipset-exclude=/tmp/list-fixture/list-3") ||
		slices.ContainsFunc(got.Args, func(arg string) bool { return strings.Contains(arg, domains[0]) }) {
		t.Fatalf("installed argv contains missing or inline list values: %v", got.Args)
	}
	if _, err := CompileInstalledBindings("/engine", "/tmp/../etc", "", 401, 0x40000000, bindings, nil, false, false); err == nil {
		t.Fatal("accepted a noncanonical Host staging directory")
	}
}
