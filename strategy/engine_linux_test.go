package strategy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"golang.org/x/sys/unix"
)

func TestPinnedEngineValidatesProfiles(t *testing.T) {
	root := os.Getenv("SOLOVEY_TEST_ENGINE_ROOT")
	if root == "" {
		t.Skip("requires independently built nfqws2 engine input")
	}
	value := fixture(t)
	value.Profiles[0].Lists = &NativeLists{
		HostListExclude: []string{"plain.example.test"}, IPSet: []string{"192.0.2.0/24", "2001:db8::/32"},
	}
	value.Profiles = append(value.Profiles, Profile{ID: "udp-quic", Protocol: intent.PacketProtocolUDP, Ports: []uint16{8443},
		Payload: QUICInitial, QUICRepeats: 1})
	cat := namedCatalog(t)
	cat.BlobAssets = map[string]string{"fixture": "40534f4c4f564559"}
	value.Profiles[len(value.Profiles)-1].QUICBlobRef = "fixture"
	sealed, err := cat.SealDraft(Draft{ID: value.ID, Name: "engine argv", Profiles: value.Profiles,
		ListRefs: ListRefs{value.Profiles[0].ID: {HostList: "names", IPSetExclude: "ips"}}})
	if err != nil {
		t.Fatal(err)
	}
	bindings := make([]MarkedStrategy, 6)
	for i := range bindings {
		bindings[i] = MarkedStrategy{Strategy: sealed.Definition, QueueMark: 0x120000 + uint32(i)}
	}
	argv, err := CompileBindings(root, 401, 0x40000000, bindings)
	if err != nil {
		t.Fatal(err)
	}
	validateEngineArgs(t, root, argv)
	value, err = ParseNative("fixture-circular", circularFixture)
	if err != nil {
		t.Fatal(err)
	}
	bindings[0].Strategy = value
	value, err = ParseNative("fixture-http", nativeHTTPFixture)
	if err != nil {
		t.Fatal(err)
	}
	bindings[1].Strategy = value
	value, err = ParseNative("fixture-ranged", rangedProfileFixture())
	if err != nil {
		t.Fatal(err)
	}
	bindings[2].Strategy = value
	value, err = ParseNative("fixture-action-range", rangedActionFixture)
	if err != nil {
		t.Fatal(err)
	}
	bindings[3].Strategy = value
	argv, _, err = CompileBindingsForReturnPath(root, 401, 0x40000000, bindings, true)
	if err != nil {
		t.Fatal(err)
	}
	validateEngineArgs(t, root, argv)
	for _, template := range PresetTemplates() {
		if template.Compatibility != "adapted" {
			continue
		}
		t.Run(template.ID, func(t *testing.T) {
			validatePresetEngine(t, root, PresetInput{Format: "template", TemplateID: template.ID})
		})
	}
	t.Run("managed-package", func(t *testing.T) {
		validatePresetEngine(t, root, PresetInput{Format: "zip", Content: fixturePresetPackage(t, portablePreset)})
	})
	t.Run("managed-binary-package", func(t *testing.T) {
		validatePresetEngine(t, root, PresetInput{Format: "zip", Content: presetBinaryFixture(t, "\xff\x00\x7f"),
			DependencyIDs: []PresetDependencyID{{"blobs/fixture.bin", "portable-blob"}}})
	})
	t.Run("multiple-managed-blobs", func(t *testing.T) {
		catalog, draft := namedBlobBudgetFixture(9, 140)
		record, err := catalog.SealDraft(draft)
		if err != nil {
			t.Fatal(err)
		}
		argv, err := record.Definition.Compile(root, 401, 0x120000, 0x40000000)
		if err != nil {
			t.Fatal(err)
		}
		validateEngineArgs(t, root, argv)
	})
	t.Run("zapretgui-package", func(t *testing.T) {
		validatePresetEngine(t, root, PresetInput{Format: "zip", Content: guiPackageFixture(t, guiFixturePreset, "standalone", nil)})
	})
	t.Run("native-list-byte-budget", func(t *testing.T) {
		text := "--filter-tcp=443 --hostlist-domains=" + strings.Join(namedListFixture(80), ",")
		validatePresetEngine(t, root, PresetInput{Format: "native", Content: []byte(text)})
	})
	t.Run("native-dual-transport", func(t *testing.T) {
		validatePresetEngine(t, root, PresetInput{Format: "native", Content: []byte(dualTransportFixture)})
	})
	t.Run("native-compact-port-ranges", func(t *testing.T) {
		text := "--filter-tcp=80,40000-46000,65534-65535 --filter-udp=3478-19302"
		validatePresetEngine(t, root, PresetInput{Format: "native", Content: []byte(text)})
	})
	t.Run("native-passive-and-pass", func(t *testing.T) {
		validatePresetEngine(t, root, PresetInput{Format: "native", Content: []byte(nativePassiveFixture)})
	})
}

func validatePresetEngine(t *testing.T, root string, input PresetInput) {
	t.Helper()
	_, review, err := emptyPresetCatalog().PreparePreset("engine-preset", input)
	if err != nil || review.Record == nil {
		t.Fatalf("preset admission: %v, %+v", err, review.Issues)
	}
	argv, err := CompileBindings(root, 401, 0x40000000, []MarkedStrategy{{Strategy: review.Record.Definition, QueueMark: 0x120000}})
	if err != nil {
		t.Fatal(err)
	}
	validateEngineArgs(t, root, argv)
}

func TestPinnedEngineRunsManagedLua(t *testing.T) {
	root := os.Getenv("SOLOVEY_TEST_ENGINE_ROOT")
	if root == "" {
		t.Skip("requires independently built nfqws2 engine input")
	}
	dependency := "\xef\xbb\xbf#!/usr/bin/env lua\r\nreturn { verdict = function() return VERDICT_PASS end }"
	source := `
local dep = require("example")
assert(_G == _ENV and getmetatable(_G) == false)
assert(io == nil and load == nil and loadfile == nil and dofile == nil and package == nil and debug == nil)
assert(os.execute == nil and os.getenv == nil and os.remove == nil)
assert(type(rawsend) == "function" and type(rawsend_dissect) == "function" and type(conntrack_feed) == "function")
assert(type(reconstruct_dissect) == "function" and type(csum_tcp_fix) == "function")
assert(type(timer_set) == "function" and type(timer_del) == "function" and type(stat) == "function")
assert(type(get_source_ip) == "function" and type(get_ifaddrs) == "function")
local address = pton("192.0.2.1")
assert(type(address) == "string" and ntop(address) == "192.0.2.1")
assert(bitand(7, 3) == 3 and bitor(4, 1) == 5 and u16(bu16(80)) == 80)
assert(type(get_ifaddrs()) == "table")
assert(DLOG_ERR("managed native API probe") == nil)
assert(getmetatable("") == nil)
assert(not pcall(require, "unselected"))
function observe(ctx, desync)
    assert(desync.track.private == nil)
    desync.track.seen = true
    assert(u16(bu16(80)) == 80)
    return dep.verdict()
end
function invalid(ctx, desync) error("controlled action failure") end`
	modules := []LuaModule{
		{"example", LuaDraft{Origin: "external", SourceRef: "test fixture", Source: dependency}, LuaSourceDigest(dependency), LuaSourceDigest(dependency)},
		{"operator", LuaDraft{Origin: "administrator", Source: source}, LuaSourceDigest(source), ""},
	}
	program, entries, err := CompileManagedLua(0, modules, []string{"observe", "invalid"})
	if err != nil {
		t.Fatal(err)
	}
	// The test uses the original Lua engine without wiring a live packet hook.
	// The trusted test tail calls the generated callbacks with inert data.
	program += `
local packet = {track = {private = function() end}}
assert(solovey_managed_0_0(nil, packet) == VERDICT_PASS)
assert(packet.track.seen == nil)
assert(solovey_managed_0_1(nil, packet) == VERDICT_DROP)
assert(not pcall(solovey_bind, "invalidtest", "missing_function"))
print("MANAGED_LUA_CALLBACKS_VERIFIED")
os.exit(0) -- The no-intercept engine otherwise waits in its timer loop.
`
	fd, err := unix.MemfdCreate("solovey-test-zapret-lua", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), "solovey-test-zapret-lua")
	defer file.Close()
	if _, err := file.WriteString(program); err != nil {
		t.Fatal(err)
	}
	if err := file.Chmod(0o444); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_SEAL|unix.F_SEAL_SHRINK|unix.F_SEAL_GROW|unix.F_SEAL_WRITE); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(root, "nfqws2"), "--intercept=0", "--uid=65534:65534", "--lua-init=@/proc/self/fd/3",
		"--filter-tcp=443", "--lua-desync="+entries[0])
	cmd.Dir = root
	cmd.ExtraFiles = []*os.File{file}
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "MANAGED_LUA_CALLBACKS_VERIFIED") || !strings.Contains(string(out), "managed Lua action failed") {
		t.Fatalf("nfqws2 managed source: %v\n%s", err, out)
	}
}

func validateEngineArgs(t *testing.T, root string, argv []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(root, "nfqws2"), append([]string{"--dry-run", "--intercept=0"}, argv...)...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "command line parameters verified") {
		t.Fatalf("nfqws2 dry-run: %v\n%s", err, out)
	}
	t.Logf("nfqws2 dry-run:\n%s", out)
}
