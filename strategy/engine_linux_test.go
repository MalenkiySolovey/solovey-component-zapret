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
	argv, _, err = CompileBindingsForReturnPath(root, 401, 0x40000000, bindings, true)
	if err != nil {
		t.Fatal(err)
	}
	validateEngineArgs(t, root, argv)
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
