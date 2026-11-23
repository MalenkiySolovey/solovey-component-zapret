package strategy

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestNativeListsRoundTrip(t *testing.T) {
	raw := "--filter-tcp=443 --hostlist-domains=b.test,A.test,a.test --hostlist-exclude-domains=plain.a.test " +
		"--ipset-ip=2001:db8::1/64,192.0.2.1 --ipset-exclude-ip=192.0.2.2"
	record, err := SealDraft(Draft{ID: "fixture", Name: "Lists", Raw: &raw})
	if err != nil {
		t.Fatal(err)
	}
	imported, err := record.Export().Import()
	if err != nil || imported.Raw == nil || *imported.Raw != raw {
		t.Fatalf("lost the original static lists on transfer: %v", err)
	}
	text, err := FormatNative(record.Definition)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := ParseNative("fixture", text)
	if err != nil || roundTrip.Revision != record.Definition.Revision {
		t.Fatalf("visual conversion changed the list revision: %v", err)
	}
	argv, err := roundTrip.Compile("/usr/local/lib/zapret", 401, 0x120000, 0x40000000)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{"--filter-mark=0x120000/0xffffffff", "--hostlist-domains=a.test,b.test",
		"--hostlist-exclude-domains=plain.a.test", "--ipset-ip=192.0.2.1/32,2001:db8::/64", "--ipset-exclude-ip=192.0.2.2/32"} {
		if !slices.Contains(argv, arg) {
			t.Errorf("missing native option %q in %v", arg, argv)
		}
	}
}

func TestListRestrictedQUICDoesNotPromiseDNSCoverage(t *testing.T) {
	const quic = "--filter-udp=853 --filter-l7=quic --payload=quic_initial --lua-desync=fake:blob=fake_default_quic:repeats=1"
	for _, test := range []struct {
		raw  string
		want bool
	}{
		{quic + " --hostlist-domains=resolver.test", false},
		{quic + " --ipset-exclude-ip=192.0.2.10 --new " + quic, true},
		{"--filter-udp=853 --ipset-ip=192.0.2.10 --new " + quic, false},
	} {
		value, err := ParseNative("fixture", test.raw)
		if err != nil || value.HandlesQUIC(IPv4, 853) != test.want {
			t.Errorf("want coverage %v for %q: %v", test.want, test.raw, err)
		}
	}
}

func TestNativeCircularReportsUnavailableReturnObservation(t *testing.T) {
	_, err := ParseNative("fixture", "--filter-tcp=443 --lua-desync=circular:fails=4")
	if !errors.Is(err, errReturnObservationUnavailable) {
		t.Fatalf("want a return-observation rejection, got %v", err)
	}
}

func TestNativeProfileRoundTrip(t *testing.T) {
	raw := "\t--name=tls-first  --filter-tcp=853,443\r\n--filter-l7=tls --payload=tls_client_hello\n--lua-desync=multisplit:pos=9\n--new=quic\n--filter-udp=853,443\n--filter-l3=ipv6\n--filter-l7=quic\n--payload=quic_initial\n--lua-desync=fake:blob=fake_default_quic:repeats=2\n--new=passthrough\n--filter-tcp=8080\n"
	value, err := ParseNative("fixture", raw)
	if err != nil {
		t.Fatal(err)
	}
	formatted, err := FormatNative(value)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := ParseNative("fixture", formatted)
	if err != nil || reloaded.Revision != value.Revision || reloaded.Profiles[0].ID != "tls-first" ||
		reloaded.Profiles[1].QUICRepeats != 2 || reloaded.Profiles[1].Family != IPv6 || reloaded.Profiles[2].ID != "passthrough" {
		t.Fatalf("native profile semantics changed after a visual round trip: %v", err)
	}
	v4, err := value.PacketScopeForFamily(IPv4)
	if err != nil || len(v4.UDP) != 0 {
		t.Fatalf("IPv6-only UDP filter leaked to IPv4: %v, %v", v4, err)
	}
	v6, err := value.PacketScopeForFamily(IPv6)
	if err != nil || len(v6.UDP) != 2 || len(v6.TCP) != 3 {
		t.Fatalf("IPv6 scope lost unrestricted or IPv6-only profiles: %v, %v", v6, err)
	}
	if !value.HandlesQUIC(IPv6, 443) || value.HandlesQUIC(IPv4, 443) {
		t.Fatal("QUIC coverage ignored the native L3 filter")
	}
	shadowed, err := ParseNative("fixture", "--filter-udp=443\n--new\n"+strings.SplitN(raw, "--new=quic", 2)[1])
	if err != nil || shadowed.HandlesQUIC(IPv6, 443) || !shadowed.HandlesQUIC(IPv6, 853) {
		t.Fatalf("QUIC coverage did not respect ordered native profiles: %v", err)
	}
}

func TestNativeProfileRejectsExecutorAndAuthorityOptions(t *testing.T) {
	for _, raw := range []string{
		"--filter-tcp=443 --lua-init=@/tmp/local.lua",
		"--filter-tcp=443 --filter-mark=123",
		"--filter-tcp=443 --qnum=11",
		"--filter-tcp=443 --hostlist=/etc/hosts",
		"--filter-tcp=443 --hostlist-domains=/etc/hosts",
		"--filter-tcp=443 --ipset=@/etc/hosts",
		"--filter-tcp=443 --hostlist-domains=a.test,,b.test",
		"--filter-tcp=443 --hostlist-domains=^a.test",
		"--filter-tcp=443 --ipset-ip=fe80::1%eth0",
		"--filter-tcp=443 --ipset-exclude-ip=2001:db8::1/129",
		"--filter-tcp=443 --hostlist-domains=" + strings.Repeat("a.test,", maxNativeListEntries) + "b.test",
		"--filter-tcp=443 --hostlist-domains=" + strings.Repeat(strings.Repeat("a", 63)+".test,", maxNativeListEntries-1) + "b.test",
		"--filter-tcp=443 --filter-l3=all",
		"--filter-udp=443 --filter-tcp=443",
		"--filter-udp=443 --filter-l7=quic --payload=quic_initial --lua-desync=fake:blob=fake_default_quic:repeats=5",
		"--filter-udp=443 --filter-l7=quic --payload=quic_initial --lua-desync=fake:blob=other:repeats=1",
		"--filter-tcp=443 --filter-l7=quic --payload=quic_initial --lua-desync=fake:blob=fake_default_quic:repeats=1",
		"--filter-udp=443 --filter-l7=tls --payload=tls_client_hello --lua-desync=multisplit:pos=9",
		"--filter-tcp=443 --filter-tcp=80",
		"--filter-tcp=443 --filter-l7=tls",
		"--filter-tcp=443 --filter-l7=http --payload=tls_client_hello --lua-desync=multisplit:pos=9",
		"--filter-tcp=443 --filter-l7=tls --payload=tls_client_hello --lua-desync=multisplit:pos=9:extra=1",
		"--filter-tcp=443 --new",
		strings.Repeat(" ", MaxNativeBytes+1),
	} {
		if _, err := ParseNative("fixture", raw); !errors.Is(err, ErrInvalidStrategy) {
			t.Fatalf("accepted unqualified native profile %q: %v", raw, err)
		}
	}
}
