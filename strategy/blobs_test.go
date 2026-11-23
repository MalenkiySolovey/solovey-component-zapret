package strategy

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
)

const referenceRaw = "--filter-udp=443 --filter-l7=quic --payload=quic_initial --lua-desync=fake:blob=asset_fixture:repeats=1"

func TestNativeBlobLifecycle(t *testing.T) {
	c := Catalog{Schema: CatalogSchema, Records: map[string]Record{}}
	data := "40ABff"
	c, err := c.WithNamedBlob("fixture", &data)
	if err != nil {
		t.Fatal(err)
	}
	raw := referenceRaw
	a, err := c.SealDraft(Draft{ID: "fixture-a", Name: "A", Raw: &raw})
	if err != nil || a.Definition.BlobData["fixture"] != "40abff" || a.Definition.HandlesQUIC(IPv4, 443) {
		t.Fatalf("raw reference = %+v, err = %v", a, err)
	}
	b, err := c.SealDraft(Draft{ID: "fixture-b", Name: "B", Profiles: []Profile{
		{ID: "quic", Protocol: intent.PacketProtocolUDP, Ports: []uint16{443}, Payload: QUICInitial, QUICRepeats: 1, QUICBlobRef: "fixture"},
	}})
	if err != nil || !strings.Contains(b.Raw, "blob=asset_fixture:repeats=1") || strings.Contains(b.Raw, "40abff") {
		t.Fatalf("visual reference = %+v, err = %v", b, err)
	}
	c.Records[a.Definition.ID] = a
	c.Records[b.Definition.ID] = b
	if _, err := c.WithNamedBlob("fixture", nil); err == nil {
		t.Fatal("deleted a referenced blob")
	}
	changed := "407f00"
	next, err := c.WithNamedBlob("fixture", &changed)
	if err != nil {
		t.Fatal(err)
	}
	for id, old := range c.Records {
		updated := next.Records[id]
		if old.Definition.Revision == updated.Definition.Revision || old.Raw != updated.Raw ||
			old.Definition.BlobData["fixture"] != "40abff" || updated.Definition.BlobData["fixture"] != changed {
			t.Fatalf("bad reseal for %s", id)
		}
	}
	transfer, err := next.Export(a.Definition.ID)
	if err != nil || transfer.BlobAssets["fixture"] != changed || len(transfer.Blobs) != 0 {
		t.Fatalf("export lost provenance: %+v, %v", transfer, err)
	}
	if _, err := c.WithTransfer("fixture-copy", transfer); err == nil {
		t.Fatal("import silently replaced a local blob")
	}
	copy, err := next.WithTransfer("fixture-copy", transfer)
	if err != nil || copy.Records["fixture-copy"].Definition.BlobData["fixture"] != changed {
		t.Fatalf("import failed: %v", err)
	}
	transfer.BlobAssets["fixture"] = "406600"
	if _, err := transfer.Import(); err == nil || next.BlobAssets["fixture"] != changed {
		t.Fatal("changed transfer data was accepted or aliased the catalog")
	}
}

func TestNativeBlobArgumentIsolation(t *testing.T) {
	seal := func(id, data string) Strategy {
		t.Helper()
		c := Catalog{BlobAssets: map[string]string{"fixture": data}}
		raw := referenceRaw
		record, err := c.SealDraft(Draft{ID: id, Name: id, Raw: &raw})
		if err != nil {
			t.Fatal(err)
		}
		return record.Definition
	}
	a, b := seal("a", "400102"), seal("b", "400304")
	argv, err := CompileBindings("/engine", 42, 0x40000000, []MarkedStrategy{{a, 0x10000}, {b, 0x20000}, {a, 0x30000}})
	if err != nil {
		t.Fatal(err)
	}
	var blobs, actions []string
	for _, arg := range argv {
		switch {
		case strings.HasPrefix(arg, "--blob="):
			blobs = append(blobs, arg)
		case strings.HasPrefix(arg, "--lua-desync="):
			actions = append(actions, arg)
		}
	}
	if !slices.Equal(blobs, []string{"--blob=solovey_blob_1:0x400102", "--blob=solovey_blob_2:0x400304"}) ||
		!slices.Equal(actions, []string{"--lua-desync=fake:blob=solovey_blob_1:repeats=1", "--lua-desync=fake:blob=solovey_blob_2:repeats=1", "--lua-desync=fake:blob=solovey_blob_1:repeats=1"}) {
		t.Fatalf("wrong engine asset isolation: %v, %v", blobs, actions)
	}
	a.BlobData["fixture"] = "400099"
	if _, err := a.Verified(); err == nil {
		t.Fatal("accepted a changed blob under a stale runtime revision")
	}
}

func TestNativeBlobRejectsUnsafeInput(t *testing.T) {
	c := Catalog{Schema: CatalogSchema, Records: map[string]Record{}}
	for _, data := range []string{"", "0x41", "A", "@/etc/passwd", "41 42", strings.Repeat("ab", MaxNativeBlobBytes+1)} {
		if _, err := c.WithNamedBlob("fixture", &data); err == nil {
			t.Errorf("accepted noncanonical or oversized blob (%d characters)", len(data))
		}
	}
	for _, raw := range []string{referenceRaw, referenceRaw + " --blob=asset_fixture:@/etc/passwd", strings.Replace(referenceRaw, "asset_fixture", "asset_../fixture", 1)} {
		if _, err := c.SealDraft(Draft{ID: "fixture", Name: "Fixture", Raw: &raw}); err == nil {
			t.Errorf("accepted unresolved reference or file input: %q", raw)
		}
	}
	c.BlobAssets = map[string]string{"fixture": strings.Repeat("ab", MaxNativeBlobBytes), "another": "00"}
	raw := referenceRaw + " --new " + strings.Replace(referenceRaw, "asset_fixture", "asset_another", 1)
	if _, err := c.SealDraft(Draft{ID: "fixture", Name: "Fixture", Raw: &raw}); err == nil {
		t.Fatal("accepted oversized aggregate blob data")
	}
	raw = referenceRaw
	r, err := c.SealDraft(Draft{ID: "fixture", Name: "Fixture", Raw: &raw})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(r.Definition)
	if err != nil || len(encoded) > maxBlobDefinitionBytes {
		t.Fatalf("maximum blob does not fit installed broker limit: length=%d err=%v", len(encoded), err)
	}
}
