package strategy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
	argv, err := value.Compile(root, 401, 0x120000, 0x40000000)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for _, prefix := range [][]string{{"--dry-run", "--intercept=0"}, {"--intercept=0"}} {
		cmd := exec.CommandContext(ctx, filepath.Join(root, "nfqws2"), append(prefix, argv...)...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("nfqws2 %v: %v\n%s", prefix, err, out)
		}
		t.Logf("nfqws2 %v:\n%s", prefix, out)
	}
}
