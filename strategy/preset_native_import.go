package strategy

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-host/outbounds"
)

type nativePresetPlan struct {
	catalog Catalog
	preview PresetPreview
	files   map[string][]byte
	used    map[string]bool
	edits   map[int]string
	lists   map[string]NamedList
	blobs   map[string]string
	decl    map[string]presetBlob
	refs    ListRefs
}

type presetBlob struct {
	source, data string
	token        sourceToken
}

func (c Catalog) prepareNativePreset(id, name, raw string, files map[string][]byte) (Catalog, PresetPreview, error) {
	if len(raw) > MaxNativeReviewBytes {
		return Catalog{}, PresetPreview{}, fmt.Errorf("native source has %d bytes; preview limit is %d bytes", len(raw), MaxNativeReviewBytes)
	}
	if !utf8.ValidString(raw) {
		return Catalog{}, PresetPreview{}, fmt.Errorf("native preset must be UTF-8 text")
	}
	p := nativePresetPlan{catalog: c, preview: PresetPreview{Source: raw}, files: files, used: map[string]bool{},
		edits: map[int]string{}, lists: map[string]NamedList{}, blobs: map[string]string{}, decl: map[string]presetBlob{}, refs: ListRefs{}}
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
		next = i + 1
		for next < len(tokens) && tokens[next].text != "--new" && !strings.HasPrefix(tokens[next].text, "--new=") {
			next++
		}
		p.profile(tokens[i:next])
	}
	for _, key := range sortedImportKeys(p.decl) {
		blob := p.decl[key]
		if _, used := p.blobs[key]; !used {
			p.preview.issue(&blob.token, "unused-asset", "this declaration is not used by an admitted QUIC action")
		}
	}
	for _, key := range sortedImportKeys(files) {
		if !p.used[key] {
			p.preview.issue(nil, "unused-entry", fmt.Sprintf("package entry %q is not referenced; nothing will be imported", key))
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
	return finishPreset(c, p.preview, Draft{ID: id, Name: firstName(name, "Imported Strategy"), Raw: &canonical, ListRefs: p.refs}, p.lists, p.blobs)
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
			value = token.text
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
	index := len(p.preview.Profiles) + 1
	profile := PresetProfile{Line: tokens[0].Line}
	id := fmt.Sprintf("profile-%d", index)
	for _, token := range tokens {
		key, value, ok := strings.Cut(token.text, "=")
		if ok && (key == "--name" || key == "--new") {
			profile.Name = value
			id = value
		}
		if isProfileOption(key) || fileListKind(key) != "" {
			profile.Options = append(profile.Options, token.text)
		}
	}
	p.preview.Profiles = append(p.preview.Profiles, profile)
	var refs NativeListRefs
	var payloadSeen bool
	for _, token := range tokens {
		key, value, ok := strings.Cut(token.text, "=")
		if key == "--payload" {
			payloadSeen = true
		}
		if key == "--lua-desync" && !payloadSeen && value != "circular" && !strings.HasPrefix(value, "circular:fails=") {
			p.preview.issue(&token, "payload-order", "the engine snapshots payload at each action; put the intended payload before the action")
		}
		switch {
		case key == "--blob" && ok:
			// Checked separately so a declaration can be resolved before use.
		case fileListKind(key) != "":
			p.mapList(token, key, value, &refs)
		case key == "--lua-desync" && p.mapBlobAction(token, value):
		case isProfileOption(key):
			p.checkProfileOption(token, key, value, ok)
		default:
			code, reason := classifyPresetOption(key)
			p.preview.issue(&token, code, reason)
		}
	}
	if refs != (NativeListRefs{}) {
		p.refs[id] = refs
	}
}

func isProfileOption(key string) bool {
	switch key {
	case "--name", "--new", "--filter-l3", "--filter-l7", "--filter-tcp", "--filter-udp", "--payload", "--in-range", "--out-range",
		"--lua-desync", "--hostlist-domains", "--hostlist-exclude-domains", "--ipset-ip", "--ipset-exclude-ip":
		return true
	}
	return false
}

func (p *nativePresetPlan) checkProfileOption(token sourceToken, key, value string, hasValue bool) {
	var reason string
	switch key {
	case "--name", "--new":
		if (hasValue || key == "--name") && !outbounds.ValidZapretStrategyRef(value) {
			reason = "profile IDs must be 1 to 64 lowercase letters, digits or hyphens; edit this ID explicitly before importing"
		}
	case "--filter-tcp", "--filter-udp":
		if _, err := parseNativePorts(value, intent.PacketProtocol(strings.TrimPrefix(key, "--filter-"))); err != nil {
			reason = err.Error()
		}
	case "--filter-l7":
		if value != "tls" && value != "http" && value != "quic" {
			reason = "only a single TLS, HTTP or QUIC protocol filter is admitted"
		}
	case "--payload":
		if payloadProtocol(Payload(value)) == "" {
			reason = "only a single TLS ClientHello, HTTP request or QUIC Initial payload is admitted"
		}
	case "--lua-desync":
		action, _, _ := strings.Cut(value, ":")
		switch action {
		case "multisplit", "circular", "http_domcase", "http_hostcase", "fake":
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
	case key == "--template", key == "--import", key == "--skip", key == "--cookie":
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

func (p *nativePresetPlan) mapList(token sourceToken, key, value string, refs *NativeListRefs) {
	var slot *string
	switch key {
	case "--hostlist":
		slot = &refs.HostList
	case "--hostlist-exclude":
		slot = &refs.HostListExclude
	case "--ipset":
		slot = &refs.IPSet
	case "--ipset-exclude":
		slot = &refs.IPSetExclude
	}
	if *slot != "" {
		p.preview.issue(&token, "multiple-list-files", "one managed list per filter is admitted; combine source entries and review the result explicitly")
		return
	}
	value = unquotePresetPath(value)
	id := presetAssetID(value, "lists/", ".txt")
	if id == "" {
		p.preview.issue(&token, "unsafe-list-path", "only lists/<managed-id>.txt can map to a managed list; no host filesystem paths or URLs are read")
		return
	}
	kind := fileListKind(key)
	state, err := p.prepareList(value, id, kind)
	p.preview.Dependencies = append(p.preview.Dependencies, PresetDependency{value, id, string(kind), state})
	if err != nil {
		p.preview.issue(&token, "list-dependency", err.Error())
		return
	}
	*slot = id
	p.edits[token.start] = ""
}

func (p *nativePresetPlan) prepareList(path, id string, kind ListKind) (string, error) {
	current, exists := p.catalog.Lists[id]
	if exists && current.Kind != kind {
		return "conflict", fmt.Errorf("managed list %q has a different kind", id)
	}
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
			return "conflict", fmt.Errorf("list %q: %w", id, err)
		}
		previous, staged := p.lists[id]
		if (staged && normal.Kind != previous.Kind) || (exists && !slices.Equal(normal.Entries, current.Entries)) {
			return "conflict", fmt.Errorf("managed list %q already has different content or a different kind", id)
		}
		p.lists[id] = normal
	}
	if exists {
		return "reuse", nil
	}
	if present {
		return "new", nil
	}
	return "missing", fmt.Errorf("create a matching managed list %q or include %q in a data-only package", id, path)
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
	if hex, ok := strings.CutPrefix(source, "0x"); ok {
		data, source = hex, id
	} else if path, ok := strings.CutPrefix(source, "@"); ok && presetAssetID(unquotePresetPath(path), "blobs/", ".hex") == id {
		source = unquotePresetPath(path)
		file, found := p.files[source]
		if !found {
			p.preview.Dependencies = append(p.preview.Dependencies, PresetDependency{source, id, "blob", "missing"})
			p.preview.issue(&token, "blob-dependency", "include the hexadecimal blob file in the data-only package, or explicitly select a managed blob")
			return
		}
		p.used[source] = true
		data = strings.TrimSpace(string(file))
	} else {
		p.preview.issue(&token, "blob-declaration", "external blob files and URLs are not read")
		return
	}
	data, err := normalizeBlob(data)
	if err != nil {
		p.preview.issue(&token, "blob-declaration", err.Error())
		return
	}
	p.decl[id] = presetBlob{source, data, token}
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
		p.preview.Dependencies = append(p.preview.Dependencies, PresetDependency{managed, managed, "blob", state})
		return true
	}
	blob, exists := p.decl[id]
	if !exists {
		p.preview.issue(&token, "blob-dependency", "choose a built-in or managed QUIC asset or provide a qualified blob declaration")
		return true
	}
	state := p.catalog.blobImportState(id, blob.data)
	p.preview.Dependencies = append(p.preview.Dependencies, PresetDependency{blob.source, id, "blob", state})
	if state == "conflict" {
		p.preview.issue(&token, "blob-dependency", fmt.Sprintf("managed blob %q already has different content", id))
	}
	p.blobs[id] = blob.data
	p.edits[token.start] = "--lua-desync=fake:blob=" + nativeBlobRefPrefix + id + ":repeats=" + repeats
	return true
}
