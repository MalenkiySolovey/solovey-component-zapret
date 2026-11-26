package strategy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLuaDraftsAreInertAndUseOnlyPerSourceInputBudget(t *testing.T) {
	base := Catalog{Schema: CatalogSchema, Records: map[string]Record{}}
	const raw = "--name=first\n--filter-tcp=443\n"
	var err error
	base.Records["baseline"], err = base.SealDraft(Draft{ID: "baseline", Name: "Baseline", Raw: new(raw)})
	if err != nil {
		t.Fatal(err)
	}
	draft := LuaDraft{Origin: "zapretgui", SourceRef: "Operator-supplied copy, unverified", Source: "-- comment\r\nthis is intentionally not Lua syntax\r\n"}
	next, err := base.WithLuaDraft("vendor-example", &draft)
	if err != nil || next.Revision() == base.Revision() || len(base.LuaDrafts) != 0 || len(next.Records) != 1 {
		t.Fatalf("draft was not stored independently: %v", err)
	}
	export, err := next.Export("baseline")
	if err != nil || export.Strategy.Raw != raw || export.Strategy.Definition.Revision != base.Records["baseline"].Definition.Revision {
		t.Fatalf("existing strategy changed: %v", err)
	}
	data, err := json.Marshal(next)
	var restored Catalog
	if err != nil || json.Unmarshal(data, &restored) != nil || restored.Validate() != nil || restored.LuaDrafts["vendor-example"] != draft {
		t.Fatal("catalog round trip did not preserve the draft")
	}
	if _, err := next.WithLuaDraft("full-size", &LuaDraft{Origin: "administrator", Source: strings.Repeat("a", MaxLuaSourceBytes)}); err != nil {
		t.Fatalf("source at the per-source limit should fit alongside other entries: %v", err)
	}
	if _, err := next.WithLuaDraft("overflow", &LuaDraft{Origin: "administrator", Source: strings.Repeat("a", MaxLuaSourceBytes+1)}); err == nil || !strings.Contains(err.Error(), "one source is limited") {
		t.Fatalf("missing per-source limit: %v", err)
	}
	if _, err := next.WithLuaDraft("binary", &LuaDraft{Origin: "administrator", Source: "\x1bLua"}); err == nil {
		t.Fatal("accepted compiled bytecode as source text")
	}
	if _, err := next.WithLuaDraft("non-utf8", &LuaDraft{Origin: "administrator", Source: "\xff"}); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
	if _, err := next.WithLuaDraft("empty", &LuaDraft{Origin: "administrator"}); err != nil {
		t.Fatalf("cannot start a new empty draft: %v", err)
	}
	if _, err := next.WithLuaDraft("external", &LuaDraft{Origin: "external", Source: draft.Source}); err == nil {
		t.Fatal("accepted external source without a provenance note")
	}
	if removed, err := next.WithLuaDraft("vendor-example", nil); err != nil || removed.Revision() != base.Revision() || len(next.LuaDrafts) != 1 {
		t.Fatalf("draft removal mutated the previous catalog or Strategy: %v", err)
	}
}
