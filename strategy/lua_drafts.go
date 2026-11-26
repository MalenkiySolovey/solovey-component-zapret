package strategy

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-host/outbounds"
)

// MaxLuaSourceBytes bounds the ingestion of one Lua source, not the library.
const MaxLuaSourceBytes = 256 << 10

// LuaDraft is inert authoring data. Execution requires separate selection of
// an exact revision and an explicit grant from the installed Host.
type LuaDraft struct {
	Origin    string `json:"origin"`
	SourceRef string `json:"sourceRef,omitempty"`
	Source    string `json:"source"`
	Imported  bool   `json:"imported,omitempty"`
}

func (d LuaDraft) RequiresApproval() bool { return d.Imported || d.Origin != "administrator" }

func (d LuaDraft) validate(id string) error {
	if !outbounds.ValidZapretStrategyRef(id) {
		return errors.New("invalid Lua draft ID")
	}
	switch d.Origin {
	case "administrator":
	case "zapretgui", "external":
	default:
		return errors.New("select a Lua draft origin")
	}
	if d.RequiresApproval() && d.SourceRef == "" {
		return errors.New("record the Lua source, version or that the origin is unknown")
	}
	if len(d.SourceRef) > 240 || !utf8.ValidString(d.SourceRef) || strings.TrimSpace(d.SourceRef) != d.SourceRef || strings.ContainsFunc(d.SourceRef, unicode.IsControl) {
		return errors.New("Lua source reference must be one line of at most 240 UTF-8 bytes")
	}
	if len(d.Source) > MaxLuaSourceBytes {
		return fmt.Errorf("Lua source has %d bytes; one source is limited to %d bytes", len(d.Source), MaxLuaSourceBytes)
	}
	if !utf8.ValidString(d.Source) || strings.ContainsRune(d.Source, '\x00') || strings.HasPrefix(d.Source, "\x1bLua") {
		return errors.New("Lua draft must be UTF-8 source text, not compiled bytecode")
	}
	return nil
}

func (c Catalog) WithLuaDraft(id string, draft *LuaDraft) (Catalog, error) {
	if err := c.Validate(); err != nil {
		return Catalog{}, err
	}
	c = c.retainLuaRevision(id)
	c.LuaDrafts = maps.Clone(c.LuaDrafts)
	if draft == nil {
		if _, ok := c.LuaDrafts[id]; !ok {
			return Catalog{}, errors.New("Lua draft does not exist")
		}
		delete(c.LuaDrafts, id)
	} else {
		next := *draft
		if old, exists := c.LuaDrafts[id]; exists {
			next.Imported = next.Imported || old.Imported || (old.RequiresApproval() && next.Origin == "administrator")
		}
		if err := next.validate(id); err != nil {
			return Catalog{}, err
		}
		if c.LuaDrafts == nil {
			c.LuaDrafts = make(map[string]LuaDraft)
		}
		c.LuaDrafts[id] = next
	}
	c = c.PruneLuaRevisions()
	return c, nil
}
