package strategy

import (
	"strings"
	"testing"
)

const nativeTemplateFixture = `--template=inactive --filter-tcp=4999
--new --template=ports --filter-tcp=18080-18082 --filter-l3=ipv4 --filter-l7=quic --hostlist-domains=shared.test
--new --template --import ports --name=web --filter-l3=ipv6 --filter-l7=http --payload=http_req
--out-range=-d1 --lua-desync=http_domcase --out-range=a --lua-desync=http_hostcase:spell=hOsT
--new=active --filter-tcp=18083 --filter-l7=tls --import=web --hostlist-domains=other.test
`

func TestNativeTemplatesProjectOnlyTheActiveScopes(t *testing.T) {
	value, err := ParseNative("test", nativeTemplateFixture)
	if err != nil {
		t.Fatal(err)
	}
	flat, err := ParseNative("test", "--name=active --filter-tcp=18080-18083 --filter-l7=http --hostlist-domains=other.test,shared.test "+
		"--payload=http_req --out-range=-d1 --lua-desync=http_domcase --out-range=a --lua-desync=http_hostcase:spell=hOsT")
	if err != nil || value.Revision != flat.Revision {
		t.Fatalf("the template must lower to the same ordered, dual-family profile: %+v, %v", value.Profiles, err)
	}
	_, review, err := emptyPresetCatalog().PreparePreset("test", PresetInput{Format: "native", Content: []byte(nativeTemplateFixture)})
	if err != nil || review.Record == nil || review.Record.Raw != nativeTemplateFixture || review.Record.Definition.Revision != flat.Revision {
		t.Fatalf("native import lost template source or compiled settings: %+v, %v", review.Issues, err)
	}
}

func TestNativeTemplateRejectsUnresolvedAndUnrepresentableCompositions(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"--import=missing --filter-tcp=80", "must be defined in an earlier profile"},
		{"--template=a --filter-tcp=80 --new --template=a --new --import=a", "already defined"},
		{"--template=a --filter-tcp=80", "templates do not run"},
		{"--template --filter-tcp=80 --new --filter-tcp=80", "name the template"},
		{"--template=a --filter-tcp=80 --new --import", "specify a template ID"},
		{"--template=a --lua-desync=pass --new --filter-tcp=80 --import=a --lua-desync=pass", "one complete action sequence"},
		{"--template=a --lua-init=@foreign.lua --new --filter-tcp=80", "UNSUPPORTED_CUSTOM_LUA"},
	} {
		if _, err := ParseNative("test", tc.text); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.text, err, tc.want)
		}
	}
}

func TestNativePresetTemplateRetainsManagedListOnItsSource(t *testing.T) {
	text := "--template=common --filter-tcp=80 --hostlist=lists/site.txt\n" +
		"--new --template=other --filter-tcp=443 --hostlist=lists/other.txt\n" +
		"--new --import   common\n--new=direct --filter-tcp=8080\n"
	candidate, preview, err := emptyPresetCatalog().PreparePreset("test", PresetInput{Format: "zip", Content: presetZIP(t, []zipSource{
		{name: "strategy.txt", contents: text}, {name: "lists/site.txt", contents: "Example.test\n"}, {name: "lists/other.txt", contents: "other.test\n"},
	}), DependencyIDs: []PresetDependencyID{{Path: "lists/site.txt", ID: "managed-site"}}})
	if err != nil || preview.Record == nil {
		t.Fatalf("managed template import: %+v, %v", preview.Issues, err)
	}
	r := *preview.Record
	if r.ListRefs[templateListPrefix+"common"].HostList != "managed-site" || r.ListRefs[templateListPrefix+"other"].HostList != "other" || len(r.ListRefs) != 2 {
		t.Fatalf("managed list ownership lost: %+v", r.ListRefs)
	}
	if profiles := r.Definition.Profiles; len(profiles) != 2 || profiles[0].Lists == nil ||
		len(profiles[0].Lists.HostList) != 1 || profiles[0].Lists.HostList[0] != "example.test" || profiles[1].Lists != nil ||
		r.Raw != strings.Replace(text, "lists/site.txt", "lists/managed-site.txt", 1) {
		t.Fatalf("incorrect compiled lists or lost template source: %+v", r)
	}
	changedSource := strings.Replace(r.Raw, "--import   common\n--new=direct --filter-tcp=8080", "--import=other\n--new=direct --import=common", 1)
	edited, err := candidate.SealDraft(Draft{ID: "test", Name: r.Name, Raw: &changedSource, ListRefs: r.ListRefs})
	if err != nil || edited.Definition.Profiles[0].Lists == nil || edited.Definition.Profiles[1].Lists == nil ||
		edited.Definition.Profiles[0].Lists.HostList[0] != "other.test" || edited.Definition.Profiles[1].Lists.HostList[0] != "example.test" {
		t.Fatalf("changing the imports must reproject their dependencies: %+v, %v", edited, err)
	}
	updated, err := candidate.WithNamedList("other", &NamedList{Kind: HostListKind, Entries: []string{"unused.test"}})
	if err != nil || updated.Records["test"].Definition.Revision != r.Definition.Revision {
		t.Fatalf("unused template data changed the active definition: %v", err)
	}
	archive, err := candidate.ExportPackage("test")
	if err != nil {
		t.Fatal(err)
	}
	restored, review, err := emptyPresetCatalog().PreparePreset("test", PresetInput{Format: "zip", Content: archive})
	if err != nil || review.Record == nil || restored.Revision() != candidate.Revision() {
		t.Fatalf("managed-list template restore drifted: %+v, %v", review.Issues, err)
	}
}

func TestNativeTemplateNeverReadsPathsOrDropsIncompatibleListSlots(t *testing.T) {
	cat := emptyPresetCatalog()
	cat.Lists = map[string]NamedList{"one": {Kind: HostListKind, Entries: []string{"one.test"}}, "two": {Kind: HostListKind, Entries: []string{"two.test"}}}
	for _, tc := range []struct{ text, cause string }{
		{"--template=a --hostlist=/etc/hosts --new --import=a --filter-tcp=80", "host paths are never read"},
		{"--template=a --hostlist=lists/missing.txt --new --filter-tcp=80", "native list \"missing\" is missing"},
		{"--template=a --hostlist=lists/one.txt --new --template=b --hostlist=lists/two.txt --new --import=a --import=b --filter-tcp=80", "same filter slot"},
	} {
		if _, err := cat.SealDraft(Draft{ID: "test", Name: "test", Raw: &tc.text}); err == nil || !strings.Contains(err.Error(), tc.cause) {
			t.Errorf("%q: %v, want %q", tc.text, err, tc.cause)
		}
	}
}

func TestNativePresetTemplateChargesActiveListProjection(t *testing.T) {
	list := strings.Join(namedListFixture(8), "\n") + "\n"
	include := "--new --import=common\n"
	active := "--new --filter-tcp=80\n"
	for _, scenario := range []struct {
		name, source string
		wantRecord   bool
	}{
		{"inactive", active + strings.Repeat(active, 10), true},
		{"expanded", include + strings.Repeat(include, 10), false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			text := "--template=common --filter-tcp=443 --hostlist=lists/site.txt\n" + scenario.source
			zip := presetZIP(t, []zipSource{{name: "strategy.txt", contents: text}, {name: "lists/site.txt", contents: list}})
			_, preview, err := emptyPresetCatalog().PreparePresetWithin("test", PresetInput{Format: "zip", Content: zip}, len(text)+len(list)+128)
			if err != nil || (preview.Record != nil) != scenario.wantRecord {
				t.Fatalf("unexpected admission result: record=%t, issues=%+v, err=%v", preview.Record != nil, preview.Issues, err)
			}
			if !scenario.wantRecord && (len(preview.Issues) != 1 || !strings.Contains(preview.Issues[0].Message, "projected bytes")) {
				t.Fatalf("expected measured projection refusal: %+v", preview.Issues)
			}
		})
	}
}
