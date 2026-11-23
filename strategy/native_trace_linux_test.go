package strategy

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
)

func TestPinnedEngineReportsMatchedNativeProfiles(t *testing.T) {
	root, parent := os.Getenv("SOLOVEY_TEST_ENGINE_ROOT"), os.Getenv("SOLOVEY_TEST_PARENT_NETNS")
	if root == "" || parent == "" {
		t.Skip("requires the pinned engine and a disposable network namespace")
	}
	current, err := os.Readlink("/proc/self/ns/net")
	initNS, initErr := os.Readlink("/proc/1/ns/net")
	links, linkErr := net.Interfaces()
	if err != nil || initErr != nil || current == parent || current == initNS || linkErr != nil || len(links) != 1 || links[0].Name != "lo" {
		t.Fatal("refusing to install a trace test queue outside a disposable loopback-only namespace")
	}
	initial, err := os.ReadFile(os.Getenv("SOLOVEY_TEST_QUIC_INITIAL"))
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(initial)) != "f4589c57749f956bb30538197a521d7005f8b0a8723b4707e72405e51ddac50a" {
		t.Fatal("missing the pinned upstream QUIC Initial fixture")
	}
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	addr := listener.LocalAddr().(*net.UDPAddr)
	profile, err := Seal(Strategy{Schema: Schema, ID: "trace-fixture", Assets: []AssetRef{Library, Actions}, Profiles: []Profile{{
		ID: "quic-action", Protocol: intent.PacketProtocolUDP, Ports: []uint16{uint16(addr.Port)},
		Payload: QUICInitial, QUICRepeats: 1, QUICBlobRef: "fixture",
	}}, BlobData: map[string]string{"fixture": "405549"}})
	if err != nil {
		t.Fatal(err)
	}
	const queue, loop = 322, 0x40000000
	args, trace, err := CompileBindingsWithNativeTrace(root, queue, loop, []MarkedStrategy{
		{Strategy: profile, QueueMark: 0x120000}, {Strategy: profile, QueueMark: 0x130000},
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	engine := exec.CommandContext(t.Context(), filepath.Join(root, "nfqws2"), args...)
	engine.Dir, engine.Stdout, engine.Stderr = root, io.MultiWriter(&stdout, trace), os.Stderr
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Process.Kill(); _ = engine.Wait() }()
	waitForQueueCondition(t, func() bool { _, bound := queueDrops(t, queue); return bound })
	nft := exec.CommandContext(t.Context(), "nft", "-f", "-")
	nft.Stdin = strings.NewReader(fmt.Sprintf(`add table inet zapret_trace_fixture
add chain inet zapret_trace_fixture output { type filter hook output priority 0; policy accept; }
add rule inet zapret_trace_fixture output oifname "lo" ip daddr 127.0.0.1 udp dport %d meta mark & 0x%x == 0 queue num %d
`, addr.Port, loop, queue))
	if output, err := nft.CombinedOutput(); err != nil {
		t.Fatalf("install isolated test queue: %v\n%s", err, output)
	}
	for _, mark := range []int{0, 0x130000, 0x120000, 0x130000, 0x120000, 0} {
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
		if err := raw.Control(func(fd uintptr) { markErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, mark) }); err != nil || markErr != nil {
			t.Fatalf("fixture socket mark: %v; %v", err, markErr)
		}
		if _, err := client.Write(initial); err != nil {
			t.Fatal(err)
		}
		_ = listener.SetReadDeadline(time.Now().Add(2 * time.Second))
		var received [1500]byte
		var originals, generated int
		for range 1 + boolInt(mark != 0) {
			n, _, err := listener.ReadFromUDP(received[:])
			switch {
			case err != nil:
				t.Fatalf("receive mark %x: %v (original=%d generated=%d)", mark, err, originals, generated)
			case bytes.Equal(received[:n], initial):
				originals++
			case bytes.Equal(received[:n], []byte{0x40, 0x55, 0x49}):
				generated++
			default:
				t.Fatalf("unexpected %d-byte datagram for mark %x", n, mark)
			}
		}
		if originals != 1 || generated != boolInt(mark != 0) {
			t.Fatalf("mark %x: original=%d generated=%d", mark, originals, generated)
		}
	}
	_ = listener.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, _, err := listener.ReadFromUDP(make([]byte, 1)); !os.IsTimeout(err) {
		t.Fatalf("unexpected extra native packet: %v", err)
	}
	_ = engine.Process.Kill()
	_ = engine.Wait()
	var slots [][2]int
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		if b, p, ok := parseNativeTrace(line); ok {
			slots = append(slots, [2]int{b, p})
		}
	}
	if !slices.Equal(slots, [][2]int{{2, 1}, {1, 1}}) {
		t.Fatalf("unexpected native profile observations: %v\n%s", slots, stdout.String())
	}
	if !slices.Equal(trace.MatchedProfiles(0), []string{"quic-action"}) || !slices.Equal(trace.MatchedProfiles(1), []string{"quic-action"}) {
		t.Fatal("trace did not resolve both profiles to their verified IDs")
	}
	t.Log("one observation per marked profile; four native-generated packets, six original datagrams intact, no unmarked generation")
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
