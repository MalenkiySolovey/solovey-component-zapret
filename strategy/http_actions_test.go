package strategy

import (
	"slices"
	"strings"
	"testing"
)

const nativeHTTPFixture = "\t--name=ordered --filter-tcp=80\r\n" +
	"--filter-l7=http --payload=http_req\n" +
	"--lua-desync=http_domcase --lua-desync=http_hostcase:spell=hOsT\n"

func TestNativeHTTPActionsPreserveOrderAndRaw(t *testing.T) {
	raw := nativeHTTPFixture
	record, err := SealDraft(Draft{ID: "fixture-http", Name: "Ordered HTTP", Raw: &raw})
	if err != nil {
		t.Fatal(err)
	}
	actions := []HTTPAction{{Name: HTTPDomainCase}, {Name: HTTPHostCase, Spell: "hOsT"}}
	if record.Raw != raw || !slices.Equal(record.Definition.Profiles[0].HTTPActions, actions) || record.Definition.NeedsReturn {
		t.Fatalf("unexpected record: %+v", record)
	}
	imported, err := record.Export().Import()
	if err != nil || imported.Raw == nil || *imported.Raw != raw {
		t.Fatalf("native transfer lost source: %+v, %v", imported, err)
	}
	canonical, err := SealDraft(Draft{ID: "fixture-http", Name: record.Name, Profiles: record.Definition.Profiles})
	if err != nil || canonical.Definition.Revision != record.Definition.Revision {
		t.Fatalf("ordered profiles did not round trip: %+v, %v", canonical, err)
	}
	argv, err := record.Definition.Compile("/engine", 401, 0x120000, 0x40000000)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--filter-mark=0x120000/0xffffffff", "--filter-tcp=80", "--filter-l7=http", "--payload=http_req",
		"--lua-desync=http_domcase", "--lua-desync=http_hostcase:spell=hOsT"}
	if !slices.Equal(argv[len(argv)-len(want):], want) {
		t.Fatalf("got argv %q, want ordered suffix %q", argv, want)
	}
	reversed := strings.Replace(raw, "--lua-desync=http_domcase --lua-desync=http_hostcase:spell=hOsT",
		"--lua-desync=http_hostcase:spell=hOsT --lua-desync=http_domcase", 1)
	other, err := ParseNative("fixture-http", reversed)
	if err != nil || other.Revision == record.Definition.Revision || other.Profiles[0].HTTPActions[0] != actions[1] {
		t.Fatalf("order not represented: %+v, %v", other, err)
	}
}

func TestNativeHTTPActionsRejectUnsafeCombinations(t *testing.T) {
	for _, text := range []string{
		strings.Replace(nativeHTTPFixture, "hOsT", "Name", 1),
		strings.Replace(nativeHTTPFixture, "hOsT", "hOsT:direction=in", 1),
		strings.Replace(nativeHTTPFixture, "http_domcase", "condition:cond=fixture", 1),
		strings.Replace(nativeHTTPFixture, "http_domcase", "http_hostcase", 1),
		strings.Replace(nativeHTTPFixture, "http_domcase", "multisplit:pos=9", 1),
		strings.Replace(nativeHTTPFixture, "--payload=http_req", "--in-range=-n32 --payload=http_req", 1),
		strings.Replace(nativeHTTPFixture, "--payload=http_req", "--payload=tls_client_hello", 1),
		strings.Replace(nativeHTTPFixture, "http_hostcase:spell=hOsT", "send", 1),
		nativeHTTPFixture + "--lua-init=@/tmp/foreign.lua",
	} {
		if _, err := ParseNative("fixture-http", text); err == nil {
			t.Errorf("admitted unqualified HTTP action source %q", text)
		}
	}
}
