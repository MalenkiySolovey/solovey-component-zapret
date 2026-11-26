package strategy

import (
	"encoding/json"
	"fmt"
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
	r, err := c.SealDraft(Draft{ID: "fixture", Name: "Fixture", Raw: &raw})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(r.Definition)
	if err != nil || len(encoded) > MaxDefinitionBytes {
		t.Fatalf("two bounded blobs do not fit installed broker limit: length=%d err=%v", len(encoded), err)
	}
}

func namedBlobBudgetFixture(count, bytesPerBlob int) (Catalog, Draft) {
	c := Catalog{Schema: CatalogSchema, Records: map[string]Record{}, BlobAssets: make(map[string]string, count)}
	d := Draft{ID: "blob-budget", Name: "Blob budget fixture", Profiles: make([]Profile, count)}
	for i := range d.Profiles {
		id := fmt.Sprintf("fixture-%d", i)
		c.BlobAssets[id] = fmt.Sprintf("%02x", 0x40+i) + strings.Repeat("00", bytesPerBlob-1)
		d.Profiles[i] = Profile{ID: id, Protocol: intent.PacketProtocolUDP, Ports: []uint16{443 + uint16(i)},
			Payload: QUICInitial, QUICRepeats: 1, QUICBlobRef: id}
	}
	return c, d
}

func TestNativeBlobsUseDefinitionAndArgumentBudgets(t *testing.T) {
	// Nine references and 1,260 total bytes do not oversize one datagram.
	const count = 9
	c, d := namedBlobBudgetFixture(count, 140)
	r, err := c.SealDraft(d)
	if err != nil || len(r.Definition.BlobData) != count {
		t.Fatalf("seal managed blobs: count=%d err=%v", len(r.Definition.BlobData), err)
	}
	c.Records[d.ID] = r
	transfer, err := c.Export(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := (Catalog{Schema: CatalogSchema, Records: map[string]Record{}}).WithTransfer("imported", transfer)
	if err != nil || len(imported.Records["imported"].Definition.BlobData) != count {
		t.Fatalf("import managed blobs: %v", err)
	}
	argv, err := r.Definition.Compile("/engine", 42, 0x10000, 0x40000000)
	if err != nil || strings.Count(strings.Join(argv, "\n"), "--blob=solovey_blob_") != count {
		t.Fatalf("compile managed blobs: %v", err)
	}
	oversized, draft := namedBlobBudgetFixture(count, MaxNativeBlobBytes)
	if _, err := oversized.SealDraft(draft); err == nil || !strings.Contains(err.Error(), "installation limit is 8192 bytes") {
		t.Fatalf("expected a definition envelope error, got %v", err)
	}
}
