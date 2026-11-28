package strategy

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"
)

const portablePreset = "\ufeff# Author comment\r\n--name=tls --filter-tcp=443\r\n" +
	"--filter-l3=ipv6,ipv4 --hostlist=\"lists/site.txt\" # managed list\r\n" +
	"--filter-l7=tls --payload=tls_client_hello --lua-desync=multisplit:pos=2\r\n" +
	"--new=quic --filter-udp=443 --filter-l7=quic --payload=quic_initial\r\n" +
	"--blob=fixture:@blobs/fixture.hex --lua-desync=fake:blob=fixture:repeats=1\r\n"

func emptyPresetCatalog() Catalog {
	return Catalog{Schema: CatalogSchema, Records: map[string]Record{}}
}

func fixturePresetPackage(t *testing.T, source string) []byte {
	t.Helper()
	return presetZIP(t, []zipSource{
		{name: "strategy.txt", contents: source}, {name: "lists/site.txt", contents: "# local fixtures\r\nExample.test\r\n"},
		{name: "blobs/fixture.hex", contents: "4050414e454c\n"},
	})
}

func TestPresetPackageMapsDependenciesAndRoundTrips(t *testing.T) {
	catalog := emptyPresetCatalog()
	before := catalog.Revision()
	input := PresetInput{Format: "zip", Name: "Two ordered profiles", Content: fixturePresetPackage(t, portablePreset)}
	next, preview, err := catalog.PreparePreset("first", input)
	if err != nil || len(preview.Issues) != 0 || preview.Record == nil || !preview.Converted || preview.Source != portablePreset {
		t.Fatalf("expected an admitted conversion: %+v, %v", preview, err)
	}
	if before != catalog.Revision() || next.Revision() == before || next.Validate() != nil {
		t.Fatal("preparation mutated the current catalog or did not make a valid candidate")
	}
	if !slices.Equal(preview.Dependencies, []PresetDependency{
		{Source: "lists/site.txt", ID: "site", Kind: "hostList", State: "new", CanRename: true},
		{Source: "blobs/fixture.hex", ID: "fixture", Kind: "blob", State: "new", CanRename: true},
	}) {
		t.Fatalf("dependency plan lost references: %+v", preview.Dependencies)
	}
	r := *preview.Record
	if r.VisualEditable() {
		t.Fatal("visual regeneration would discard the original comments or layout")
	}
	if !strings.Contains(r.Raw, "# Author comment\r\n") || !strings.Contains(r.Raw, "# managed list\r\n") || strings.Contains(r.Raw, "lists/site.txt") || strings.Contains(r.Raw, "--blob=") {
		t.Fatalf("expected comments and no native host paths, got %q", r.Raw)
	}
	profiles := r.Definition.Profiles
	if len(profiles) != 2 || profiles[0].ID != "tls" || profiles[0].Family != "" || profiles[1].ID != "quic" || profiles[1].QUICBlobRef != "fixture" || r.ListRefs["tls"].HostList != "site" {
		t.Fatalf("conversion changed profile order or dependencies: %+v", r)
	}
	if !slices.Equal(profiles[0].Lists.HostList, []string{"example.test"}) || r.Definition.BlobData["fixture"] != "4050414e454c" {
		t.Fatalf("conversion changed asset data: %+v", r)
	}
	transfer, err := next.Export("first")
	if err != nil {
		t.Fatal(err)
	}
	jsonData, err := json.Marshal(transfer)
	if err != nil {
		t.Fatal(err)
	}
	packaged, err := next.ExportPackage("first")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []PresetInput{{Format: "json", Content: jsonData}, {Format: "zip", Content: packaged}} {
		restored, review, err := emptyPresetCatalog().PreparePreset("first", input)
		if err != nil || review.Record == nil || restored.Revision() != next.Revision() {
			t.Errorf("%s did not round-trip exact raw source, assets and definition: %+v, %v", input.Format, review, err)
		}
	}
	clone, review, err := next.PreparePreset("copy", input)
	if err != nil || review.Record == nil || len(clone.Records) != 2 || len(review.Dependencies) != 2 || review.Dependencies[0].State != "reuse" || review.Dependencies[1].State != "reuse" {
		t.Fatalf("existing identical dependencies were not reused: %+v, %v", review, err)
	}
}

func presetBinaryFixture(t *testing.T, blob string) []byte {
	t.Helper()
	return presetZIP(t, []zipSource{
		{name: "strategy.txt", contents: strings.Replace(portablePreset, "blobs/fixture.hex", "blobs/fixture.bin", 1)},
		{name: "lists/site.txt", contents: "Example.test\n"},
		{name: "blobs/fixture.bin", contents: blob},
	})
}

func TestPresetPackageRepairsListAndBinaryConflicts(t *testing.T) {
	base := emptyPresetCatalog()
	base.Lists = map[string]NamedList{"site": {Kind: HostListKind, Entries: []string{"other.test"}}}
	base.BlobAssets = map[string]string{"fixture": "40"}
	before := base.Revision()
	input := PresetInput{Format: "zip", Content: presetBinaryFixture(t, "\xff\x00\x7f")}
	_, preview, err := base.PreparePreset("imported", input)
	if err != nil || preview.Record != nil || len(preview.Dependencies) != 2 ||
		!slices.EqualFunc(preview.Dependencies, []string{"lists/site.txt", "blobs/fixture.bin"}, func(dep PresetDependency, path string) bool {
			return dep.Source == path && dep.State == "conflict" && dep.CanRename
		}) {
		t.Fatalf("expected actionable list and binary conflicts: %+v, %v", preview, err)
	}
	input.DependencyIDs = []PresetDependencyID{{"lists/site.txt", "imported-list"}, {"blobs/fixture.bin", "imported-blob"}}
	next, preview, err := base.PreparePreset("imported", input)
	if err != nil || preview.Record == nil || base.Revision() != before || next.Validate() != nil {
		t.Fatalf("renamed assets failed to validate or modified the live catalog: %+v, %v", preview, err)
	}
	record := preview.Record
	if record.ListRefs["tls"].HostList != "imported-list" || record.Definition.BlobData["imported-blob"] != "ff007f" ||
		!strings.Contains(record.Raw, "fake:blob=asset_imported-blob:") || !strings.Contains(record.Raw, "# Author comment\r\n") ||
		!slices.Equal(next.Lists["site"].Entries, []string{"other.test"}) || next.BlobAssets["fixture"] != "40" {
		t.Fatalf("explicit ID choice changed data or replaced a current dependency: %+v", record)
	}
	packaged, err := next.ExportPackage("imported")
	if err != nil {
		t.Fatal(err)
	}
	copy, reviewed, err := base.PreparePreset("imported", PresetInput{Format: "zip", Content: packaged})
	if err != nil || reviewed.Record == nil || copy.Revision() != next.Revision() {
		t.Fatalf("ZIP round trip lost renamed IDs or binary bytes: %+v, %v", reviewed, err)
	}
	input.DependencyIDs = append(input.DependencyIDs, PresetDependencyID{"lists/not-included.txt", "ignored"})
	if _, preview, err = base.PreparePreset("imported", input); err != nil || preview.Record != nil ||
		!slices.ContainsFunc(preview.Issues, func(issue PresetIssue) bool { return issue.Code == "unbound-dependency-id" }) {
		t.Fatalf("unused explicit ID was silently ignored: %+v, %v", preview, err)
	}
	if _, err := readPresetPackage(presetBinaryFixture(t, strings.Repeat("x", MaxNativeBlobBytes+1)), DefaultPresetExpandedBytes); err == nil {
		t.Fatal("binary asset exceeded the bounded per-entry size")
	}
}

func TestPresetMixedSourceIsInertAndReportsOptions(t *testing.T) {
	c := emptyPresetCatalog()
	before := c.Revision()
	source := "# original Windows settings\n--wf-tcp-out=443\n--lua-init=@lua/custom.lua\n" + portablePreset[len("\ufeff"):] + `--template=hidden` + "\n"
	next, review, err := c.PreparePreset("new", PresetInput{Format: "zip", Content: fixturePresetPackage(t, source)})
	if err != nil || review.Record != nil || next.Records != nil || c.Revision() != before || review.Source != source || len(review.Profiles) != 2 {
		t.Fatalf("mixed source was not retained as inert evidence: %+v, %v", review, err)
	}
	for i, want := range []struct {
		option, code string
		line         int
	}{{"--wf-tcp-out", "platform-option", 2}, {"--lua-init", "trust-restricted", 3}, {"--template", "unrepresented-profile", 10}} {
		if len(review.Issues) <= i || review.Issues[i].Option != want.option || review.Issues[i].Code != want.code || review.Issues[i].Span == nil || review.Issues[i].Span.Line != want.line {
			t.Errorf("wrong position or reason for %s: %+v", want.option, review.Issues)
		}
	}
}

func TestPresetRejectsDependencyConflictsAndPayloadReordering(t *testing.T) {
	catalog := emptyPresetCatalog()
	list := NamedList{Kind: HostListKind, Entries: []string{"other.test"}}
	catalog.Lists = map[string]NamedList{"site": list}
	catalog.BlobAssets = map[string]string{"fixture": "40"}
	before := catalog.Revision()
	_, review, err := catalog.PreparePreset("imported", PresetInput{Format: "zip", Content: fixturePresetPackage(t, portablePreset)})
	if err != nil || review.Record != nil || len(review.Dependencies) != 2 || review.Dependencies[0].State != "conflict" || review.Dependencies[1].State != "conflict" || catalog.Revision() != before {
		t.Fatalf("conflicting data was overwritten or not explained: %+v, %v", review, err)
	}
	const reversed = "--filter-tcp=443 --filter-l7=tls --lua-desync=multisplit:pos=2 --payload=tls_client_hello"
	_, review, err = catalog.PreparePreset("legacy", PresetInput{Format: "native", Content: []byte(reversed)})
	if err != nil || review.Record != nil || len(review.Issues) != 1 || review.Issues[0].Code != "payload-order" {
		t.Fatalf("source import silently gave meaning to a later payload flag: %+v, %v", review, err)
	}
	if _, err := SealDraft(Draft{ID: "legacy", Name: "Existing", Raw: stringPointer(reversed)}); err != nil {
		t.Fatalf("existing Solovey native text became unreadable: %v", err)
	}
}

func stringPointer(s string) *string { return &s }

func TestPresetZIPAcceptsOnlyBoundedRegularDataFiles(t *testing.T) {
	for _, extra := range []zipSource{
		{name: "../strategy.txt"}, {name: "lists\\site.txt"}, {name: "lists/../../script.txt"}, {name: "Lists/site.txt"},
		{name: "script.lua"}, {name: "lists/site.exe"}, {name: "lists/site.txt", mode: fs.ModeSymlink | 0777},
		{name: "strategy.txt"}, {name: "lists/site.txt", contents: strings.Repeat("a", DefaultPresetExpandedBytes+1)},
	} {
		data := presetZIP(t, []zipSource{{name: "strategy.txt", contents: "--filter-tcp=443"}, extra})
		_, review, err := emptyPresetCatalog().PreparePreset("invalid", PresetInput{Format: "zip", Content: data})
		if err == nil || review.Record != nil {
			t.Errorf("ZIP entry %q was not rejected: %v", extra.name, err)
		}
	}
	input := PresetInput{Format: "json", Content: []byte(`{"license":"MIT","License":"MIT"}`)}
	if _, _, err := emptyPresetCatalog().PreparePreset("invalid", input); err == nil {
		t.Fatal("case-folded duplicate transfer keys accepted")
	}
}

func TestPresetLargeListTransferAndExpansionBudgets(t *testing.T) {
	const entries = 33000
	var domains strings.Builder
	for i := range entries {
		digest := sha256.Sum256(fmt.Appendf(nil, "%d", i))
		fmt.Fprintf(&domains, "%x.fixture.example.test\n", digest[:8])
	}
	archive := presetZIP(t, []zipSource{
		{name: "strategy.txt", contents: "--filter-tcp=443 --hostlist=lists/large.txt"},
		{name: "lists/large.txt", contents: domains.String()},
	})
	if len(archive) <= MaxPresetBytes || domains.Len() <= len(archive) || len(archive) > MaxPresetTransferBytes {
		t.Fatalf("fixture does not exercise independent budgets: compressed=%d, expanded list=%d", len(archive), domains.Len())
	}
	base := emptyPresetCatalog()
	input := PresetInput{Format: "zip", Content: archive}
	if _, preview, err := base.PreparePresetWithin("large", input, MaxPresetBytes); err == nil ||
		!strings.Contains(err.Error(), "import workspace") || preview.Record != nil || len(base.Records) != 0 || len(base.Lists) != 0 {
		t.Fatalf("a small ZIP workspace must reject without changing the catalog: %v", err)
	}
	next, preview, err := base.PreparePresetWithin("large", input, DefaultPresetExpandedBytes)
	if err != nil || preview.Record == nil || len(next.Lists["large"].Entries) != entries || len(base.Records) != 0 {
		t.Fatalf("large list was not imported intact: %v, %+v", err, preview.Issues)
	}
	if _, err := next.ExportPackageWithin("large", MaxPresetBytes); err == nil || !strings.Contains(err.Error(), "expansion budget") {
		t.Fatalf("ZIP export must honor the expansion budget: %v", err)
	}
	backup, err := next.ExportPackage("large")
	if err != nil {
		t.Fatal(err)
	}
	restored, review, err := base.PreparePreset("large", PresetInput{Format: "zip", Content: backup})
	if err != nil || review.Record == nil || restored.Revision() != next.Revision() {
		t.Fatalf("ZIP round-trip lost the strategy or list revision: %v, %+v", err, review.Issues)
	}
	if _, _, err := base.PreparePreset("large", PresetInput{Format: "zip", Content: make([]byte, MaxPresetTransferBytes+1)}); err == nil ||
		!strings.Contains(err.Error(), "transfer limit") {
		t.Fatalf("oversized transfer must be rejected before opening ZIP: %v", err)
	}
}

func TestInactiveAssetsDoNotConsumeTheActivationBudget(t *testing.T) {
	c := emptyPresetCatalog()
	var err error
	for i := range 20 {
		id := fmt.Sprintf("asset-%d", i)
		c, err = c.WithNamedList(id, &NamedList{Kind: IPSetKind, Entries: []string{"198.51.100.1/32"}})
		if err == nil {
			c, err = c.WithNamedBlob(id, stringPointer("4054"))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if c.Validate() != nil || len(c.BlobAssets) != 20 || len(c.Lists) != 20 {
		t.Fatal("catalog rejected valid inactive dependencies")
	}
	for i := range 140 {
		c.BlobAssets[fmt.Sprintf("large-%d", i)] = strings.Repeat("40", MaxNativeBlobBytes)
	}
	data, err := json.Marshal(c)
	if err != nil || len(data) <= 256<<10 || c.Validate() != nil {
		t.Fatalf("valid inactive assets above the old limit did not survive: %d bytes, %v", len(data), err)
	}
}

func TestFrozenTemplatesAdmitOnlyAdaptedDefinitions(t *testing.T) {
	for _, template := range PresetTemplates() {
		t.Run(template.ID, func(t *testing.T) {
			base := emptyPresetCatalog()
			next, review, err := base.PreparePreset("template-copy", PresetInput{Format: "template", TemplateID: template.ID})
			if err != nil || (review.Record != nil) != (template.Compatibility == "adapted") || len(review.Profiles) != template.Profiles {
				t.Fatalf("template metadata does not match owner validation: %+v, %v", review, err)
			}
			if review.Record == nil {
				return
			}
			if !review.Record.VisualEditable() {
				t.Fatal("an adapted template needs to be editable in both modes")
			}
			origin := review.Record.Origin
			if origin == nil || origin.TemplateID != template.ID || origin.ReferenceCommit != template.Commit ||
				origin.ReferenceVersion != template.Version || origin.SHA256 != fixtureDigest(template.source) {
				t.Fatalf("saved template lost its immutable source reference: %+v", origin)
			}
			editedRaw := review.Record.Raw + "# User note\n"
			edited, err := next.SealDraft(Draft{ID: "template-copy", Name: "Renamed", Raw: &editedRaw})
			if err != nil || edited.Origin == nil || *edited.Origin != *origin || edited.Definition.Revision != review.Record.Definition.Revision {
				t.Fatalf("edit replaced template provenance or the action revision: %v", err)
			}
			next.Records["template-copy"] = edited
			data, err := next.ExportPackage("template-copy")
			if err != nil {
				t.Fatal(err)
			}
			copy, preview, err := base.PreparePreset("template-copy", PresetInput{Format: "zip", Content: data})
			if err != nil || preview.Record == nil || copy.Revision() != next.Revision() {
				t.Fatalf("template origin or edited source did not survive the existing ZIP transfer: %+v, %v", preview, err)
			}
		})
	}
}

type zipSource struct {
	name, contents string
	mode           fs.FileMode
}

func presetZIP(t *testing.T, sources []zipSource) []byte {
	t.Helper()
	var bytes bytes.Buffer
	archive := zip.NewWriter(&bytes)
	for _, source := range sources {
		header := &zip.FileHeader{Name: source.name, Method: zip.Deflate}
		header.SetMode(source.mode | 0600)
		file, err := archive.CreateHeader(header)
		if err == nil {
			_, err = file.Write([]byte(source.contents))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.Bytes()
}
