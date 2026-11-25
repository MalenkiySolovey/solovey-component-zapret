package strategy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

const guiFixturePreset = `# Preset: Two transport filters
--name=first --filter-tcp=443 --hostlist="lists\SITE hosts.txt"
--new=second --filter-udp=443 --ipset-exclude=lists/local.txt
`

func guiPackageFixture(t *testing.T, source, storage string, mutate func(*guiPresetManifest)) []byte {
	t.Helper()
	manifest := guiPresetManifest{Format: guiPresetFormat, Version: 1, Engine: "winws2", FileName: "Two transport filters.txt",
		Lists: []guiPresetList{
			{FileName: "Site hosts.txt", Kind: "hostlist", Storage: storage, Path: "lists/Site hosts.txt", SHA256: fixtureDigest("# comment\nExample.test\n")},
			{FileName: "local.txt", Kind: "ipset", Storage: "standalone", Path: "lists/local.txt", SHA256: fixtureDigest("192.0.2.0/24\n")},
		},
	}
	if mutate != nil {
		mutate(&manifest)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return presetZIP(t, []zipSource{
		{name: "manifest.json", contents: string(data)}, {name: "preset.txt", contents: source},
		{name: "lists/Site hosts.txt", contents: "# comment\r\nExample.test\r\n\r\n"},
		{name: "lists/local.txt", contents: "192.0.2.0/24\n"},
	})
}

func fixtureDigest(text string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(text))) }

func TestPresetZapretGUIArchiveCompilesAndPreservesOrigin(t *testing.T) {
	base := emptyPresetCatalog()
	before := base.Revision()
	data := guiPackageFixture(t, guiFixturePreset, "standalone", nil)
	next, preview, err := base.PreparePreset("imported", PresetInput{Format: "zip", Content: data})
	if err != nil || len(preview.Issues) != 0 || preview.Record == nil || !preview.Converted || preview.Source != guiFixturePreset {
		t.Fatalf("expected a usable preview: %+v, %v", preview, err)
	}
	r := *preview.Record
	if r.Name != "Two transport filters" || preview.Origin == nil || r.Origin == nil || *preview.Origin != *r.Origin || r.Origin.SHA256 != fixtureDigest(string(data)) {
		t.Fatalf("wrong original package receipt: %+v", preview.Origin)
	}
	if strings.Contains(r.Raw, `lists\`) || strings.Contains(r.Raw, "lists/") || !strings.Contains(r.Raw, "# Preset: Two transport filters\n") {
		t.Fatalf("raw mode kept a host path or lost comments: %q", r.Raw)
	}
	if len(preview.Dependencies) != 2 || preview.Dependencies[0].ID == "" || preview.Dependencies[0].State != "new" || preview.Dependencies[1].ID != "local" {
		t.Fatalf("unexpected managed dependencies: %+v", preview.Dependencies)
	}
	if err := next.Validate(); err != nil || base.Revision() != before {
		t.Fatalf("preview mutated live settings or did not produce a valid candidate: %v", err)
	}
	args, err := CompileBindings("/engine", 42, 0x40000000, []MarkedStrategy{{r.Definition, 0x10000}})
	if err != nil || !slices.Contains(args, "--hostlist-domains=example.test") || !slices.Contains(args, "--ipset-exclude-ip=192.0.2.0/24") {
		t.Fatalf("compiler did not preserve the TCP include and UDP exclusion: %v, %v", args, err)
	}
	packageData, err := next.ExportPackage("imported")
	if err != nil {
		t.Fatal(err)
	}
	restored, review, err := base.PreparePreset("imported", PresetInput{Format: "zip", Content: packageData})
	if err != nil || review.Record == nil || restored.Revision() != next.Revision() {
		t.Fatalf("Solovey ZIP lost the origin or dependencies: %+v, %v", review, err)
	}
	edited, err := next.SealDraft(Draft{ID: "imported", Name: "Renamed", Raw: &r.Raw, ListRefs: r.ListRefs})
	if err != nil || edited.Origin == nil || *edited.Origin != *r.Origin || edited.Definition.Revision != r.Definition.Revision {
		t.Fatalf("renaming lost import provenance or replaced runtime revision: %+v, %v", edited.Origin, err)
	}
	updated, err := next.WithNamedList("local", &NamedList{Kind: IPSetKind, Entries: []string{"192.0.2.2"}})
	if err != nil || updated.Records["imported"].Origin == nil || *updated.Records["imported"].Origin != *r.Origin {
		t.Fatalf("re-sealing a dependency lost the archive origin: %v", err)
	}
}

func TestPresetZapretGUIArchiveDoesNotInventDependencies(t *testing.T) {
	for _, tc := range []struct {
		name, storage, source, message string
	}{
		{"overlay", "overlay", guiFixturePreset, "unverified base list"},
		{"base omitted", "standalone", strings.Replace(guiFixturePreset, "SITE hosts.txt", "omitted.txt", 1), "export or supply the full effective list"},
		{"wrong kind", "standalone", strings.Replace(guiFixturePreset, "--hostlist=", "--ipset=", 1), "not ipSet"},
		{"platform option", "standalone", "--wf-tcp-out=443\n" + guiFixturePreset, "Windows interception"},
		{"unused entry", "standalone", strings.Replace(guiFixturePreset, " --ipset-exclude=lists/local.txt", "", 1), "not referenced"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := emptyPresetCatalog()
			base.Lists = map[string]NamedList{"omitted": {Kind: HostListKind, Entries: []string{"not-the-source.test"}}}
			before := base.Revision()
			next, review, err := base.PreparePreset("candidate", PresetInput{Format: "zip", Content: guiPackageFixture(t, tc.source, tc.storage, nil)})
			if err != nil || review.Record != nil || next.Records != nil || review.Source != tc.source || review.Origin == nil || base.Revision() != before ||
				!slices.ContainsFunc(review.Issues, func(issue PresetIssue) bool { return strings.Contains(issue.Message, tc.message) }) {
				t.Fatalf("unsupported candidate was saved or lost its reason: %+v, %v", review, err)
			}
		})
	}
}

func TestPresetZapretGUIRepairUsesVerifiedArchivePath(t *testing.T) {
	base := emptyPresetCatalog()
	base.Lists = map[string]NamedList{"local": {Kind: IPSetKind, Entries: []string{"198.51.100.0/24"}}}
	before := base.Revision()
	data := guiPackageFixture(t, guiFixturePreset, "standalone", nil)
	input := PresetInput{Format: "zip", Content: data, DependencyIDs: map[string]string{"lists/local.txt": "imported-local"}}
	next, preview, err := base.PreparePreset("imported", input)
	if err != nil || preview.Record == nil || base.Revision() != before || preview.Origin.SHA256 != fixtureDigest(string(data)) ||
		preview.Record.ListRefs["second"].IPSetExclude != "imported-local" ||
		!slices.Equal(next.Lists["local"].Entries, []string{"198.51.100.0/24"}) || !slices.Equal(next.Lists["imported-local"].Entries, []string{"192.0.2.0/24"}) {
		t.Fatalf("did not attach the verified list to only the imported record: %+v, %v", preview, err)
	}
	input.DependencyIDs["lists/Site hosts.txt"] = "imported-local"
	if _, preview, err := base.PreparePreset("imported", input); err != nil || preview.Record != nil ||
		len(preview.Dependencies) != 2 || preview.Dependencies[1].State != "conflict" || base.Revision() != before {
		t.Fatalf("different archive dependencies were conflated under one ID: %+v, %v", preview, err)
	}
	input.Content = guiPackageFixture(t, guiFixturePreset, "overlay", nil)
	if _, preview, err := base.PreparePreset("imported", input); err != nil || preview.Record != nil ||
		len(preview.Dependencies) != 2 || preview.Dependencies[0].CanRename || preview.Dependencies[0].State != "missing" {
		t.Fatalf("alternate ID resolved a missing overlay base: %+v, %v", preview, err)
	}
}

func TestPresetZapretGUIArchiveRejectsInvalidEnvelope(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*guiPresetManifest)
	}{
		{"checksum", func(m *guiPresetManifest) { m.Lists[0].SHA256 = strings.Repeat("0", 64) }},
		{"traversal", func(m *guiPresetManifest) { m.Lists[0].Path = "lists/../Site hosts.txt" }},
		{"alias", func(m *guiPresetManifest) { m.Lists[1].FileName = "sITE HOSTS.TXT" }},
		{"old engine", func(m *guiPresetManifest) { m.Engine = "winws1" }},
		{"unknown version", func(m *guiPresetManifest) { m.Version++ }},
		{"unsafe name", func(m *guiPresetManifest) { m.FileName = "../other.txt" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, preview, err := emptyPresetCatalog().PreparePreset("invalid", PresetInput{Format: "zip", Content: guiPackageFixture(t, guiFixturePreset, "standalone", test.edit)})
			if err == nil || preview.Record != nil {
				t.Fatalf("malformed original archive accepted: %+v", preview)
			}
		})
	}
	for _, extra := range []zipSource{{name: "strategy.json"}, {name: "lua/run.txt"}, {name: "lists/../bad.txt"}} {
		data := presetZIP(t, []zipSource{{name: "manifest.json"}, {name: "preset.txt"}, extra})
		if _, _, err := readGuiFixture(data); err == nil {
			t.Errorf("unexpected original ZIP entry %q accepted", extra.name)
		}
	}
}

func readGuiFixture(data []byte) (Catalog, PresetPreview, error) {
	return emptyPresetCatalog().PreparePreset("invalid", PresetInput{Format: "zip", Content: data})
}
