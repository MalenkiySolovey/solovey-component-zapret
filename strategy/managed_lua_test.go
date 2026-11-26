package strategy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManagedLuaRequiresExactSelectedSources(t *testing.T) {
	source := "-- user quotes: \"\\\r\nfunction check() return VERDICT_PASS end\n"
	module := LuaModule{"example", LuaDraft{Origin: "external", SourceRef: "fixture", Source: source}, LuaSourceDigest(source), ""}
	if _, _, err := CompileManagedLua(0, []LuaModule{module}, []string{"check"}); err == nil {
		t.Fatal("imported source was selected without approval")
	}
	module.ApprovedDigest = module.Digest
	compiled, entries, err := CompileManagedLua(3, []LuaModule{module}, []string{"check", "another_check"})
	if err != nil || len(entries) != 2 || entries[0] != "solovey_managed_3_0" || entries[1] != "solovey_managed_3_1" ||
		!strings.Contains(compiled, `\013\010function check`) {
		t.Fatalf("managed Lua compiler: %v, %v", entries, err)
	}
	if _, _, err := CompileManagedLua(0, []LuaModule{module, module}, []string{"check"}); err == nil {
		t.Fatal("duplicate module ID accepted")
	}
	if _, _, err := CompileManagedLua(0, []LuaModule{module}, []string{"check; return"}); err == nil {
		t.Fatal("non-identifier accepted as a function name")
	}
	module.Draft.Source += "\n-- changed"
	if _, _, err := CompileManagedLua(0, []LuaModule{module}, []string{"check"}); err == nil {
		t.Fatal("changed source was accepted by a stale selector")
	}
}

func TestManagedLuaFileImportRetainsProvenanceOnEdit(t *testing.T) {
	base := Catalog{Schema: CatalogSchema, Records: map[string]Record{}}
	source := LuaDraft{Origin: "administrator", Source: "function inspect() return VERDICT_PASS end", Imported: true}
	if _, err := base.WithLuaDraft("operator-file", &source); err == nil || !strings.Contains(err.Error(), "record the Lua source") {
		t.Fatalf("file import without provenance was not refused: %v", err)
	}
	source.SourceRef = "Operator file fixture"
	catalog, err := base.WithLuaDraft("operator-file", &source)
	if err != nil {
		t.Fatal(err)
	}
	change := source
	change.Imported = false
	change.Source += "\n-- edited"
	catalog, err = catalog.WithLuaDraft("operator-file", &change)
	if err != nil {
		t.Fatal(err)
	}
	stored := catalog.LuaDrafts["operator-file"]
	ref := LuaReference{ID: "operator-file", Revision: stored.Revision()}
	if !stored.Imported || stored.Source != change.Source || ref.Revision == source.Revision() {
		t.Fatalf("edit removed provenance or failed to save a new revision: %+v", stored)
	}
	if _, err := catalog.SelectedLua([]LuaReference{ref}); err == nil || !strings.Contains(err.Error(), "explicitly authorize") {
		t.Fatalf("edited import did not require approval: %v", err)
	}
}

func TestManagedLuaBindingSnapshot(t *testing.T) {
	const id = "observe"
	const raw = "--filter-tcp=443 --filter-l7=tls --payload=tls_client_hello --lua-desync=inspect:first=1"
	original := LuaDraft{Origin: "external", SourceRef: "operator-reviewed fixture", Source: "function inspect() return VERDICT_PASS end"}
	catalog := Catalog{Schema: CatalogSchema, Records: map[string]Record{}, LuaDrafts: map[string]LuaDraft{id: original}}
	draft := Draft{ID: "controlled", Name: "Controlled Lua", Raw: new(raw), ManagedLua: []LuaReference{{ID: id, Revision: original.Revision()}}}
	if _, err := catalog.SealDraft(draft); err != nil {
		t.Fatalf("unapproved selection should remain editable: %v", err)
	}
	if _, err := catalog.SelectedLua(draft.ManagedLua); err == nil {
		t.Fatal("imported Lua became executable without approval")
	}
	draft.ManagedLua[0].ApproveImported = true
	record, err := catalog.SealDraft(draft)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Records[draft.ID] = record
	updated := LuaDraft{Origin: "external", SourceRef: original.SourceRef, Source: original.Source + "\n-- revised"}
	catalog, err = catalog.WithLuaDraft(id, &updated)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	var restored Catalog
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if err := restored.Validate(); err != nil {
		t.Fatal(err)
	}
	modules, err := restored.SelectedLua(record.Definition.ManagedLua)
	if err != nil || len(modules) != 1 || modules[0].Draft != original {
		t.Fatalf("edited draft silently replaced the selected revision: %v", err)
	}
	bindings := []MarkedStrategy{{Strategy: record.Definition, QueueMark: 0x120000}}
	if _, err := CompileBindings("/engine", 401, 0x40000000, bindings); err == nil {
		t.Fatal("managed Lua compiled without a broker-owned bundle")
	}
	args, _, program, err := CompileManagedBindings("/engine", "/run/managed.lua", 401, 0x40000000, bindings, [][]LuaModule{modules}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, expected := range []string{"--lua-init=@/run/managed.lua", "--filter-mark=0x120000", "--lua-desync=solovey_managed_0_0:first=1"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("compiled invocation missing %s: %s", expected, joined)
		}
	}
	if !strings.Contains(program, original.Source) || strings.Contains(program, "-- revised") {
		t.Fatal("managed bundle did not use the exact selected source")
	}
	modules[0].Draft = updated
	if _, _, _, err := CompileManagedBindings("/engine", "/run/managed.lua", 401, 0x40000000, bindings, [][]LuaModule{modules}, false, false); err == nil {
		t.Fatal("a mismatched revision compiled")
	}
}

func TestManagedLuaPackageDoesNotTransferApproval(t *testing.T) {
	const id = "strategy"
	const stored = "same-id"
	const raw = "--filter-tcp=18080 --lua-desync=inspect"
	local := LuaDraft{Origin: "administrator", Source: "function inspect() return VERDICT_PASS end\n"}
	library := LuaDraft{Origin: "external", SourceRef: "Reviewed fixture", Source: "return {version = 1}\n"}
	source := Catalog{Schema: CatalogSchema, Records: map[string]Record{}, LuaDrafts: map[string]LuaDraft{stored: local, "lib": library}}
	refs := []LuaReference{{ID: "lib", Revision: library.Revision(), ApproveImported: true}, {ID: stored, Revision: local.Revision()}}
	record, err := source.SealDraft(Draft{ID: id, Name: "Lua transfer", Raw: new(raw), ManagedLua: refs})
	if err != nil {
		t.Fatal(err)
	}
	source.Records[id] = record
	transfer, err := source.Export(id)
	if err != nil {
		t.Fatal(err)
	}
	if transfer.Strategy.Definition.ManagedLua[0].ApproveImported || !record.Definition.ManagedLua[0].ApproveImported || len(transfer.LuaSources) != 2 {
		t.Fatal("export carried an approval or changed the original selection")
	}
	zip, err := source.ExportPackage(id)
	if err != nil {
		t.Fatal(err)
	}
	other := LuaDraft{Origin: "administrator", Source: "-- keep existing draft\n"}
	destination := Catalog{Schema: CatalogSchema, Records: map[string]Record{}, LuaDrafts: map[string]LuaDraft{stored: other}}
	imported, review, err := destination.PreparePreset("copy", PresetInput{Format: "zip", Content: zip})
	if err != nil || review.Record == nil || len(review.Dependencies) != 2 || review.Dependencies[0].State != "needs-approval" {
		t.Fatalf("ZIP was not reviewed and imported as inert: %+v, %v", review.Issues, err)
	}
	selections := review.Record.Definition.ManagedLua
	if len(selections) != 2 || selections[0].ID != "lib" || selections[1].ID != stored || imported.LuaDrafts[stored] != other ||
		imported.LuaRevisions[selections[1].key()].Source != local.Source || !imported.LuaRevisions[selections[1].key()].Imported {
		t.Fatal("import changed dependency order, source bytes, or a pre-existing draft")
	}
	if _, err := imported.SelectedLua(selections); err == nil {
		t.Fatal("import implicitly approved executable source")
	}
	selections[0].ApproveImported = true
	if _, err := imported.SelectedLua(selections); err == nil {
		t.Fatal("an administrator-origin source from another catalog became trusted")
	}
	selections[1].ApproveImported = true
	modules, err := imported.SelectedLua(selections)
	if err != nil || modules[0].Draft != library || modules[1].Draft.Source != local.Source || modules[1].Draft.SourceRef == "" {
		t.Fatalf("separate approval did not recover exact sources and provenance: %v", err)
	}
	// Even a transfer constructed with a previously approved sealed record is inert.
	transfer.Strategy = record
	fromJSON, err := destination.WithTransfer("json-copy", transfer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fromJSON.SelectedLua(fromJSON.Records["json-copy"].Definition.ManagedLua); err == nil {
		t.Fatal("JSON import honored an approval from the original catalog")
	}
}
