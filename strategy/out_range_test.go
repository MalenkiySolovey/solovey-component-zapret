package strategy

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
)

const rangedActionFixture = "\t--name=ranged --filter-tcp=80\r\n" +
	"--filter-l7=http --payload=http_req --out-range=-d1 --lua-desync=http_domcase\n" +
	"--out-range=a --lua-desync=http_hostcase:spell=hOsT\n" +
	"--new=quic --filter-udp=443 --filter-l7=quic --payload=quic_initial --out-range=x " +
	"--lua-desync=fake:blob=fake_default_quic:repeats=1\n" +
	"--new=tls --filter-tcp=443 --filter-l7=tls --payload=tls_client_hello --lua-desync=multisplit:pos=9\n"

func TestOutgoingActionRangesRoundTripAndTrace(t *testing.T) {
	raw := rangedActionFixture
	record, err := SealDraft(Draft{ID: "out-range", Name: "Action ranges", Raw: &raw})
	if err != nil {
		t.Fatal(err)
	}
	value := record.Definition
	profiles := value.Profiles
	if record.Raw != raw || profiles[0].HTTPActions[0].OutRange != "-d1" || profiles[0].HTTPActions[1].OutRange != "" ||
		profiles[1].OutRange != "x" || profiles[2].OutRange != "" {
		t.Fatalf("native ranges did not follow action order or reset between profiles: %+v", profiles)
	}
	imported, err := record.Export().Import()
	if err != nil || imported.Raw == nil || *imported.Raw != raw {
		t.Fatalf("transfer lost source: %+v, %v", imported, err)
	}
	visual, err := SealDraft(Draft{ID: value.ID, Name: record.Name, Profiles: profiles})
	if err != nil || visual.Definition.Revision != value.Revision {
		t.Fatalf("visual edit lost range semantics: %+v, %v", visual, err)
	}
	scope, err := value.PacketScope()
	if err != nil || scope.NeedsReturn || !slices.Equal(scope.TCP, []uint16{80, 443}) || !slices.Equal(scope.UDP, []uint16{443}) {
		t.Fatalf("action filter changed broker scope: %+v, %v", scope, err)
	}
	args, _, err := CompileBindingsWithNativeTrace("/engine", 400, 0x40000000, []MarkedStrategy{{Strategy: value, QueueMark: 0x120000}})
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "--out-range=") || strings.HasPrefix(arg, "--lua-desync=") || strings.HasPrefix(arg, "--new=") {
			actions = append(actions, arg)
		}
	}
	const trace = "--lua-desync=solovey_profile_match:slot=1/"
	want := []string{"--out-range=-d1", "--lua-desync=http_domcase", "--out-range=a", "--lua-desync=http_hostcase:spell=hOsT",
		trace + "1", "--out-range=-d1", trace + "1",
		"--new=quic", "--out-range=x", "--lua-desync=fake:blob=fake_default_quic:repeats=1", trace + "2",
		"--new=tls", "--lua-desync=multisplit:pos=9", trace + "3"}
	if !slices.Equal(actions, want) {
		t.Fatalf("got %q, want %q", actions, want)
	}
	inherit := strings.Replace(raw, "--out-range=a ", "", 1)
	other, err := ParseNative(value.ID, inherit)
	if err != nil || other.Profiles[0].HTTPActions[1].OutRange != "-d1" || other.Revision == value.Revision {
		t.Fatalf("later HTTP action lost inherited range: %+v, %v", other, err)
	}
}

func TestOutgoingRangeCannotClaimUnconditionalQUICCoverage(t *testing.T) {
	const prefix = "--filter-udp=443 --filter-l7=quic --payload=quic_initial "
	const action = "--lua-desync=fake:blob=fake_default_quic:repeats=1"
	for _, filter := range []string{"", "--out-range=a ", "--out-range=d2- ", "--out-range=x "} {
		// Even with a later unconditional action, an earlier ranged action
		// selects the profile independently of its packet range.
		raw := prefix + filter + action + "\n--new\n" + prefix + action
		value, err := ParseNative("quic", raw)
		want := filter == "" || filter == "--out-range=a "
		if err != nil || value.HandlesQUIC(IPv4, 443) != want || value.HandlesQUIC(IPv6, 443) != want {
			t.Fatalf("unconditional QUIC coverage with %q = %v, want %v: %v", filter, value.HandlesQUIC(IPv4, 443), want, err)
		}
	}
}

func TestOutgoingRangeRejectsUnqualifiedForms(t *testing.T) {
	for _, good := range []string{"-d3", "<n5", "n2<n8", "b100-", "s0-p2147483647", "-b4294967295", "a", "x"} {
		if _, err := sealOutRange(good, intent.PacketProtocolTCP); err != nil {
			t.Errorf("rejected %q: %v", good, err)
		}
	}
	for _, bad := range []string{"-", "n2", "n2<", "-d3tail", "n2--d3", "-d4294967296", "-p2147483648", "x-n2", "-d+2", "-n00000000000"} {
		if _, err := sealOutRange(bad, intent.PacketProtocolTCP); err == nil {
			t.Errorf("admitted %q", bad)
		}
	}
	for _, bad := range []string{
		"--filter-tcp=80 --out-range=x",
		rangedActionFixture + "--out-range=-d2",
		strings.Replace(rangedActionFixture, "--out-range=x", "--out-range=-s2", 1),
		strings.Replace(circularFixture, "--in-range=-n32", "--out-range=x --in-range=-n32", 1),
	} {
		if _, err := ParseNative("out-range", bad); !errors.Is(err, ErrInvalidStrategy) {
			t.Errorf("admitted an unqualified range in %q: %v", bad, err)
		}
	}
	_, err := SealDraft(Draft{ID: "bad", Name: "Invalid range", Profiles: []Profile{{
		ID: "passive", Protocol: intent.PacketProtocolTCP, Ports: []uint16{80}, OutRange: "x",
	}}})
	if !errors.Is(err, ErrInvalidStrategy) {
		t.Fatalf("visual draft admitted an action range on a passive profile: %v", err)
	}
}
