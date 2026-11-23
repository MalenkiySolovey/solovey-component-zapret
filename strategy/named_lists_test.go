package strategy

import (
	"slices"
	"strings"
	"testing"
)

func TestNamedListsResolveAndReviseOnlyConsumers(t *testing.T) {
	c := namedCatalog(t)
	for _, id := range []string{"first", "second", "other"} {
		d := namedDraft(id)
		if id == "other" {
			d.ListRefs = nil
		}
		r, err := c.SealDraft(d)
		if err != nil {
			t.Fatal(err)
		}
		c.Records[id] = r
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	argv, err := c.Records["first"].Definition.Compile("/engine", 10, 1, 2)
	if err != nil || !slices.Contains(argv, "--hostlist-domains=example.test") || !slices.Contains(argv, "--ipset-exclude-ip=192.0.2.0/24") {
		t.Fatalf("unexpected named native lists: %v, %v", argv, err)
	}
	before := c.Revision()
	next, err := c.WithNamedList("names", &NamedList{Kind: HostListKind, Entries: []string{"alt.example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	for id, record := range next.Records {
		prev := c.Records[id]
		if changed := prev.Definition.Revision != record.Definition.Revision; changed != (id != "other") || prev.Raw != record.Raw {
			t.Fatalf("incorrect change for %q", id)
		}
	}
	if before != c.Revision() || before == next.Revision() || next.Validate() != nil {
		t.Fatal("catalog mutation was not sealed or changed its input")
	}
	if _, err := c.WithNamedList("names", nil); err == nil {
		t.Fatal("deletion of a referenced list succeeded")
	}
	if _, err := c.WithNamedList("names", &NamedList{Kind: IPSetKind, Entries: []string{"192.0.2.1"}}); err == nil {
		t.Fatal("replacement with a cross-kind reference succeeded")
	}
}

func TestNamedListsRejectUnresolvedShadowedAndUnboundedData(t *testing.T) {
	c := namedCatalog(t)
	for _, refs := range []ListRefs{
		{"unknown-profile": {HostList: "names"}},
		{"profile-1": {HostList: "ips"}},
		{"profile-1": {HostList: "absent"}},
		{"profile-1": {}},
	} {
		d := namedDraft("test")
		d.ListRefs = refs
		if _, err := c.SealDraft(d); err == nil {
			t.Fatalf("invalid references accepted: %v", refs)
		}
	}
	d := namedDraft("test")
	raw := *d.Raw + " --hostlist-domains=inline.example.test"
	d.Raw = &raw
	if _, err := c.SealDraft(d); err == nil {
		t.Fatal("inline data overshadowed a same-field reference")
	}
	for _, value := range []NamedList{
		{Kind: HostListKind, Entries: []string{"/etc/passwd"}},
		{Kind: IPSetKind, Entries: []string{"192.0.2.1,--queue-bypass"}},
		{Kind: HostListKind, Entries: slices.Repeat([]string{"example.test"}, maxNativeListEntries+1)},
	} {
		if _, err := c.WithNamedList("unsafe", &value); err == nil {
			t.Fatalf("unsafe list data accepted: %v", value)
		}
	}
}

func TestNamedListsPortableTransferRejectsConflictAndStaleRevision(t *testing.T) {
	c := namedCatalog(t)
	d := namedDraft("quic")
	raw := "--filter-udp=443 --filter-l7=quic --payload=quic_initial --lua-desync=fake:blob=fake_default_quic:repeats=1"
	d.Raw = &raw
	r, err := c.SealDraft(d)
	if err != nil {
		t.Fatal(err)
	}
	c.Records[d.ID] = r
	transfer, err := c.Export(d.ID)
	if err != nil || len(transfer.Lists) != 2 || !slices.Equal(transfer.Blobs, []AssetRef{DefaultQUICBlob}) {
		t.Fatalf("missing engine/list provenance: %+v, %v", transfer, err)
	}
	empty := Catalog{Schema: CatalogSchema, Records: make(map[string]Record)}
	owner, err := empty.WithTransfer("new-quic", transfer)
	if err != nil || owner.Validate() != nil || len(owner.Records) != 1 || len(empty.Records) != 0 {
		t.Fatalf("transfer did not preserve the selection: %v", err)
	}
	conflict, err := empty.WithNamedList("names", &NamedList{Kind: HostListKind, Entries: []string{"other.test"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conflict.WithTransfer("new-quic", transfer); err == nil {
		t.Fatal("import overwrote existing asset")
	}
	transfer.Blobs = []AssetRef{"/tmp/blob"}
	if _, err = transfer.Import(); err == nil {
		t.Fatal("import accepted an external blob")
	}
	transfer.Blobs = []AssetRef{DefaultQUICBlob}
	transfer.Lists["names"].Entries[0] = "stale.example.test"
	if _, err = transfer.Import(); err == nil || !strings.Contains(err.Error(), "sealed definition do not match") {
		t.Fatalf("tampering with list contents did not fail revision verification: %v", err)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("export aliased the stored named list: %v", err)
	}
}

func namedDraft(id string) Draft {
	raw := "--filter-tcp=443"
	return Draft{ID: id, Name: id, Raw: &raw, ListRefs: ListRefs{"profile-1": {HostList: "names", IPSetExclude: "ips"}}}
}

func namedCatalog(t *testing.T) Catalog {
	t.Helper()
	c := Catalog{Schema: CatalogSchema, Records: make(map[string]Record)}
	var err error
	c, err = c.WithNamedList("names", &NamedList{Kind: HostListKind, Entries: []string{"EXAMPLE.TEST"}})
	if err == nil {
		c, err = c.WithNamedList("ips", &NamedList{Kind: IPSetKind, Entries: []string{"192.0.2.3/24"}})
	}
	if err != nil {
		t.Fatal(err)
	}
	return c
}
