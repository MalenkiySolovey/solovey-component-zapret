package strategy

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/MalenkiySolovey/solovey-host/outbounds"
)

const NamedListLimit = 16

type ListKind string

const (
	HostListKind ListKind = "hostList"
	IPSetKind    ListKind = "ipSet"
)

// NamedList contains native match data, not file paths or Routing rules.
type NamedList struct {
	Kind    ListKind `json:"kind"`
	Entries []string `json:"entries"`
}

type NativeListRefs struct {
	HostList        string `json:"hostList,omitempty"`
	HostListExclude string `json:"hostListExclude,omitempty"`
	IPSet           string `json:"ipSet,omitempty"`
	IPSetExclude    string `json:"ipSetExclude,omitempty"`
}

// ListRefs maps native profile IDs to their optional shared list references.
type ListRefs map[string]NativeListRefs

func (r NativeListRefs) ids() [4]string {
	return [...]string{r.HostList, r.HostListExclude, r.IPSet, r.IPSetExclude}
}

func (l NamedList) normalize() (NamedList, error) {
	var lists NativeLists
	if len(l.Entries) == 0 {
		return NamedList{}, errors.New("the native list must not be empty")
	}
	switch l.Kind {
	case HostListKind:
		lists.HostList = l.Entries
	case IPSetKind:
		lists.IPSet = l.Entries
	default:
		return NamedList{}, errors.New("unsupported native list kind")
	}
	remaining := maxNativeListBytes
	sealed, err := lists.seal(&remaining)
	if err != nil {
		return NamedList{}, err
	}
	if l.Kind == HostListKind {
		l.Entries = sealed.HostList
	} else {
		l.Entries = sealed.IPSet
	}
	return l, nil
}

func (c Catalog) validateLists() error {
	if len(c.Lists) > NamedListLimit {
		return errors.New("too many named native lists")
	}
	for id, list := range c.Lists {
		if n, err := list.normalize(); err != nil || !outbounds.ValidZapretStrategyRef(id) || !slices.Equal(n.Entries, list.Entries) {
			return fmt.Errorf("invalid stored native list %q", id)
		}
	}
	return nil
}

func (c Catalog) withResolvedLists(r Record) (Record, error) {
	if len(r.ListRefs) == 0 {
		r.ListRefs = nil
		return r, nil
	}
	if len(r.ListRefs) > len(r.Definition.Profiles) {
		return Record{}, errors.New("native lists refer to an unknown profile")
	}
	r.ListRefs = maps.Clone(r.ListRefs)
	var matched int
	for i := range r.Definition.Profiles {
		profile := &r.Definition.Profiles[i]
		refs, ok := r.ListRefs[profile.ID]
		if !ok {
			continue
		}
		matched++
		if refs == (NativeListRefs{}) {
			return Record{}, errors.New("empty native list references")
		}
		if profile.Lists == nil {
			profile.Lists = new(NativeLists)
		}
		fields := profile.Lists.fields()
		kinds := [...]ListKind{HostListKind, HostListKind, IPSetKind, IPSetKind}
		for j, id := range refs.ids() {
			if id == "" {
				continue
			}
			list, exists := c.Lists[id]
			if !exists || list.Kind != kinds[j] || len(list.Entries) == 0 {
				return Record{}, fmt.Errorf("native list %q is missing or has the wrong kind", id)
			}
			if len(*fields[j].entries) != 0 {
				return Record{}, fmt.Errorf("profile %q cannot combine inline values and a reference for %s", profile.ID, fields[j].flag)
			}
			*fields[j].entries = list.Entries
		}
	}
	if matched != len(r.ListRefs) {
		return Record{}, errors.New("native lists refer to an unknown profile")
	}
	var err error
	r.Definition, err = Seal(r.Definition)
	return r, err
}

func namedListIDs(refs ListRefs) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, profile := range refs {
		for _, id := range profile.ids() {
			if id != "" {
				ids[id] = struct{}{}
			}
		}
	}
	return ids
}

// WithNamedList returns a new catalog with all affected runtime definitions
// re-sealed. A nil list deletes an unused entry; a referenced list cannot be
// deleted or changed to an incompatible kind. The input is never mutated.
func (c Catalog) WithNamedList(id string, value *NamedList) (Catalog, error) {
	if err := c.Validate(); err != nil {
		return Catalog{}, err
	}
	if !outbounds.ValidZapretStrategyRef(id) {
		return Catalog{}, errors.New("invalid native list identity")
	}
	c.Lists = maps.Clone(c.Lists)
	if value == nil {
		if _, exists := c.Lists[id]; !exists {
			return Catalog{}, errors.New("native list does not exist")
		}
		delete(c.Lists, id)
	} else {
		normalized, err := value.normalize()
		if err != nil {
			return Catalog{}, err
		}
		if c.Lists == nil {
			c.Lists = make(map[string]NamedList)
		}
		c.Lists[id] = normalized
		if len(c.Lists) > NamedListLimit {
			return Catalog{}, errors.New("too many named native lists")
		}
	}
	c.Records = maps.Clone(c.Records)
	for ref, r := range c.Records {
		if _, usesList := namedListIDs(r.ListRefs)[id]; !usesList {
			continue
		}
		next, err := c.SealDraft(Draft{ID: ref, Name: r.Name, Raw: &r.Raw, ListRefs: r.ListRefs})
		if err != nil {
			return Catalog{}, fmt.Errorf("native list %q is used by strategy %q: %w", id, ref, err)
		}
		c.Records[ref] = next
	}
	return c, nil
}

func (c Catalog) Export(id string) (Transfer, error) {
	if err := c.Validate(); err != nil {
		return Transfer{}, err
	}
	r, exists := c.Records[id]
	if !exists {
		return Transfer{}, errors.New("native strategy does not exist")
	}
	t := r.Export()
	t.Lists = make(map[string]NamedList)
	for id := range namedListIDs(r.ListRefs) {
		list := c.Lists[id]
		list.Entries = slices.Clone(list.Entries)
		t.Lists[id] = list
	}
	return t, nil
}

// WithTransfer imports the selected strategy and its asset dependencies. It
// refuses to replace a same-name asset with different content.
func (c Catalog) WithTransfer(id string, t Transfer) (Catalog, error) {
	if err := c.Validate(); err != nil {
		return Catalog{}, err
	}
	if _, exists := c.Records[id]; exists || len(c.Records) == CatalogLimit {
		return Catalog{}, errors.New("native strategy already exists or the catalog is full")
	}
	draft, err := t.Import()
	if err != nil {
		return Catalog{}, err
	}
	c.Lists = maps.Clone(c.Lists)
	if c.Lists == nil && len(t.Lists) > 0 {
		c.Lists = make(map[string]NamedList)
	}
	for name, list := range t.Lists {
		if current, exists := c.Lists[name]; exists && (current.Kind != list.Kind || !slices.Equal(current.Entries, list.Entries)) {
			return Catalog{}, fmt.Errorf("native list %q conflicts with an existing asset", name)
		}
		list.Entries = slices.Clone(list.Entries)
		c.Lists[name] = list
	}
	if len(c.Lists) > NamedListLimit {
		return Catalog{}, errors.New("too many named native lists")
	}
	if len(t.BlobAssets) != 0 {
		c.BlobAssets = maps.Clone(c.BlobAssets)
		if c.BlobAssets == nil {
			c.BlobAssets = make(map[string]string)
		}
		for name, data := range t.BlobAssets {
			if current, exists := c.BlobAssets[name]; exists && current != data {
				return Catalog{}, fmt.Errorf("native blob %q conflicts with an existing asset", name)
			}
			c.BlobAssets[name] = data
		}
		if err := c.validateBlobAssets(); err != nil {
			return Catalog{}, err
		}
	}
	draft.ID = id
	record, err := c.SealDraft(draft)
	if err != nil {
		return Catalog{}, err
	}
	c.Records = maps.Clone(c.Records)
	c.Records[id] = record
	return c, nil
}
