package strategy

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	CatalogSchema  = "solovey.zapret-catalog/v1"
	TransferSchema = "solovey.zapret-transfer/v1"
	EngineCommit   = "038c97e5819df10f8e8c22a2f1fffe57f2f7379b"
	EngineLicense  = "MIT"
	CatalogLimit   = 16
)

// Record retains raw editor text and a separate sealed runtime definition.
// Only Definition is ever sent across the privileged boundary.
type Record struct {
	Name       string   `json:"name"`
	Raw        string   `json:"raw"`
	Definition Strategy `json:"definition"`
	ListRefs   ListRefs `json:"listRefs,omitempty"`
}

type Catalog struct {
	Schema     string               `json:"schema"`
	Records    map[string]Record    `json:"records"`
	Lists      map[string]NamedList `json:"lists,omitempty"`
	BlobAssets map[string]string    `json:"blobAssets,omitempty"`
}

type Draft struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Raw      *string   `json:"raw,omitempty"`
	Profiles []Profile `json:"profiles,omitempty"`
	ListRefs ListRefs  `json:"listRefs,omitempty"`
}

// Transfer binds the strategy to the immutable engine and assets. Referenced
// native data travels with the strategy; imports install no files or scripts.
type Transfer struct {
	Schema       string               `json:"schema"`
	EngineCommit string               `json:"engineCommit"`
	License      string               `json:"license"`
	Assets       []AssetRef           `json:"assets,omitempty"`
	Blobs        []AssetRef           `json:"blobs,omitempty"`
	Strategy     Record               `json:"strategy"`
	Lists        map[string]NamedList `json:"lists,omitempty"`
	BlobAssets   map[string]string    `json:"blobAssets,omitempty"`
}

func SealDraft(d Draft) (Record, error) { return (Catalog{}).SealDraft(d) }

func (c Catalog) SealDraft(d Draft) (Record, error) {
	if d.Name == "" || len(d.Name) > 120 || !utf8.ValidString(d.Name) || strings.TrimSpace(d.Name) != d.Name || strings.ContainsFunc(d.Name, unicode.IsControl) {
		return Record{}, errors.New("enter a strategy name of at most 120 UTF-8 bytes")
	}
	var raw string
	if d.Raw != nil {
		if len(d.Profiles) != 0 {
			return Record{}, errors.New("provide raw text or ordered profiles, not both")
		}
		raw = *d.Raw
	} else {
		compiled := Strategy{Schema: Schema, ID: d.ID, Profiles: d.Profiles, Assets: nativeAssets(d.Profiles)}
		compiled, err := c.resolveBlobs(compiled)
		if err != nil {
			return Record{}, err
		}
		raw, err = FormatNative(compiled)
		if err != nil {
			return Record{}, err
		}
	}
	definition, err := parseNative(d.ID, raw)
	if err != nil {
		return Record{}, err
	}
	definition, err = c.resolveBlobs(definition)
	if err != nil {
		return Record{}, err
	}
	definition, err = Seal(definition)
	if err != nil {
		return Record{}, err
	}
	return c.withResolvedLists(Record{Name: d.Name, Raw: raw, Definition: definition, ListRefs: d.ListRefs})
}

func (r Record) Validate() error { return (Catalog{}).validateRecord(r) }

func (c Catalog) validateRecord(r Record) error {
	checked, err := c.SealDraft(Draft{ID: r.Definition.ID, Name: r.Name, Raw: &r.Raw, ListRefs: r.ListRefs})
	if err != nil {
		return err
	}
	if _, err := r.Definition.Verified(); err != nil || r.Definition.Revision != checked.Definition.Revision {
		return errors.New("strategy native text and sealed definition do not match")
	}
	return nil
}

// Export creates a standalone transfer for a record without named lists.
// Use Catalog.Export when a record references catalog-owned lists.
func (r Record) Export() Transfer {
	return Transfer{Schema: TransferSchema, EngineCommit: EngineCommit, License: EngineLicense,
		Assets: r.Definition.Assets, Blobs: r.Definition.BlobRefs(), Strategy: r, BlobAssets: maps.Clone(r.Definition.BlobData)}
}

func (t Transfer) Import() (Draft, error) {
	if t.Schema != TransferSchema || t.EngineCommit != EngineCommit || t.License != EngineLicense || len(t.Assets) != len(t.Strategy.Definition.Assets) {
		return Draft{}, errors.New("unsupported Zapret engine, asset or license provenance")
	}
	for i, asset := range t.Assets {
		if asset != t.Strategy.Definition.Assets[i] {
			return Draft{}, errors.New("strategy asset references do not match its definition")
		}
	}
	if len(t.Blobs) != 0 {
		if refs := t.Strategy.Definition.BlobRefs(); len(refs) != 1 || len(t.Blobs) != 1 || t.Blobs[0] != refs[0] {
			return Draft{}, errors.New("strategy blob does not match its pinned native action")
		}
	}
	c := Catalog{Lists: t.Lists, BlobAssets: t.BlobAssets}
	if err := c.validateLists(); err != nil {
		return Draft{}, err
	}
	if err := c.validateBlobAssets(); err != nil {
		return Draft{}, err
	}
	if err := c.validateRecord(t.Strategy); err != nil {
		return Draft{}, err
	}
	if len(t.Lists) != len(namedListIDs(t.Strategy.ListRefs)) {
		return Draft{}, errors.New("transfer includes unused native lists")
	}
	if len(t.BlobAssets) != len(t.Strategy.Definition.BlobData) {
		return Draft{}, errors.New("transfer includes unused native blobs")
	}
	return Draft{ID: t.Strategy.Definition.ID, Name: t.Strategy.Name, Raw: &t.Strategy.Raw, ListRefs: t.Strategy.ListRefs}, nil
}

func (c Catalog) Validate() error {
	if c.Schema != CatalogSchema || c.Records == nil || len(c.Records) > CatalogLimit {
		return errors.New("invalid Zapret catalog or too many strategies")
	}
	if err := c.validateLists(); err != nil {
		return err
	}
	if err := c.validateBlobAssets(); err != nil {
		return err
	}
	for id, record := range c.Records {
		if id != record.Definition.ID {
			return errors.New("invalid Zapret catalog strategy identity")
		}
		if err := c.validateRecord(record); err != nil {
			return fmt.Errorf("strategy %q: %w", id, err)
		}
	}
	return nil
}

// Revision fences editor changes, including changes to names or whitespace
// that intentionally leave the runtime definition unchanged.
func (c Catalog) Revision() string {
	data, _ := json.Marshal(c)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
