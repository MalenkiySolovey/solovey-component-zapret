package strategy

import (
	"fmt"
	"strings"
	"testing"
)

func TestCatalogRawRevisionAndTransfer(t *testing.T) {
	const a = "--filter-tcp=443  \r\n--filter-l7=tls --payload=tls_client_hello --lua-desync=multisplit:pos=9\n"
	raw := a
	r, err := SealDraft(Draft{ID: "tls", Name: "TLS", Raw: &raw})
	if err != nil || r.Raw != a {
		t.Fatalf("seal: %v", err)
	}
	c := Catalog{Schema: CatalogSchema, Records: map[string]Record{"tls": r}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	before := c.Revision()
	d, err := r.Export().Import()
	if err != nil || d.Raw == nil || *d.Raw != a {
		t.Fatalf("import: %v", err)
	}
	r.Raw += " \n"
	c.Records["tls"] = r
	if err := c.Validate(); err != nil || c.Revision() == before {
		t.Fatalf("raw-only revision was lost: %v", err)
	}
	bad := r.Export()
	bad.EngineCommit = "upstream/latest"
	if _, err := bad.Import(); err == nil {
		t.Fatal("unverified engine provenance accepted")
	}
	r.Raw = "--filter-tcp=125"
	c.Records["tls"] = r
	if err := c.Validate(); err == nil {
		t.Fatal("backup accepted inconsistent raw text and runtime definition")
	}
}

func TestCatalogCapacityFollowsStoredBytesNotRecordCount(t *testing.T) {
	raw := "--filter-tcp=443"
	record, err := SealDraft(Draft{ID: "fixture", Name: "Unused example", Raw: &raw})
	if err != nil {
		t.Fatal(err)
	}
	c := Catalog{Schema: CatalogSchema, Records: make(map[string]Record)}
	for i := range 20 {
		c, err = c.WithTransfer(fmt.Sprintf("strategy-%d", i), record.Export())
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Validate(); err != nil || len(c.Records) != 20 {
		t.Fatalf("small records should fit: %v", err)
	}

	raw += strings.Repeat(" ", MaxNativeBytes-len(raw))
	for i := range MaxCatalogBytes/MaxNativeBytes + 1 {
		id := fmt.Sprintf("long-%d", i)
		r, err := SealDraft(Draft{ID: id, Name: "Long but valid", Raw: &raw})
		if err != nil {
			t.Fatal(err)
		}
		c.Records[id] = r
	}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "storage limit") {
		t.Fatalf("oversized catalog did not report its byte budget: %v", err)
	}
}

func TestAuthoringUsesTheSealedDefinitionBudget(t *testing.T) {
	raw := "--filter-tcp=100-163" + strings.Repeat(" --new --filter-tcp=100-163", 40)
	draft := Draft{ID: "fixture", Name: "Ranges", Raw: &raw}
	if _, err := SealDraft(draft); err == nil || !strings.Contains(err.Error(), "installation limit") {
		t.Fatalf("definition expansion did not report its install budget: %v", err)
	}
	value, err := ParseNative(draft.ID, raw)
	if err != nil {
		t.Fatal(err)
	}
	r := Record{Name: draft.Name, Raw: raw, Definition: value}
	if err := r.Validate(); err != nil {
		t.Fatalf("a stored definition did not remain readable: %v", err)
	}
}
