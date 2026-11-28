package strategy

import (
	"bytes"
	"context"
	"fmt"
	"net"
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
	t.Run("native-wildcard-and-negated-ports", func(t *testing.T) {
		text := "--filter-tcp=~80-82,81 --filter-udp=*"
		validateEngineArgs(t, root, []string{"--qnum=401", "--filter-tcp=~80-82,81", "--filter-udp=*"})
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
	dependency := "\xef\xbb\xbf#!/usr/bin/env lua\r\nreturn { verdict = function() return VERDICT_PASS end, edit = function() return VERDICT_MODIFY | VERDICT_PRESERVE_NEXT end }"
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
    assert(desync.track.lua_state.private == nil)
    desync.track.seen = true
    desync.track.lua_state.seen = true
    assert(u16(bu16(80)) == 80)
    return dep.verdict()
end
local retained
function edit(ctx, desync)
    assert(desync.track.lua_state.seen)
    retained = desync.dis
    retained.payload = "new"
    return dep.edit()
end
function invalid(ctx, desync)
    retained.payload = "not shared"
    desync.dis.ip.ip_dst = "bad address"
    return VERDICT_MODIFY
end
function retarget(ctx, desync)
    desync.dis.udp.uh_dport = desync.dis.udp.uh_dport + 1
    return VERDICT_MODIFY
end
function multiple() return VERDICT_PASS, VERDICT_DROP end
function invalid_flag() return VERDICT_MODIFY | 8 end
function error_action() error("controlled action failure") end`
	modules := []LuaModule{
		{"example", LuaDraft{Origin: "external", SourceRef: "test fixture", Source: dependency}, LuaSourceDigest(dependency), LuaSourceDigest(dependency)},
		{"operator", LuaDraft{Origin: "administrator", Source: source}, LuaSourceDigest(source), ""},
	}
	program, entries, err := CompileManagedLua(0, modules, []string{"observe", "edit", "invalid", "retarget", "multiple", "invalid_flag", "error_action"})
	if err != nil {
		t.Fatal(err)
	}
	// The test uses the original Lua engine without wiring a live packet hook.
	// The trusted test tail calls the generated callbacks with inert data.
	program += `
local original = dissect(parse_hex("4500001f00000000401100007f0000017f000001c001a157000b00006f6c64"))
assert(original.udp and original.payload == "old")
local packet = {dis = original, track = {private = function() end, lua_state = {private = true}}}
assert(solovey_managed_0_0(nil, packet) == VERDICT_PASS)
assert(packet.track.seen == nil and packet.track.lua_state.seen == nil)
assert(solovey_managed_0_1(nil, packet) == (VERDICT_MODIFY | VERDICT_PRESERVE_NEXT))
assert(original.payload == "old" and packet.dis.payload == "new")
assert(dissect(reconstruct_dissect(packet.dis)).payload == "new")
local committed = packet.dis
for index = 2, 6 do
    assert(_G["solovey_managed_0_" .. index](nil, packet) == VERDICT_DROP)
    assert(packet.dis == committed and packet.dis.payload == "new")
end
assert(not pcall(solovey_bind, "invalidtest", "missing_function"))
print("MANAGED_LUA_CALLBACKS_VERIFIED")
os.exit(0) -- The no-intercept engine otherwise waits in its timer loop.
`
	file := sealedEngineLua(t, program)
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

func TestPinnedEngineAppliesManagedLuaVerdicts(t *testing.T) {
	root, parent := os.Getenv("SOLOVEY_TEST_ENGINE_ROOT"), os.Getenv("SOLOVEY_TEST_PARENT_NETNS")
	if root == "" || parent == "" {
		t.Skip("requires the pinned engine and a disposable network namespace")
	}
	current, err := os.Readlink("/proc/self/ns/net")
	initNS, initErr := os.Readlink("/proc/1/ns/net")
	links, linkErr := net.Interfaces()
	if err != nil || initErr != nil || current == parent || current == initNS || linkErr != nil || len(links) != 1 || links[0].Name != "lo" {
		t.Fatal("refusing to install a managed Lua test queue outside a disposable loopback-only namespace")
	}
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	addr := listener.LocalAddr().(*net.UDPAddr)
	const source = `function edit(ctx, desync)
    if not desync.outgoing then return end
    if desync.dis.payload == "reject" then
        desync.dis.udp.uh_dport = desync.dis.udp.uh_dport + 1
    else
        desync.dis.payload = desync.dis.payload:upper()
    end
    return VERDICT_MODIFY
end`
	lua := LuaDraft{Origin: "administrator", Source: source}
	native := fmt.Sprintf("--filter-udp=%d --payload=all --lua-desync=edit", addr.Port)
	catalog := Catalog{Schema: CatalogSchema, Records: map[string]Record{}, LuaDrafts: map[string]LuaDraft{"operator": lua}}
	refs := []LuaReference{{ID: "operator", Revision: lua.Revision()}}
	record, err := catalog.SealDraft(Draft{ID: "managed-edit", Name: "Managed edit", Raw: &native, ManagedLua: refs})
	if err != nil {
		t.Fatal(err)
	}
	modules, err := catalog.SelectedLua(refs)
	if err != nil {
		t.Fatal(err)
	}
	const queue, mark, loop = 323, 0x120000, 0x40000000
	args, _, program, err := CompileManagedBindings(root, "/proc/self/fd/3", queue, loop, []MarkedStrategy{{Strategy: record.Definition, QueueMark: mark}},
		[][]LuaModule{modules}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	engine := exec.CommandContext(t.Context(), filepath.Join(root, "nfqws2"), append([]string{"--uid=65534:65534"}, args...)...)
	engine.Dir, engine.Stdout, engine.Stderr = root, &output, &output
	engine.ExtraFiles = []*os.File{sealedEngineLua(t, program)}
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = engine.Process.Kill()
		_ = engine.Wait()
		if t.Failed() {
			t.Log(output.String())
		}
	})
	waitForQueueCondition(t, func() bool { _, bound := queueDrops(t, queue); return bound })
	nft := exec.CommandContext(t.Context(), "nft", "-f", "-")
	nft.Stdin = strings.NewReader(fmt.Sprintf(`add table inet zapret_managed_fixture
add chain inet zapret_managed_fixture output { type filter hook output priority 0; policy accept; }
add rule inet zapret_managed_fixture output oifname "lo" ip daddr 127.0.0.1 udp dport %d queue num %d
`, addr.Port, queue))
	if out, err := nft.CombinedOutput(); err != nil {
		t.Fatalf("install isolated fixture queue: %v\n%s", err, out)
	}
	for _, tc := range []struct {
		mark       int
		send, want string
	}{{0, "unchanged", "unchanged"}, {mark, "edit", "EDIT"}, {mark, "reject", ""}, {mark, "recover", "RECOVER"}} {
		client, err := net.DialUDP("udp4", nil, addr)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		raw, err := client.SyscallConn()
		if err != nil {
			t.Fatal(err)
		}
		var markErr error
		if err := raw.Control(func(fd uintptr) {
			markErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, tc.mark)
		}); err != nil || markErr != nil {
			t.Fatalf("fixture mark: %v; %v", err, markErr)
		}
		if _, err := client.Write([]byte(tc.send)); err != nil {
			t.Fatal(err)
		}
		_ = listener.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var received [32]byte
		n, from, err := listener.ReadFromUDP(received[:])
		if tc.want == "" {
			if !os.IsTimeout(err) {
				t.Fatalf("rejected edit reached peer: %q, %v", received[:n], err)
			}
			continue
		}
		if err != nil || string(received[:n]) != tc.want || from.Port != client.LocalAddr().(*net.UDPAddr).Port {
			t.Fatalf("receiving %q from admitted flow: got %q from %v, %v", tc.want, received[:n], from, err)
		}
	}
	if _, bound := queueDrops(t, queue); !bound {
		t.Fatal("the managed runtime lost its queue")
	}
	t.Log("native managed packet edit, unselected path, rejection and recovery verified")
}

func sealedEngineLua(t *testing.T, program string) *os.File {
	t.Helper()
	fd, err := unix.MemfdCreate("solovey-test-zapret-lua", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), "solovey-test-zapret-lua")
	t.Cleanup(func() { _ = file.Close() })
	if _, err := file.WriteString(program); err != nil {
		t.Fatal(err)
	}
	if err := file.Chmod(0o444); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_SEAL|unix.F_SEAL_SHRINK|unix.F_SEAL_GROW|unix.F_SEAL_WRITE); err != nil {
		t.Fatal(err)
	}
	return file
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
