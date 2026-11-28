package strategy

import (
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-host/outbounds"
)

type nativePresetSource struct {
	text          string
	files         map[string][]byte
	origin        *PresetOrigin
	guiLists      map[string]guiPresetList
	dependencyIDs map[string]string
	listWorkspace *int
}

type nativePresetPlan struct {
	catalog  Catalog
	preview  PresetPreview
	files    map[string][]byte
	guiLists map[string]guiPresetList
	used     map[string]bool
	edits    map[int]string
	lists    map[string]NamedList
	blobs    map[string]string
	decl     map[string]presetBlob
	refs     ListRefs
	active   int
	ids      map[string]string
	bound    map[string]bool
}

type presetBlob struct {
	source, data string
	token        sourceToken
	packaged     bool
	used         bool
}

func (c Catalog) prepareNativeSource(id, name string, source nativePresetSource) (Catalog, PresetPreview, error) {
	raw, files := source.text, source.files
	if len(raw) > MaxNativeReviewBytes {
		return Catalog{}, PresetPreview{}, fmt.Errorf("native source has %d bytes; preview limit is %d bytes", len(raw), MaxNativeReviewBytes)
	}
	if !utf8.ValidString(raw) {
		return Catalog{}, PresetPreview{}, fmt.Errorf("native preset must be UTF-8 text")
	}
	p := nativePresetPlan{catalog: c, preview: PresetPreview{Source: raw, Origin: source.origin}, files: files, guiLists: source.guiLists, used: map[string]bool{},
		edits: map[int]string{}, lists: map[string]NamedList{}, blobs: map[string]string{}, decl: map[string]presetBlob{}, refs: ListRefs{},
		ids: source.dependencyIDs, bound: make(map[string]bool, len(source.dependencyIDs))}
	tokens, err := nativeSource(raw)
	if err != nil {
		p.preview.issue(nil, "invalid-source", err.Error())
		return Catalog{}, p.preview, nil
	}
	for _, token := range tokens {
		if value, ok := strings.CutPrefix(token.text, "--blob="); ok {
			p.declareBlob(token, value)
		}
	}
	for i, next := 0, 0; i < len(tokens); i = next {
		next = nativeProfileEnd(tokens, i)
		p.profile(tokens[i:next])
	}
	for _, key := range sortedImportKeys(p.decl) {
		blob := p.decl[key]
		if !blob.used {
			p.preview.issue(&blob.token, "unused-asset", "this declaration is not used by an admitted QUIC action")
		}
	}
	for _, key := range sortedImportKeys(files) {
		if !p.used[key] {
			p.preview.issue(nil, "unused-entry", fmt.Sprintf("package entry %q is not referenced; nothing will be imported", key))
		}
	}
	for _, key := range sortedImportKeys(p.ids) {
		if !p.bound[key] {
			p.preview.issue(nil, "unbound-dependency-id", fmt.Sprintf("dependency ID for %q needs a complete, referenced file in the archive", key))
		}
	}
	if len(p.preview.Issues) != 0 {
		slices.SortStableFunc(p.preview.Issues, func(a, b PresetIssue) int {
			if a.Span == nil || b.Span == nil {
				switch {
				case a.Span == b.Span:
					return 0
				case a.Span == nil:
					return 1
				default:
					return -1
				}
			}
			if diff := a.Span.Line - b.Span.Line; diff != 0 {
				return diff
			}
			return a.Span.Column - b.Span.Column
		})
		return Catalog{}, p.preview, nil
	}
	canonical := p.rewrite(tokens)
	return finishPreset(c, p.preview, Draft{ID: id, Name: firstName(name, "Imported Strategy"), Raw: &canonical, ListRefs: p.refs, listWorkspace: source.listWorkspace}, p.lists, p.blobs)
}

func sortedImportKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func (p *nativePresetPlan) rewrite(tokens []sourceToken) string {
	if len(p.edits) == 0 {
		return p.preview.Source
	}
	var text strings.Builder
	previous := 0
	p.preview.projected = make(map[int]sourceToken, len(tokens))
	for _, token := range tokens {
		text.WriteString(p.preview.Source[previous:token.start])
		value, changed := p.edits[token.start]
		if !changed {
			value = p.preview.Source[token.start:token.end]
		}
		if value != "" {
			p.preview.projected[text.Len()] = token
			text.WriteString(value)
		}
		previous = token.end
	}
	text.WriteString(p.preview.Source[previous:])
	p.preview.Converted = true
	return text.String()
}

func (p *nativePresetPlan) profile(tokens []sourceToken) {
	profile := PresetProfile{Line: tokens[0].Line}
	id := fmt.Sprintf("profile-%d", p.active+1)
	isTemplate, skipped := false, nativeProfileSkipped(tokens)
	for _, token := range tokens {
		key, value, ok := strings.Cut(token.text, "=")
		isTemplate = isTemplate || key == "--template"
		if ok && (key == "--name" || key == "--new" || key == "--template") {
			profile.Name = value
			id = value
		}
		if isProfileOption(key) || fileListKind(key) != "" || key == "--skip" && !ok {
			profile.Options = append(profile.Options, token.text)
		}
	}
	if !isTemplate {
		p.active++
	}
	p.preview.Profiles = append(p.preview.Profiles, profile)
	var refs NativeListRefs
	var payloadSeen bool
	for _, token := range tokens {
		key, value, ok := strings.Cut(token.text, "=")
		if skipped && skippedNativeOption(key, ok) && (fileListKind(key) == "" || !p.hasSkippedList(value)) {
			continue
		}
		if key == "--payload" {
			payloadSeen = true
		}
		if key == "--lua-desync" && !payloadSeen && value != "pass" && value != "circular" && !strings.HasPrefix(value, "circular:fails=") {
			p.preview.issue(&token, "payload-order", "the engine snapshots payload at each action; put the intended payload before the action")
		}
		switch {
		case key == "--blob" && ok:
			// Checked separately so a declaration can be resolved before use.
		case fileListKind(key) != "":
			p.mapList(token, key, value, &refs)
			if isTemplate && *refs.slot(key) != "" {
				// Keep the dependency in its source scope. It must follow future
				// edits to --import, not the profile that first consumed it.
				managed := "lists/" + *refs.slot(key) + ".txt"
				if unquotePresetPath(value) == managed {
					delete(p.edits, token.start)
				} else {
					p.edits[token.start] = key + "=" + managed
				}
			}
		case key == "--lua-desync" && p.mapBlobAction(token, value):
		case isProfileOption(key):
			p.checkProfileOption(token, key, value, ok)
		default:
			code, reason := classifyPresetOption(key)
			p.preview.issue(&token, code, reason)
		}
	}
	if !isTemplate && refs != (NativeListRefs{}) {
		if _, exists := p.refs[id]; exists {
			p.preview.issue(&tokens[0], "duplicate-list-owner", "use distinct profile IDs for managed lists")
		} else {
			p.refs[id] = refs
		}
	}
}

func isProfileOption(key string) bool {
	switch key {
	case "--name", "--new", "--template", "--import", "--filter-l3", "--filter-l7", "--filter-tcp", "--filter-udp", "--payload", "--in-range", "--out-range",
		"--lua-desync", "--hostlist-domains", "--hostlist-exclude-domains", "--ipset-ip", "--ipset-exclude-ip":
		return true
	}
	return false
}

func (p *nativePresetPlan) checkProfileOption(token sourceToken, key, value string, hasValue bool) {
	var reason string
	switch key {
	case "--name", "--new", "--template", "--import":
		if (hasValue || key == "--name" || key == "--import") && !outbounds.ValidZapretStrategyRef(value) {
			reason = "profile IDs must be 1 to 64 lowercase letters, digits or hyphens; edit this ID explicitly before importing"
		}
	case "--filter-tcp", "--filter-udp":
		if _, err := parseNativePorts(value, intent.PacketProtocol(strings.TrimPrefix(key, "--filter-"))); err != nil {
			reason = err.Error()
		}
	case "--filter-l7":
		if !validNativeSelector(value, nativeL7Names) {
			reason = "use comma-separated L7 protocol names from the pinned engine"
		}
	case "--payload":
		if !validNativeSelector(value, nativePayloadNames) {
			reason = "use comma-separated native payload names from the pinned engine"
		}
	case "--lua-desync":
		action, _, _ := strings.Cut(value, ":")
		switch action {
		case "multisplit", "circular", "http_domcase", "http_hostcase", "fake", "pass":
			// The owner parser checks action parameters and prior filters.
		default:
			reason = "this action is outside the admitted typed subset; engine or Windows preset availability is not Solovey support"
		}
	}
	if reason != "" {
		p.preview.issue(&token, "unsupported-profile-option", reason)
	}
}

func classifyPresetOption(key string) (string, string) {
	switch {
	case strings.HasPrefix(key, "--wf-"), key == "--writable":
		return "platform-option", "Windows interception or file-write option; Solovey uses its scoped Linux network owner"
	case key == "--lua-init":
		return "trust-restricted", "Lua loading is runtime-owned; imported programs and script paths are never executed"
	case key == "--cookie":
		return "unrepresented-profile", "this native profile/template control cannot be represented without changing its semantics"
	case strings.HasPrefix(key, "--ctrack-"), strings.HasPrefix(key, "--ipcache-"), key == "--qnum", key == "--debug":
		return "runtime-option", "global engine and queue configuration is runtime-owned, not part of a Strategy"
	case key == "--filter-icmp", key == "--filter-ipp":
		return "outside-scope", "an installed Strategy requires a positive TCP or UDP port scope"
	default:
		return "unknown-option", "not in the admitted profile grammar; kept in the original source for review"
	}
}

func fileListKind(key string) ListKind {
	switch key {
	case "--hostlist", "--hostlist-exclude":
		return HostListKind
	case "--ipset", "--ipset-exclude":
		return IPSetKind
	}
	return ""
}

// Keep data supplied for an inactive profile in managed storage. A missing
// inactive dependency can remain in its original source until reactivation.
func (p *nativePresetPlan) hasSkippedList(value string) bool {
	value = unquotePresetPath(value)
	if p.guiLists != nil {
		name, ok := strings.CutPrefix(strings.ReplaceAll(value, "\\", "/"), "lists/")
		_, present := p.guiLists[strings.ToLower(name)]
		return ok && present
	}
	if _, present := p.files[value]; present {
		return true
	}
	_, present := p.catalog.Lists[presetAssetID(value, "lists/", ".txt")]
	return present
}

func (p *nativePresetPlan) mapList(token sourceToken, key, value string, refs *NativeListRefs) {
	slot := refs.slot(key)
	if *slot != "" {
		p.preview.issue(&token, "multiple-list-files", "one managed list per filter is admitted; combine source entries and review the result explicitly")
		return
	}
	value = unquotePresetPath(value)
	kind := fileListKind(key)
	if p.guiLists != nil {
		p.mapGUIList(token, value, kind, slot)
		return
	}
	id := presetAssetID(value, "lists/", ".txt")
	if id == "" {
		p.preview.issue(&token, "unsafe-list-path", "only lists/<managed-id>.txt can map to a managed list; no host filesystem paths or URLs are read")
		return
	}
	dep, err := p.prepareList(value, id, kind)
	p.preview.Dependencies = append(p.preview.Dependencies, dep)
	if err != nil {
		p.preview.issue(&token, "list-dependency", err.Error())
		return
	}
	*slot = dep.ID
	p.edits[token.start] = ""
}

func (p *nativePresetPlan) dependencyID(path, fallback string) string {
	if id, ok := p.ids[path]; ok {
		p.bound[path] = true
		return id
	}
	return fallback
}

func (p *nativePresetPlan) prepareList(path, id string, kind ListKind) (PresetDependency, error) {
	dep := PresetDependency{Source: path, ID: id, Kind: string(kind), State: "missing"}
	data, present := p.files[path]
	if present {
		p.used[path] = true
		list := NamedList{Kind: kind}
		for line := range strings.SplitSeq(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
				list.Entries = append(list.Entries, line)
			}
		}
		normal, err := list.normalize()
		if err != nil {
			dep.State = "conflict"
			return dep, fmt.Errorf("list %q: %w", dep.Source, err)
		}
		dep.ID, dep.CanRename = p.dependencyID(path, id), true
		current, exists := p.catalog.Lists[dep.ID]
		previous, staged := p.lists[dep.ID]
		if (staged && (normal.Kind != previous.Kind || !slices.Equal(normal.Entries, previous.Entries))) ||
			(exists && (normal.Kind != current.Kind || !slices.Equal(normal.Entries, current.Entries))) {
			dep.State = "conflict"
			return dep, fmt.Errorf("managed list %q already has different content or a different kind", dep.ID)
		}
		p.lists[dep.ID] = normal
		dep.State = "new"
		if exists {
			dep.State = "reuse"
		}
		return dep, nil
	}
	if current, exists := p.catalog.Lists[id]; exists {
		if current.Kind != kind {
			dep.State = "conflict"
			return dep, fmt.Errorf("managed list %q has a different kind", id)
		}
		dep.State = "reuse"
		return dep, nil
	}
	return dep, fmt.Errorf("create a matching managed list %q or include %q in a data-only package", id, path)
}

func unquotePresetPath(input string) string {
	if len(input) > 1 && input[0] == '"' && input[len(input)-1] == '"' {
		return input[1 : len(input)-1]
	}
	return input
}

func (p *nativePresetPlan) declareBlob(token sourceToken, value string) {
	id, source, ok := strings.Cut(value, ":")
	if !ok || !outbounds.ValidZapretStrategyRef(id) {
		p.preview.issue(&token, "blob-declaration", "use a lowercase managed blob ID and inline hexadecimal data or a blobs/<id>.hex package entry")
		return
	}
	if _, duplicate := p.decl[id]; duplicate {
		p.preview.issue(&token, "blob-declaration", "duplicate blob declaration would change dependency resolution")
		return
	}
	var data string
	var packaged bool
	if inline, ok := strings.CutPrefix(source, "0x"); ok {
		data, source = inline, id
	} else if strings.HasPrefix(source, "@") {
		source = unquotePresetPath(source[1:])
		binary := presetAssetID(source, "blobs/", ".bin") == id
		if !binary && presetAssetID(source, "blobs/", ".hex") != id {
			p.preview.issue(&token, "blob-declaration", "use inline hexadecimal data or a matching blobs/<id>.hex or blobs/<id>.bin package entry")
			return
		}
		file, found := p.files[source]
		if !found {
			p.preview.Dependencies = append(p.preview.Dependencies, PresetDependency{Source: source, ID: id, Kind: "blob", State: "missing"})
			p.preview.issue(&token, "blob-dependency", "include the blob file in the data-only package, or explicitly select a managed blob")
			return
		}
		p.used[source] = true
		packaged = true
		if binary {
			data = hex.EncodeToString(file)
		} else {
			data = strings.TrimSpace(string(file))
		}
	} else {
		p.preview.issue(&token, "blob-declaration", "external blob files and URLs are not read")
		return
	}
	data, err := normalizeBlob(data)
	if err != nil {
		p.preview.issue(&token, "blob-declaration", err.Error())
		return
	}
	p.decl[id] = presetBlob{source: source, data: data, token: token, packaged: packaged}
	p.edits[token.start] = ""
}

func (p *nativePresetPlan) mapBlobAction(token sourceToken, value string) bool {
	fake, ok := strings.CutPrefix(value, "fake:blob=")
	if !ok {
		return false
	}
	id, repeats, ok := strings.Cut(fake, ":repeats=")
	if !ok || id == defaultQUICBlobName {
		return false
	}
	if managed, ok := strings.CutPrefix(id, nativeBlobRefPrefix); ok {
		state := "reuse"
		if _, exists := p.catalog.BlobAssets[managed]; !exists {
			state = "missing"
			p.preview.issue(&token, "blob-dependency", fmt.Sprintf("managed blob %q does not exist", managed))
		}
		p.preview.Dependencies = append(p.preview.Dependencies, PresetDependency{Source: managed, ID: managed, Kind: "blob", State: state})
		return true
	}
	blob, exists := p.decl[id]
	if !exists {
		p.preview.issue(&token, "blob-dependency", "choose a built-in or managed QUIC asset or provide a qualified blob declaration")
		return true
	}
	blob.used = true
	p.decl[id] = blob
	if blob.packaged {
		id = p.dependencyID(blob.source, id)
	}
	state := p.catalog.blobImportState(id, blob.data)
	if previous, staged := p.blobs[id]; staged && previous != blob.data {
		state = "conflict"
	}
	p.preview.Dependencies = append(p.preview.Dependencies, PresetDependency{Source: blob.source, ID: id, Kind: "blob", State: state, CanRename: blob.packaged})
	if state == "conflict" {
		p.preview.issue(&token, "blob-dependency", fmt.Sprintf("managed blob %q already has different content", id))
		return true
	}
	p.blobs[id] = blob.data
	p.edits[token.start] = "--lua-desync=fake:blob=" + nativeBlobRefPrefix + id + ":repeats=" + repeats
	return true
}
