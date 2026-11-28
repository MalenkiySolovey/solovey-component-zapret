package strategy

import (
	"slices"
	"strings"
	"testing"
)

const nativeSkippedFixture = "--name=disabled --filter-tcp=8080 --skip --lua-desync=pass\n" +
	"--new=active --filter-tcp=18080 --lua-desync=pass\n" +
	"--new=last --skip --filter-udp=18443"

func TestNativeSkippedProfileProjectsOnlyActiveSource(t *testing.T) {
	value, err := ParseNative("test", nativeSkippedFixture)
	if err != nil {
		t.Fatal(err)
	}
	active, err := ParseNative("test", "--name=active --filter-tcp=18080 --lua-desync=pass")
	if err != nil || active.Revision != value.Revision || len(value.Profiles) != 1 {
		t.Fatalf("skipped profile reached the active definition: %+v, %v", value.Profiles, err)
	}
	compiled, err := value.Compile("/opt/zapret", 401, 0x120000, 0x40000000)
	if err != nil || slices.Contains(compiled, "--skip") || slices.Contains(compiled, "--filter-tcp=8080") || !slices.Contains(compiled, "--filter-tcp=18080") {
		t.Fatalf("unexpected installed argv %v: %v", compiled, err)
	}
	_, preview, err := emptyPresetCatalog().PreparePreset("test", PresetInput{Format: "native", Content: []byte(nativeSkippedFixture)})
	if err != nil || preview.Record == nil || preview.Record.Raw != nativeSkippedFixture || preview.Record.VisualEditable() {
		t.Fatalf("the native editor must retain the disabled source: %+v, %v", preview.Issues, err)
	}
}

func TestNativeSkippedProfileKeepsItsManagedLists(t *testing.T) {
	text := "--filter-tcp=8080 --skip --hostlist=lists/paused.txt --lua-desync=pass\n" +
		"--new --filter-tcp=18080 --lua-desync=pass"
	catalog, preview, err := emptyPresetCatalog().PreparePreset("test", PresetInput{Format: "zip", Content: presetZIP(t, []zipSource{
		{name: "strategy.txt", contents: text}, {name: "lists/paused.txt", contents: "paused.test\n"},
	})})
	if err != nil || preview.Record == nil {
		t.Fatalf("preset failed: %+v, %v", preview.Issues, err)
	}
	paused := *preview.Record
	if paused.ListRefs["profile-1"].HostList != "paused" || paused.Definition.Profiles[0].ID != "profile-2" || paused.Definition.Profiles[0].Lists != nil {
		t.Fatalf("skipping reassigned a managed list or a profile ID: %+v", paused)
	}
	bundle, err := catalog.ExportPackage("test")
	if err != nil {
		t.Fatal(err)
	}
	restored, preview, err := emptyPresetCatalog().PreparePreset("test", PresetInput{Format: "zip", Content: bundle})
	if err != nil || preview.Record == nil || preview.Record.Raw != paused.Raw || preview.Record.Definition.Revision != paused.Definition.Revision {
		t.Fatalf("skip did not survive ZIP round trip: %+v, %v", preview.Issues, err)
	}
	active := strings.Replace(paused.Raw, " --skip", "", 1)
	enabled, err := restored.SealDraft(Draft{ID: "test", Name: paused.Name, Raw: &active, ListRefs: paused.ListRefs})
	if err != nil || len(enabled.Definition.Profiles) != 2 || enabled.Definition.Profiles[0].Lists == nil ||
		!slices.Equal(enabled.Definition.Profiles[0].Lists.HostList, []string{"paused.test"}) || enabled.Definition.Profiles[1].Lists != nil {
		t.Fatalf("managed list was not restored to its profile: %+v, %v", enabled.Definition.Profiles, err)
	}
}

func TestNativeSkipRetainsRuntimeBoundaries(t *testing.T) {
	for _, test := range []struct{ text, message string }{
		{"--skip --filter-tcp=80", "Add an active profile"},
		{"--skip --qnum=1 --new --filter-tcp=80", "only to native profile options"},
		{"--skip --lua-init=@host.lua --new --filter-tcp=80", "UNSUPPORTED_CUSTOM_LUA"},
		{"--template=hidden --skip --new --import=hidden --filter-tcp=80", "must be defined in an earlier profile"},
	} {
		if _, err := ParseNative("test", test.text); err == nil || !strings.Contains(err.Error(), test.message) {
			t.Errorf("%q: got %v, want %q", test.text, err, test.message)
		}
	}
}

func TestNativeSkippedTemplateRetainsItsOwnList(t *testing.T) {
	text := "--template=base --skip --filter-tcp=18080 --hostlist=lists/paused.txt\n" +
		"--new=active --filter-tcp=18080 --lua-desync=pass"
	catalog, preview, err := emptyPresetCatalog().PreparePreset("test", PresetInput{Format: "zip", Content: presetZIP(t, []zipSource{
		{name: "strategy.txt", contents: text}, {name: "lists/paused.txt", contents: "paused.test\n"},
	}), DependencyIDs: []PresetDependencyID{{Path: "lists/paused.txt", ID: "managed-paused"}}})
	if err != nil || preview.Record == nil {
		t.Fatalf("preset failed: %+v, %v", preview.Issues, err)
	}
	paused := *preview.Record
	if paused.ListRefs[templateListPrefix+"base"].HostList != "managed-paused" || paused.Definition.Profiles[0].Lists != nil {
		t.Fatalf("inactive template lost or applied its managed list: %+v", paused)
	}
	afterEdit, err := catalog.WithNamedList("managed-paused", &NamedList{Kind: HostListKind, Entries: []string{"changed.test"}})
	if err != nil || afterEdit.Records["test"].Definition.Revision != paused.Definition.Revision {
		t.Fatalf("editing a skipped template's list changed the active revision: %v", err)
	}
	enabledSource := strings.Replace(paused.Raw, " --skip", "", 1) + " --import=base"
	enabled, err := afterEdit.SealDraft(Draft{ID: "test", Name: paused.Name, Raw: &enabledSource, ListRefs: paused.ListRefs})
	if err != nil || len(enabled.Definition.Profiles) != 1 || enabled.Definition.Profiles[0].Lists == nil ||
		!slices.Equal(enabled.Definition.Profiles[0].Lists.HostList, []string{"changed.test"}) {
		t.Fatalf("enabling the template did not activate the remapped list: %+v, %v", enabled.Definition.Profiles, err)
	}
}
