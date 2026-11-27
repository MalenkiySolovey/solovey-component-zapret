package strategy

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
)

func rangedProfileFixture() string {
	raw := "  --name=ranged --filter-tcp=8000-8039,65534-65535\r\n"
	for i := range 17 {
		raw += fmt.Sprintf("--new=extra-%d --filter-tcp=%d\n", i, 8000+i)
	}
	return raw
}

func TestNativeRangesPreserveSourceProfileOrderAndExactPorts(t *testing.T) {
	raw := rangedProfileFixture()
	record, err := SealDraft(Draft{ID: "fixture", Name: "Ranged profiles", Raw: &raw})
	if err != nil {
		t.Fatal(err)
	}
	value := record.Definition
	if len(value.Profiles) != 18 || value.Profiles[0].ID != "ranged" || value.Profiles[17].ID != "extra-16" {
		t.Fatal("native profile order changed")
	}
	scope, err := value.PacketScope()
	if err != nil || len(scope.TCP) != 42 || scope.TCP[0] != 8000 || scope.TCP[39] != 8039 || scope.TCP[41] != 65535 || len(scope.UDP) != 0 {
		t.Fatalf("wrong exact Host port scope: %+v, %v", scope, err)
	}
	transfer, err := record.Export().Import()
	if err != nil || transfer.Raw == nil || *transfer.Raw != raw {
		t.Fatalf("export did not preserve the original source: %v", err)
	}
	visual, err := SealDraft(Draft{ID: "fixture", Name: record.Name, Profiles: value.Profiles})
	if err != nil || visual.Definition.Revision != value.Revision {
		t.Fatalf("visual save changed the ordered native definition: %v", err)
	}
	argv, err := value.Compile("/usr/local/lib/zapret", 401, 0x120000, 0x40000000)
	if err != nil || !slices.Contains(argv, "--filter-tcp=8000-8039,65534-65535") {
		t.Fatalf("engine did not receive the bounded range: %v, %v", argv, err)
	}
}

func TestNativeRangesRejectUnboundedOrMalformedPorts(t *testing.T) {
	for _, input := range []string{"*", "~443", "0-2", "443-42", "65535-65536", "443-444-445", "443,"} {
		if _, err := ParseNative("fixture", "--filter-tcp="+input); !errors.Is(err, ErrInvalidStrategy) {
			t.Errorf("accepted invalid or ambiguous port filter %q: %v", input, err)
		}
	}
}

func TestWideNativeRangesStayCompactAcrossParsingAndScope(t *testing.T) {
	raw := "--filter-tcp=8080-65535 --filter-udp=100-164 --new --filter-udp=164-200"
	record, err := SealDraft(Draft{ID: "wide", Name: "Wide ranges", Raw: &raw})
	if err != nil {
		t.Fatal(err)
	}
	value := record.Definition
	scope, err := value.PacketScope()
	if err != nil || len(scope.TCP) != 0 || len(scope.UDP) != 0 ||
		!slices.Equal(scope.TCPRanges, []intent.PortInterval{{First: 8080, Last: 65535}}) ||
		!slices.Equal(scope.UDPRanges, []intent.PortInterval{{First: 100, Last: 200}}) {
		t.Fatalf("mixed scope expanded or gained ports: %+v: %v", scope, err)
	}
	argv, err := value.Compile("/opt/zapret2", 401, 0x120000, 0x40000000)
	if err != nil || !slices.Contains(argv, "--filter-tcp=8080-65535") || !slices.Contains(argv, "--filter-udp=100-164") {
		t.Fatalf("lost compact per-profile filter: %v: %v", argv, err)
	}
	visual, err := SealDraft(Draft{ID: "wide", Name: record.Name, Profiles: value.Profiles})
	if err != nil || visual.Definition.Revision != value.Revision {
		t.Fatalf("visual roundtrip changed a wide range: %v", err)
	}
	transfer, err := record.Export().Import()
	if err != nil || transfer.Raw == nil || *transfer.Raw != raw {
		t.Fatalf("export lost the original wide filters: %v", err)
	}
}

const dualTransportFixture = "--name=passive\n--filter-tcp=8000-8002\n--filter-udp=443\n--filter-l3=ipv6\n" +
	"--ipset-exclude-ip=2001:db8::1/128\n--new=quic\n--filter-tcp=8443\n--filter-udp=443,853\n" +
	"--filter-l7=quic\n--payload=quic_initial\n--lua-desync=fake:blob=fake_default_quic:repeats=1\n"

func TestNativeDualTransportProfiles(t *testing.T) {
	raw := dualTransportFixture
	record, err := SealDraft(Draft{ID: "fixture", Name: "Dual transport", Raw: &raw})
	if err != nil || !record.VisualEditable() {
		t.Fatalf("canonical dual-transport source must be visually editable: %v", err)
	}
	value := record.Definition
	otherOrder := strings.Replace(raw, "--filter-tcp=8000-8002\n--filter-udp=443", "--filter-udp=443\n--filter-tcp=8000-8002", 1)
	otherOrder = strings.Replace(otherOrder, "--filter-tcp=8443\n--filter-udp=443,853", "--filter-udp=443,853\n--filter-tcp=8443", 1)
	other, err := SealDraft(Draft{ID: "fixture", Name: record.Name, Raw: &otherOrder})
	if err != nil || other.Definition.Revision != value.Revision || other.VisualEditable() || other.Raw != otherOrder {
		t.Fatalf("flag order changed runtime semantics or original source was lost: %v", err)
	}
	visual, err := SealDraft(Draft{ID: "fixture", Name: record.Name, Profiles: value.Profiles})
	if err != nil || visual.Definition.Revision != value.Revision || visual.Raw != raw {
		t.Fatalf("visual round trip lost a port filter or changed order: %v", err)
	}
	imported, err := other.Export().Import()
	if err != nil || imported.Raw == nil || *imported.Raw != otherOrder {
		t.Fatalf("transfer did not preserve the original filter order: %v", err)
	}
	for _, tc := range []struct {
		family AddressFamily
		tcp    []uint16
	}{{IPv4, []uint16{8443}}, {IPv6, []uint16{8000, 8001, 8002, 8443}}} {
		scope, err := value.PacketScopeForFamily(tc.family)
		if err != nil || !slices.Equal(scope.TCP, tc.tcp) || !slices.Equal(scope.UDP, []uint16{443, 853}) || scope.NeedsReturn {
			t.Errorf("%s scope mixed transport/family or gained return traffic: %+v, %v", tc.family, scope, err)
		}
	}
	if !value.HandlesQUIC(IPv4, 443) || value.HandlesQUIC(IPv6, 443) || !value.HandlesQUIC(IPv6, 853) {
		t.Fatal("QUIC coverage ignored the earlier mixed passive profile or matched the wrong family/port")
	}
	argv, err := value.Compile("/opt/zapret2", 401, 0x120000, 0x40000000)
	if err != nil {
		t.Fatal(err)
	}
	first := slices.Index(argv, "--filter-mark=0x120000/0xffffffff")
	want := []string{"--filter-mark=0x120000/0xffffffff", "--filter-tcp=8000-8002", "--filter-udp=443", "--filter-l3=ipv6",
		"--ipset-exclude-ip=2001:db8::1/128", "--new=quic", "--filter-mark=0x120000/0xffffffff",
		"--filter-tcp=8443", "--filter-udp=443,853", "--filter-l7=quic", "--payload=quic_initial", "--lua-desync=fake:blob=fake_default_quic:repeats=1"}
	if first < 0 || !slices.Equal(argv[first:], want) {
		t.Fatalf("combined filters were split or the exclusion or mark was lost: %v", argv)
	}
}

func TestNativeDualTransportL7DoesNotShadowQUIC(t *testing.T) {
	const source = "--filter-tcp=443 --filter-udp=853 --filter-l7=tls --payload=tls_client_hello --lua-desync=multisplit:pos=9 " +
		"--new=quic --filter-udp=853 --filter-l7=quic --payload=quic_initial --lua-desync=fake:blob=fake_default_quic:repeats=1"
	value, err := ParseNative("fixture", source)
	if err != nil || !value.HandlesQUIC(IPv6, 853) {
		t.Fatalf("TCP/TLS action in a mixed profile cannot shadow QUIC: %v", err)
	}
}

const nativePassiveFixture = "--name=observe\n--filter-tcp=18080\n--filter-udp=18443\n--filter-l7=http,dns\n" +
	"--payload=http_req,dns_query\n--out-range=-d3\n--lua-desync=pass\n--new=other\n--filter-tcp=18080\n--filter-l7=unknown\n"

func TestNativePassivePresetAndPassRoundTrip(t *testing.T) {
	input := PresetInput{Format: "native", Content: []byte(nativePassiveFixture)}
	catalog, review, err := emptyPresetCatalog().PreparePreset("fixture", input)
	if err != nil || review.Record == nil || review.Record.VisualEditable() || review.Record.Raw != nativePassiveFixture {
		t.Fatalf("pass and independent L7 selection must be lossless native options: %v, %+v", err, review)
	}
	value := review.Record.Definition
	if !value.Profiles[0].Pass || value.Profiles[0].Payload != "http_req,dns_query" || value.Profiles[1].Pass || value.Profiles[1].L7Filter != "unknown" {
		t.Fatalf("native pass/selection lost: %+v", value.Profiles)
	}
	argv, err := value.Compile("/opt/zapret2", 401, 0x120000, 0x40000000)
	want := "--filter-tcp=18080 --filter-udp=18443 --filter-l7=http,dns --payload=http_req,dns_query --out-range=-d3 --lua-desync=pass " +
		"--new=other --filter-mark=0x120000/0xffffffff --filter-tcp=18080 --filter-l7=unknown"
	if err != nil || !strings.HasSuffix(strings.Join(argv, " "), want) {
		t.Fatalf("native selection, filter or action was not preserved in engine argv: %v, %v", argv, err)
	}
	data, err := catalog.ExportPackage("fixture")
	if err != nil {
		t.Fatal(err)
	}
	_, restored, err := emptyPresetCatalog().PreparePreset("fixture", PresetInput{Format: "zip", Content: data})
	if err != nil || restored.Record == nil || restored.Record.Raw != nativePassiveFixture || restored.Record.Definition.Revision != value.Revision {
		t.Fatalf("sealed ZIP did not preserve the native profile: %v, %+v", err, restored)
	}
}

func TestNativePassiveL7CoverageAndActionOrder(t *testing.T) {
	const later = " --new=quic --filter-udp=853 --filter-l7=quic --payload=quic_initial --lua-desync=fake:blob=fake_default_quic:repeats=1"
	for _, tc := range []struct {
		filter string
		covers bool
	}{
		{"--filter-l7=http,tls", true},
		{"--filter-l7=dns --lua-desync=pass", true},
		{"--filter-l7=quic --payload=http_req --lua-desync=pass", false},
		{"--filter-l7=known", false},
		{"--filter-l7=unknown", false},
		{"--filter-l7=all", false},
	} {
		value, err := ParseNative("fixture", "--filter-udp=853 "+tc.filter+later)
		if err != nil || value.HandlesQUIC(IPv4, 853) != tc.covers {
			t.Errorf("incorrect QUIC coverage with %q: want %v, %v", tc.filter, tc.covers, err)
		}
	}
	for _, bad := range []string{
		"--filter-l7=http,unrecognized", "--payload=dns_query", "--lua-desync=pass --payload=http_req",
		"--payload=unrecognized --lua-desync=pass", "--lua-desync=pass --lua-desync=pass", "--lua-desync=pass:unexpected=1",
		"--filter-l7=http,tls --payload=tls_client_hello --lua-desync=multisplit:pos=9",
	} {
		if _, err := ParseNative("fixture", "--filter-tcp=443 "+bad); !errors.Is(err, ErrInvalidStrategy) {
			t.Errorf("accepted invalid or unrepresented native action syntax %q: %v", bad, err)
		}
	}
}

func TestNativeSingleTransportRevisionsUnchanged(t *testing.T) {
	for _, tc := range []struct{ filter, legacy string }{
		{"--filter-tcp=443", "1cb5e0caa34931049ac5e219168ceb26f342a3663ccd65efa35b4f7710b99656"},
		{"--filter-udp=443", "c166fd36ef88f95912cee03a752e10e3ffb04c49f6f86e94feec0a13653d709d"},
	} {
		value, err := ParseNative("fixture", tc.filter)
		if err != nil || value.Revision != tc.legacy {
			t.Errorf("%s changed its sealed v1 revision: %s, %v", tc.filter, value.Revision, err)
		}
	}
}

func TestNativeRawNeverAdmitsCustomLua(t *testing.T) {
	const raw = "--filter-tcp=443 --lua-init=@/private/path.lua"
	_, err := ParseNative("fixture", raw)
	if !errors.Is(err, ErrInvalidStrategy) || !errors.Is(err, ErrUnsupportedCustomLua) || strings.Contains(err.Error(), "/private/") {
		t.Fatalf("custom code did not return a safe trust-boundary diagnostic: %v", err)
	}
}

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

const circularFixture = "\t--name=rotate --filter-tcp=443 --filter-l3=ipv6\r\n" +
	"--filter-l7=tls --in-range=-n32 --lua-desync=circular:fails=2\n" +
	"--in-range=x --payload=tls_client_hello\r\n" +
	"--lua-desync=multisplit:pos=17:strategy=1\n--lua-desync=multisplit:pos=9:strategy=2\n" +
	"--lua-desync=multisplit:pos=11:strategy=3\n--lua-desync=multisplit:pos=13:strategy=4\n" +
	"--lua-desync=multisplit:pos=19:strategy=5\n--new=passive --filter-tcp=853\n"

func TestNativeCircularRoundTripAndReturnScope(t *testing.T) {
	raw := circularFixture
	record, err := SealDraft(Draft{ID: "fixture", Name: "Circular", Raw: &raw})
	if err != nil {
		t.Fatal(err)
	}
	value := record.Definition
	if record.Raw != circularFixture || !value.NeedsReturn ||
		!slices.Equal(value.Assets, []AssetRef{Library, Actions, Automation}) ||
		!slices.Equal(value.Profiles[0].Circular.Splits, []uint16{17, 9, 11, 13, 19}) {
		t.Fatalf("lost native Circular ordering or asset identity: %+v", value)
	}
	imported, err := record.Export().Import()
	if err != nil || imported.Raw == nil || *imported.Raw != circularFixture {
		t.Fatalf("native transfer lost the original source: %v", err)
	}
	draft, err := SealDraft(Draft{ID: "fixture", Name: "Circular", Profiles: value.Profiles})
	if err != nil || draft.Definition.Revision != value.Revision {
		t.Fatalf("visual representation changed the runtime definition: %v", err)
	}
	v4, err := value.PacketScopeForFamily(IPv4)
	if err != nil || v4.NeedsReturn || len(v4.ReturnTCP) != 0 || !slices.Equal(v4.TCP, []uint16{853}) {
		t.Fatalf("unselected family acquired return scope: %+v, %v", v4, err)
	}
	v6, err := value.PacketScopeForFamily(IPv6)
	if err != nil || !v6.NeedsReturn || !slices.Equal(v6.ReturnTCP, []uint16{443}) || !slices.Equal(v6.TCP, []uint16{443, 853}) {
		t.Fatalf("return scope does not distinguish the Circular port: %+v, %v", v6, err)
	}
	if _, err := value.Compile("/opt/zapret2", 401, 0x120000, 0x40000000); !errors.Is(err, errReturnObservationUnavailable) {
		t.Fatalf("an ordinary backend accepted an unadmitted return path: %v", err)
	}
	arg, trace, err := CompileBindingsForReturnPath("/opt/zapret2", 401, 0x40000000, []MarkedStrategy{{Strategy: value, QueueMark: 0x120000}}, false)
	if err != nil || trace != nil {
		t.Fatalf("compile Circular: %v", err)
	}
	want := []string{"--in-range=-n32", "--lua-desync=circular:fails=2:key=solovey_1/1:hostkey=solovey_circular_hostkey",
		"--in-range=x", "--payload=tls_client_hello", "--lua-desync=multisplit:pos=17:strategy=1", "--lua-desync=multisplit:pos=9:strategy=2"}
	start := slices.Index(arg, want[0])
	if start < 0 || len(arg) < start+len(want) || !slices.Equal(arg[start:start+len(want)], want) ||
		!slices.Contains(arg, "--lua-init=@/opt/zapret2/lua/zapret-auto.lua") {
		t.Fatalf("engine lost the action-local range or pinned asset: %v", arg)
	}
}

func TestNativeCircularRejectsLostOrUnboundedReturnSemantics(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(circularFixture, "--in-range=-n32 ", "", 1),
		strings.Replace(circularFixture, "--in-range=-n32", "--in-range=-n64", 1),
		strings.Replace(circularFixture, "--in-range=x ", "", 1),
		strings.Replace(circularFixture, "--in-range=x --payload=tls_client_hello", "--payload=tls_client_hello --in-range=x", 1),
		strings.Replace(circularFixture, "--in-range=x --payload=tls_client_hello", "--in-range=x --payload=tls_client_hello --in-range=-n32", 1),
		strings.Replace(circularFixture, "circular:fails=2", "circular:fails=2:hostkey=untrusted", 1),
		strings.Replace(circularFixture, "pos=9:strategy=2", "pos=9:strategy=3", 1),
		circularFixture + " --lua-init=@/tmp/untrusted.lua",
	} {
		if _, err := ParseNative("fixture", raw); !errors.Is(err, ErrInvalidStrategy) {
			t.Fatalf("accepted altered Circular semantics %q: %v", raw, err)
		}
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

func TestNativeManagedLuaPreservesMixedActionOrder(t *testing.T) {
	const source = "function inspect() return VERDICT_PASS end\n"
	catalog := Catalog{
		Schema: CatalogSchema, Records: map[string]Record{},
		LuaDrafts: map[string]LuaDraft{"inspect": {Origin: "administrator", Source: source}},
	}
	// LuaDraft.Revision includes provenance, so use the catalog's exact stored
	// draft rather than reconstructing a source-only digest at the call site.
	ref := LuaReference{ID: "inspect", Revision: catalog.LuaDrafts["inspect"].Revision()}

	for name, raw := range map[string]string{
		"builtin-first": "--name=mixed\n--filter-tcp=443\n--filter-l7=tls\n--payload=tls_client_hello\n" +
			"--lua-desync=multisplit:pos=9\n--lua-desync=inspect:first=1\n",
		"managed-first": "--name=mixed\n--filter-tcp=443\n--filter-l7=tls\n--payload=tls_client_hello\n" +
			"--lua-desync=inspect:first=1\n--lua-desync=multisplit:pos=9\n",
	} {
		t.Run(name, func(t *testing.T) {
			record, err := catalog.SealDraft(Draft{ID: "mixed-" + name, Name: "Mixed", Raw: &raw, ManagedLua: []LuaReference{ref}})
			if err != nil {
				t.Fatal(err)
			}
			profile := record.Definition.Profiles[0]
			if len(profile.ActionOrder) != 2 || len(profile.LuaActions) != 1 || profile.Payload != "" ||
				profile.ActionOrder[0].Kind == profile.ActionOrder[1].Kind {
				t.Fatalf("mixed action order was not retained: %+v", profile)
			}
			canonical, err := catalog.SealDraft(Draft{ID: record.Definition.ID, Name: record.Name,
				Profiles: record.Definition.Profiles, ManagedLua: record.Definition.ManagedLua})
			if err != nil || canonical.Definition.Revision != record.Definition.Revision {
				t.Fatalf("mixed visual round trip changed the native definition: %v", err)
			}
			modules, err := catalog.SelectedLua(record.Definition.ManagedLua)
			if err != nil {
				t.Fatal(err)
			}
			args, _, _, err := CompileManagedBindings("/engine", "/run/managed.lua", 401, 0x40000000,
				[]MarkedStrategy{{Strategy: record.Definition, QueueMark: 0x120000}}, [][]LuaModule{modules}, false, false)
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(args, "\x00")
			builtin := strings.Index(joined, "--lua-desync=multisplit:pos=9")
			managed := strings.Index(joined, "--lua-desync=solovey_managed_0_0:first=1")
			if builtin < 0 || managed < 0 || (name == "builtin-first") != (builtin < managed) {
				t.Fatalf("compiled native action order changed: %s", joined)
			}
		})
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
		"--filter-tcp=443 --hostlist-domains=" + strings.Repeat("a.test,", maxNativeListBytes/len("a.test,")+1) + "b.test",
		"--filter-tcp=443 --filter-l3=all",
		"--filter-udp=443 --filter-tcp=443 --filter-udp=853",
		strings.Replace(circularFixture, "--filter-tcp=443", "--filter-tcp=443 --filter-udp=443", 1),
		"--filter-udp=443 --filter-l7=quic --payload=quic_initial --lua-desync=fake:blob=fake_default_quic:repeats=5",
		"--filter-udp=443 --filter-l7=quic --payload=quic_initial --lua-desync=fake:blob=other:repeats=1",
		"--filter-tcp=443 --filter-l7=quic --payload=quic_initial --lua-desync=fake:blob=fake_default_quic:repeats=1",
		"--filter-udp=443 --filter-l7=tls --payload=tls_client_hello --lua-desync=multisplit:pos=9",
		"--filter-tcp=443 --filter-tcp=80",
		"--filter-tcp=443 --filter-l7=tls,unknown-proto",
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
