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
)

func TestPinnedEngineValidatesProfiles(t *testing.T) {
	root := os.Getenv("SOLOVEY_TEST_ENGINE_ROOT")
	if root == "" {
		t.Skip("requires independently built nfqws2 engine input")
	}
	value := fixture(t)
	value.Profiles = append(value.Profiles, Profile{ID: "udp-pass", Protocol: intent.PacketProtocolUDP, Ports: []uint16{8443}})
	value, err := Seal(value)
	if err != nil {
		t.Fatal(err)
	}
	argv, err := CompileBindings(root, 401, 0x40000000, []MarkedStrategy{
		{Strategy: value, QueueMark: 0x120000}, {Strategy: value, QueueMark: 0x130000},
	})
	if err != nil {
		t.Fatal(err)
	}
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
