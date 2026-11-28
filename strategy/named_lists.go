package strategy

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/MalenkiySolovey/solovey-host/outbounds"
)

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

// ListRefs maps profile IDs to editor-selected lists. The template: namespace
// indexes managed file directives kept in raw source, even when not active.
type ListRefs map[string]NativeListRefs

const templateListPrefix = "template:"

func (r NativeListRefs) ids() [4]string {
	return [...]string{r.HostList, r.HostListExclude, r.IPSet, r.IPSetExclude}
}

func (r *NativeListRefs) slot(option string) *string {
	switch option {
	case "--hostlist":
		return &r.HostList
	case "--hostlist-exclude":
		return &r.HostListExclude
	case "--ipset":
		return &r.IPSet
	case "--ipset-exclude":
		return &r.IPSetExclude
	}
	return nil
}

func (r *NativeListRefs) merge(other NativeListRefs) error {
	slots := [...]*string{&r.HostList, &r.HostListExclude, &r.IPSet, &r.IPSetExclude}
	for i, id := range other.ids() {
		if id == "" {
			continue
		}
		if *slots[i] != "" && *slots[i] != id {
			return fmt.Errorf("managed lists %q and %q occupy the same filter slot; combine them into one managed list and select it explicitly", *slots[i], id)
		}
		*slots[i] = id
	}
	return nil
}

type nativeListProjection struct {
	templates ListRefs
	active    ListRefs
}

func (p *nativeListProjection) addTemplate(id string, refs NativeListRefs) {
	if p.templates == nil {
		p.templates = make(ListRefs)
	}
	p.templates[templateListPrefix+id] = refs
}

func (p *nativeListProjection) addActive(id string, refs NativeListRefs) {
	if p.active == nil {
		p.active = make(ListRefs)
	}
	p.active[id] = refs
}

func (p nativeListProjection) resolve(saved ListRefs) (authored, effective ListRefs, err error) {
	authored, effective = maps.Clone(p.templates), maps.Clone(p.active)
	for profile, refs := range saved {
		if strings.HasPrefix(profile, templateListPrefix) {
			continue // Derived from raw source, never from a stale client projection.
		}
		if refs == (NativeListRefs{}) {
			return nil, nil, errors.New("empty native list references")
		}
		if authored == nil {
			authored = make(ListRefs)
		}
		authored[profile] = refs
		if effective == nil {
			effective = make(ListRefs)
		}
		merged := effective[profile]
		if err := merged.merge(refs); err != nil {
			return nil, nil, fmt.Errorf("profile %q: %w", profile, err)
		}
		effective[profile] = merged
	}
	return authored, effective, nil
}

func (r Record) effectiveListRefs() (ListRefs, error) {
	for key := range r.ListRefs {
		if strings.HasPrefix(key, templateListPrefix) {
			var projection nativeListProjection
			if _, err := parseNativeWithLua(r.Definition.ID, r.Raw, r.Definition.ManagedLua, &projection); err != nil {
				return nil, err
			}
			authored, effective, err := projection.resolve(r.ListRefs)
			if err == nil && !maps.Equal(authored, r.ListRefs) {
				return nil, errors.New("template lists do not match native source")
			}
			return effective, err
		}
	}
	return r.ListRefs, nil
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
	sealed, err := lists.seal()
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
	for id, list := range c.Lists {
		if n, err := list.normalize(); err != nil || !outbounds.ValidZapretStrategyRef(id) || !slices.Equal(n.Entries, list.Entries) {
			return fmt.Errorf("invalid stored native list %q", id)
		}
	}
	return nil
}

var nativeListKinds = [...]ListKind{HostListKind, HostListKind, IPSetKind, IPSetKind}

func (c Catalog) checkTemplateLists(templates ListRefs) error {
	for template, refs := range templates {
		for i, id := range refs.ids() {
			if id == "" {
				continue
			}
			if list, ok := c.Lists[id]; !ok || list.Kind != nativeListKinds[i] || len(list.Entries) == 0 {
				return fmt.Errorf("template %q: native list %q is missing or has the wrong kind", strings.TrimPrefix(template, templateListPrefix), id)
			}
		}
	}
	return nil
}

func (c Catalog) withResolvedLists(r Record, effective ListRefs) (Record, error) {
	if len(effective) == 0 {
		return r, nil
	}
	if len(effective) > len(r.Definition.Profiles) {
		return Record{}, errors.New("native lists refer to an unknown profile")
	}
	var matched int
	for i := range r.Definition.Profiles {
		profile := &r.Definition.Profiles[i]
		refs, ok := effective[profile.ID]
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
		for j, id := range refs.ids() {
			if id == "" {
				continue
			}
			list, exists := c.Lists[id]
			if !exists || list.Kind != nativeListKinds[j] || len(list.Entries) == 0 {
				return Record{}, fmt.Errorf("native list %q is missing or has the wrong kind", id)
			}
			if len(*fields[j].entries) != 0 {
				return Record{}, fmt.Errorf("profile %q cannot combine inline values and a reference for %s", profile.ID, fields[j].flag)
			}
			*fields[j].entries = list.Entries
		}
	}
	if matched != len(effective) {
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
	}
	c.Records = maps.Clone(c.Records)
	for ref, r := range c.Records {
		if _, usesList := namedListIDs(r.ListRefs)[id]; !usesList {
			continue
		}
		next, err := c.SealDraft(Draft{ID: ref, Name: r.Name, Raw: &r.Raw, ListRefs: r.ListRefs, ManagedLua: r.Definition.ManagedLua})
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
	if refs := r.Definition.ManagedLua; len(refs) != 0 {
		modules, err := c.resolveLua(refs, false)
		if err != nil {
			return Transfer{}, err
		}
		t.LuaSources = make(map[string]LuaDraft, len(refs))
		t.Strategy.Definition.ManagedLua = slices.Clone(refs)
		for i, ref := range refs {
			t.LuaSources[ref.key()] = modules[i].Draft
			t.Strategy.Definition.ManagedLua[i].ApproveImported = false
		}
		t.Strategy.Definition, err = Seal(t.Strategy.Definition)
		if err != nil {
			return Transfer{}, err
		}
	}
	return t, nil
}

// WithTransfer imports the selected strategy and its asset dependencies. It
// refuses to replace a same-name asset with different content.
func (c Catalog) WithTransfer(id string, t Transfer) (Catalog, error) {
	if err := c.Validate(); err != nil {
		return Catalog{}, err
	}
	draft, err := t.Import()
	if err != nil {
		return Catalog{}, err
	}
	draft.ID = id
	return c.withImportedDraft(draft, t.Lists, t.BlobAssets)
}

func (c Catalog) withImportedDraft(draft Draft, lists map[string]NamedList, blobs map[string]string) (Catalog, error) {
	if _, exists := c.Records[draft.ID]; exists {
		return Catalog{}, errors.New("native strategy already exists")
	}
	c.Lists = maps.Clone(c.Lists)
	if c.Lists == nil && len(lists) > 0 {
		c.Lists = make(map[string]NamedList)
	}
	for name, list := range lists {
		if current, exists := c.Lists[name]; exists && (current.Kind != list.Kind || !slices.Equal(current.Entries, list.Entries)) {
			return Catalog{}, fmt.Errorf("native list %q conflicts with an existing asset", name)
		}
		list.Entries = slices.Clone(list.Entries)
		c.Lists[name] = list
	}
	if len(blobs) != 0 {
		c.BlobAssets = maps.Clone(c.BlobAssets)
		if c.BlobAssets == nil {
			c.BlobAssets = make(map[string]string)
		}
		for name, data := range blobs {
			if current, exists := c.BlobAssets[name]; exists && current != data {
				return Catalog{}, fmt.Errorf("native blob %q conflicts with an existing asset", name)
			}
			c.BlobAssets[name] = data
		}
		if err := c.validateBlobAssets(); err != nil {
			return Catalog{}, err
		}
	}
	if len(draft.luaSources) != 0 {
		c.LuaDrafts = maps.Clone(c.LuaDrafts)
		c.LuaRevisions = maps.Clone(c.LuaRevisions)
		if c.LuaDrafts == nil {
			c.LuaDrafts = make(map[string]LuaDraft)
		}
		if c.LuaRevisions == nil {
			c.LuaRevisions = make(map[string]LuaDraft)
		}
		for _, ref := range draft.ManagedLua {
			source := draft.luaSources[ref.key()]
			if current, exists := c.LuaDrafts[ref.ID]; !exists {
				c.LuaDrafts[ref.ID] = source
			} else if current.Revision() != ref.Revision {
				c.LuaRevisions[ref.key()] = source
			}
		}
	}
	record, err := c.SealDraft(draft)
	if err != nil {
		return Catalog{}, err
	}
	c.Records = maps.Clone(c.Records)
	c.Records[draft.ID] = record
	return c, nil
}
