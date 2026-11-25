package strategy

import (
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/MalenkiySolovey/solovey-host/outbounds"
)

const (
	NamedBlobLimit      = 8        // Distinct assets in one installed Strategy, not stored assets.
	MaxNativeBlobBytes  = 1200     // Total distinct custom blob bytes per Strategy.
	nativeBlobRefPrefix = "asset_" // Editor reference, never a runtime Lua variable.
)

func normalizeBlob(data string) (string, error) {
	if len(data) == 0 || len(data) > MaxNativeBlobBytes*2 || len(data)%2 != 0 {
		return "", errors.New("an asset must contain 1 to 1200 bytes as hexadecimal pairs")
	}
	if _, err := hex.DecodeString(data); err != nil {
		return "", errors.New("an asset accepts hexadecimal pairs only")
	}
	return strings.ToLower(data), nil
}

func (s Strategy) nativeBlobIDs() map[string]struct{} {
	ids := make(map[string]struct{})
	for _, profile := range s.Profiles {
		if profile.QUICBlobRef != "" {
			ids[profile.QUICBlobRef] = struct{}{}
		}
	}
	return ids
}

func (s Strategy) validateBlobs() error {
	ids := s.nativeBlobIDs()
	if len(ids) > NamedBlobLimit {
		return fmt.Errorf("%w: strategy references %d native blobs; installation limit is %d", ErrInvalidStrategy, len(ids), NamedBlobLimit)
	}
	if len(s.BlobData) != len(ids) {
		return fmt.Errorf("%w: missing or unused native blob assets", ErrInvalidStrategy)
	}
	remaining := MaxNativeBlobBytes * 2
	for id := range ids {
		data := s.BlobData[id]
		remaining -= len(data)
		if canonical, err := normalizeBlob(data); err != nil || canonical != data {
			return fmt.Errorf("%w: invalid or missing native blob %q", ErrInvalidStrategy, id)
		}
	}
	if remaining < 0 {
		return fmt.Errorf("%w: native blobs require %d bytes; installation limit is %d bytes per Strategy", ErrInvalidStrategy, MaxNativeBlobBytes-remaining/2, MaxNativeBlobBytes)
	}
	return nil
}

func (c Catalog) validateBlobAssets() error {
	for id, data := range c.BlobAssets {
		if canonical, err := normalizeBlob(data); err != nil || canonical != data || !outbounds.ValidZapretStrategyRef(id) {
			return fmt.Errorf("invalid stored native blob %q", id)
		}
	}
	return nil
}

func (c Catalog) resolveBlobs(s Strategy) (Strategy, error) {
	for id := range s.nativeBlobIDs() {
		data, ok := c.BlobAssets[id]
		if !ok {
			return Strategy{}, fmt.Errorf("native blob %q does not exist", id)
		}
		if s.BlobData == nil {
			s.BlobData = make(map[string]string)
		}
		s.BlobData[id] = data
	}
	return s, nil
}

// WithNamedBlob changes one catalog-owned binary asset and re-seals its
// consumers. Deletion has no effect unless all references were removed first.
func (c Catalog) WithNamedBlob(id string, data *string) (Catalog, error) {
	if err := c.Validate(); err != nil {
		return Catalog{}, err
	}
	if !outbounds.ValidZapretStrategyRef(id) {
		return Catalog{}, errors.New("invalid native blob identity")
	}
	c.BlobAssets = maps.Clone(c.BlobAssets)
	if data == nil {
		if _, exists := c.BlobAssets[id]; !exists {
			return Catalog{}, errors.New("native blob does not exist")
		}
		delete(c.BlobAssets, id)
	} else {
		canonical, err := normalizeBlob(*data)
		if err != nil {
			return Catalog{}, err
		}
		if c.BlobAssets == nil {
			c.BlobAssets = make(map[string]string)
		}
		c.BlobAssets[id] = canonical
	}
	c.Records = maps.Clone(c.Records)
	for ref, record := range c.Records {
		if _, used := record.Definition.BlobData[id]; !used {
			continue
		}
		next, err := c.SealDraft(Draft{ID: ref, Name: record.Name, Raw: &record.Raw, ListRefs: record.ListRefs})
		if err != nil {
			return Catalog{}, fmt.Errorf("native blob %q is used by strategy %q: %w", id, ref, err)
		}
		c.Records[ref] = next
	}
	if err := c.checkSize(); err != nil {
		return Catalog{}, err
	}
	return c, nil
}
