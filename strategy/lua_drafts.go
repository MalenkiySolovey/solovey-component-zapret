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

// LuaDraft is authoring data. It is never referenced by a Strategy, passed to
// a Lua interpreter, or written to any runtime engine path.
type LuaDraft struct {
	Origin    string `json:"origin"`
	SourceRef string `json:"sourceRef,omitempty"`
	Source    string `json:"source"`
}

func (d LuaDraft) validate(id string) error {
	if !outbounds.ValidZapretStrategyRef(id) {
		return errors.New("invalid Lua draft ID")
	}
	switch d.Origin {
	case "administrator":
	case "zapretgui", "external":
		if d.SourceRef == "" {
			return errors.New("record the Lua source, version or that the origin is unknown")
		}
	default:
		return errors.New("select a Lua draft origin")
	}
	if len(d.SourceRef) > 240 || !utf8.ValidString(d.SourceRef) || strings.TrimSpace(d.SourceRef) != d.SourceRef || strings.ContainsFunc(d.SourceRef, unicode.IsControl) {
		return errors.New("Lua source reference must be one line of at most 240 UTF-8 bytes")
	}
	if len(d.Source) > MaxCatalogBytes {
		return fmt.Errorf("Lua source has %d bytes; the entire catalog has a %d-byte limit", len(d.Source), MaxCatalogBytes)
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
	c.LuaDrafts = maps.Clone(c.LuaDrafts)
	if draft == nil {
		if _, ok := c.LuaDrafts[id]; !ok {
			return Catalog{}, errors.New("Lua draft does not exist")
		}
		delete(c.LuaDrafts, id)
	} else {
		if err := draft.validate(id); err != nil {
			return Catalog{}, err
		}
		if c.LuaDrafts == nil {
			c.LuaDrafts = make(map[string]LuaDraft)
		}
		c.LuaDrafts[id] = *draft
	}
	if err := c.checkSize(); err != nil {
		return Catalog{}, err
	}
	return c, nil
}
