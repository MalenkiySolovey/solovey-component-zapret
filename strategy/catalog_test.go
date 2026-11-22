package strategy

import "testing"

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
