package strategy

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
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
}

type Catalog struct {
	Schema  string            `json:"schema"`
	Records map[string]Record `json:"records"`
}

type Draft struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Raw      *string   `json:"raw,omitempty"`
	Profiles []Profile `json:"profiles,omitempty"`
}

// Transfer describes the only assets this profile can use. It carries no
// executable content or filesystem paths. On import these fields must match
// the immutable engine input; neither asset is installed by the editor.
type Transfer struct {
	Schema       string     `json:"schema"`
	EngineCommit string     `json:"engineCommit"`
	License      string     `json:"license"`
	Assets       []AssetRef `json:"assets,omitempty"`
	Strategy     Record     `json:"strategy"`
}

func SealDraft(d Draft) (Record, error) {
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
		compiled := Strategy{Schema: Schema, ID: d.ID, Profiles: d.Profiles}
		for _, p := range d.Profiles {
			if p.Protocol != intent.PacketProtocolTCP {
				return Record{}, errors.New("the installed editor supports TCP profiles only")
			}
			if p.SplitAt != 0 {
				compiled.Assets = []AssetRef{Library, Actions}
			}
		}
		var err error
		raw, err = FormatNative(compiled)
		if err != nil {
			return Record{}, err
		}
	}
	definition, err := ParseNative(d.ID, raw)
	if err != nil {
		return Record{}, err
	}
	return Record{Name: d.Name, Raw: raw, Definition: definition}, nil
}

func (r Record) Validate() error {
	checked, err := SealDraft(Draft{ID: r.Definition.ID, Name: r.Name, Raw: &r.Raw})
	if err != nil {
		return err
	}
	if _, err := r.Definition.Verified(); err != nil || r.Definition.Revision != checked.Definition.Revision {
		return errors.New("strategy native text and sealed definition do not match")
	}
	return nil
}

func (r Record) Export() Transfer {
	return Transfer{Schema: TransferSchema, EngineCommit: EngineCommit, License: EngineLicense,
		Assets: r.Definition.Assets, Strategy: r}
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
	if err := t.Strategy.Validate(); err != nil {
		return Draft{}, err
	}
	return Draft{ID: t.Strategy.Definition.ID, Name: t.Strategy.Name, Raw: &t.Strategy.Raw}, nil
}

func (c Catalog) Validate() error {
	if c.Schema != CatalogSchema || c.Records == nil || len(c.Records) > CatalogLimit {
		return errors.New("invalid Zapret catalog or too many strategies")
	}
	for id, record := range c.Records {
		if id != record.Definition.ID {
			return errors.New("invalid Zapret catalog strategy identity")
		}
		if err := record.Validate(); err != nil {
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
