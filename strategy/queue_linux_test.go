package strategy

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPinnedEngineQueueOverloadDrops(t *testing.T) {
	root, parent := os.Getenv("SOLOVEY_TEST_ENGINE_ROOT"), os.Getenv("SOLOVEY_TEST_PARENT_NETNS")
	if root == "" || parent == "" {
		t.Skip("requires an independently built engine and a disposable Linux network namespace")
	}
	current, err := os.Readlink("/proc/self/ns/net")
	initNS, initErr := os.Readlink("/proc/1/ns/net")
	links, linkErr := net.Interfaces()
	if err != nil || initErr != nil || current == parent || current == initNS || linkErr != nil || len(links) != 1 || links[0].Name != "lo" {
		t.Fatal("refusing to install the test queue outside a disposable loopback-only namespace")
	}
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	client, err := net.DialUDP("udp4", nil, listener.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	const queue = 321
	var logs bytes.Buffer
	engine := exec.CommandContext(t.Context(), filepath.Join(root, "nfqws2"), fmt.Sprintf("--qnum=%d", queue), "--filter-l4=udp")
	engine.Dir = root
	engine.Stdout, engine.Stderr = &logs, &logs
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = engine.Process.Kill()
		_ = engine.Wait()
		if t.Failed() {
			t.Log(logs.String())
		}
	})
	var before uint64
	waitForQueueCondition(t, func() bool {
		var bound bool
		before, bound = queueDrops(t, queue)
		return bound
	})
	nft := exec.CommandContext(t.Context(), "nft", "-f", "-")
	nft.Stdin = strings.NewReader(fmt.Sprintf(`add table inet r8b1_queue_fixture
add chain inet r8b1_queue_fixture output { type filter hook output priority 0; policy accept; }
add rule inet r8b1_queue_fixture output oifname "lo" ip daddr 127.0.0.1 udp dport %d queue num %d
`, listener.LocalAddr().(*net.UDPAddr).Port, queue))
	if output, err := nft.CombinedOutput(); err != nil {
		t.Fatalf("install scoped fixture rule: %v\n%s", err, output)
	}
	if _, err := client.Write([]byte("ready")); err != nil {
		t.Fatal(err)
	}
	_ = listener.SetReadDeadline(time.Now().Add(2 * time.Second))
	var received [64]byte
	if n, _, err := listener.ReadFromUDP(received[:]); err != nil || string(received[:n]) != "ready" {
		t.Fatalf("live consumer did not accept the control datagram: %v", err)
	}
	if err := engine.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	waitForQueueCondition(t, func() bool {
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", engine.Process.Pid))
		return err == nil && strings.Contains(string(raw), "State:\tT")
	})
	var after uint64
	for i := 0; i < 128; i++ {
		for range 128 {
			// The full queue can surface a synchronous drop error on UDP.
			_, _ = client.Write([]byte("must not reach the wire"))
		}
		var bound bool
		if after, bound = queueDrops(t, queue); !bound {
			t.Fatal("consumer lost its queue instead of overflowing it")
		}
		if after > before {
			break
		}
	}
	_ = listener.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if n, _, err := listener.ReadFromUDP(received[:]); err == nil || !os.IsTimeout(err) {
		t.Fatalf("queue overflow did not fail closed: received %d bytes, err=%v", n, err)
	}
	if after <= before {
		t.Fatal("the stopped consumer's queue never reported an overload drop")
	}
	t.Logf("live UDP control delivered; overload drops=%d; leaked datagrams=0", after-before)
}

func waitForQueueCondition(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the NFQUEUE fixture")
}

func queueDrops(t *testing.T, queue int) (uint64, bool) {
	t.Helper()
	raw, err := os.ReadFile("/proc/net/netfilter/nfnetlink_queue")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != strconv.Itoa(queue) {
			continue
		}
		if len(fields) < 8 {
			t.Fatalf("invalid NFQUEUE observation: %q", line)
		}
		dropped, queueErr := strconv.ParseUint(fields[5], 10, 64)
		userspaceDropped, userErr := strconv.ParseUint(fields[6], 10, 64)
		if queueErr != nil || userErr != nil {
			t.Fatalf("invalid NFQUEUE drop counters: %q", line)
		}
		// A queue is ready only after COPY_PACKET mode has been configured.
		return dropped + userspaceDropped, fields[3] == "2"
	}
	return 0, false
}
