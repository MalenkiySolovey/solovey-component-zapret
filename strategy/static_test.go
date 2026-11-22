package strategy

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
)

func fixture(t *testing.T) Strategy {
	t.Helper()
	value, err := Seal(Strategy{
		Schema: Schema,
		ID:     "strategy-a",
		Assets: []AssetRef{Library, Actions},
		Profiles: []Profile{{
			ID: "tls-main", Protocol: intent.PacketProtocolTCP, Ports: []uint16{443},
			Payload: TLSClientHello, SplitAt: 1,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestSealAndCompileAreDeterministic(t *testing.T) {
	value := fixture(t)
	args, err := value.Compile("/opt/zapret2", 401, 0x120000, 0x40000000)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\x00")
	for _, want := range []string{"--qnum=401", "--fwmark=0x40000000", "--filter-mark=0x120000/0xffffffff", "--filter-tcp=443", "--payload=tls_client_hello"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled args do not contain %q: %v", want, args)
		}
	}
	copy := value
	copy.Profiles = append([]Profile(nil), value.Profiles...)
	copy.Profiles[0].Ports = []uint16{80}
	if next, err := Seal(copy); err != nil || next.Revision == value.Revision {
		t.Fatalf("semantic profile change did not change revision: %v", err)
	}
}

func TestSealRejectsInvalidNativeShapeAndReturnCapture(t *testing.T) {
	value := fixture(t)
	value.Profiles[0].SplitAt = 0
	if _, err := Seal(value); err == nil {
		t.Fatal("payload without a native action was accepted")
	}
	value = fixture(t)
	value.NeedsReturn = true
	if _, err := value.Compile("/opt/zapret2", 401, 0x120000, 0x40000000); err == nil {
		t.Fatal("unqualified return capture was activated")
	}
}

func TestCompileBindingsUseDisjointMarksAndOneLuaSetup(t *testing.T) {
	value := fixture(t)
	bindings := []MarkedStrategy{{value, 0x120000}, {value, 0x130000}}
	args, err := CompileBindings("/opt/zapret2", 401, 0x40000000, bindings)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(args, " ")
	if strings.Count(argv, "--qnum=") != 1 || strings.Count(argv, "--lua-init=") != 2 ||
		!strings.Contains(argv, "--name=b1-p1 --filter-mark=0x120000/0xffffffff --filter-tcp=443") ||
		!strings.Contains(argv, "--new=b2-p1 --filter-mark=0x130000/0xffffffff --filter-tcp=443") {
		t.Fatal(args)
	}
	bindings[1].QueueMark = bindings[0].QueueMark
	if _, err := CompileBindings("/opt/zapret2", 401, 0x40000000, bindings); err == nil {
		t.Fatal("duplicate input mark shared different grants")
	}
}
