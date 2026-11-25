package strategy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"
)

type PresetInput struct {
	Format     string `json:"format"`
	Name       string `json:"name,omitempty"`
	TemplateID string `json:"templateId,omitempty"`
	Content    []byte `json:"content,omitempty"`
}

type PresetIssue struct {
	Span    *SourceSpan `json:"span,omitempty"`
	Option  string      `json:"option,omitempty"`
	Code    string      `json:"code"`
	Message string      `json:"message"`
}

type PresetDependency struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	State  string `json:"state"` // new, reuse, conflict, or missing
}

type PresetProfile struct {
	Line    int      `json:"line"`
	Name    string   `json:"name,omitempty"`
	Options []string `json:"options,omitempty"`
}

type PresetPreview struct {
	Source       string             `json:"source,omitempty"`
	Profiles     []PresetProfile    `json:"profiles,omitempty"`
	Issues       []PresetIssue      `json:"issues,omitempty"`
	Dependencies []PresetDependency `json:"dependencies,omitempty"`
	Record       *Record            `json:"record,omitempty"`
	Converted    bool               `json:"converted,omitempty"`
	projected    map[int]sourceToken
}

func (p *PresetPreview) issue(token *sourceToken, code, reason string) {
	issue := PresetIssue{Code: code, Message: reason}
	if token != nil {
		issue.Span = &token.SourceSpan
		issue.Option, _, _ = strings.Cut(token.text, "=")
	}
	p.Issues = append(p.Issues, issue)
}

// PreparePreset is used for both preview and commit. A rejected candidate
// retains its inert source and never returns an updated Catalog.
func (c Catalog) PreparePreset(id string, input PresetInput) (Catalog, PresetPreview, error) {
	if err := c.Validate(); err != nil {
		return Catalog{}, PresetPreview{}, err
	}
	if len(input.Content) > MaxPresetBytes {
		return Catalog{}, PresetPreview{}, fmt.Errorf("preset has %d bytes; import limit is %d bytes", len(input.Content), MaxPresetBytes)
	}
	if input.Format == "template" && input.TemplateID != "" && len(input.Content) == 0 {
		template, ok := findTemplate(input.TemplateID)
		if !ok {
			return Catalog{}, PresetPreview{}, errors.New("preset template does not exist")
		}
		return c.prepareNativePreset(id, firstName(input.Name, template.Name), template.source, nil)
	}
	if input.TemplateID != "" || len(input.Content) == 0 {
		return Catalog{}, PresetPreview{}, errors.New("select one preset source")
	}
	switch input.Format {
	case "native":
		return c.prepareNativePreset(id, input.Name, string(input.Content), nil)
	case "json":
		return c.prepareTransferPreset(id, input.Name, input.Content)
	case "zip":
		files, err := readPresetPackage(input.Content)
		if err != nil {
			return Catalog{}, PresetPreview{}, err
		}
		if encoded, ok := files["strategy.json"]; ok && len(files) == 1 {
			return c.prepareTransferPreset(id, input.Name, encoded)
		}
		if source, ok := files["strategy.txt"]; ok && files["strategy.json"] == nil {
			delete(files, "strategy.txt")
			return c.prepareNativePreset(id, input.Name, string(source), files)
		}
		return Catalog{}, PresetPreview{}, errors.New("ZIP must contain either strategy.json alone or strategy.txt with its data dependencies")
	default:
		return Catalog{}, PresetPreview{}, errors.New("unknown preset format")
	}
}

func firstName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

func (c Catalog) prepareTransferPreset(id, name string, data []byte) (Catalog, PresetPreview, error) {
	transfer, err := decodePresetTransfer(data)
	if err != nil {
		return Catalog{}, PresetPreview{}, err
	}
	p := PresetPreview{Source: transfer.Strategy.Raw}
	for key, list := range transfer.Lists {
		state := "new"
		if current, ok := c.Lists[key]; ok {
			state = "reuse"
			if list.Kind != current.Kind || !slices.Equal(list.Entries, current.Entries) {
				state = "conflict"
			}
		}
		p.Dependencies = append(p.Dependencies, PresetDependency{key, key, string(list.Kind), state})
	}
	for key, data := range transfer.BlobAssets {
		p.Dependencies = append(p.Dependencies, PresetDependency{key, key, "blob", c.blobImportState(key, data)})
	}
	slices.SortFunc(p.Dependencies, func(a, b PresetDependency) int {
		if result := strings.Compare(a.Kind, b.Kind); result != 0 {
			return result
		}
		return strings.Compare(a.ID, b.ID)
	})
	draft, err := transfer.Import()
	if err == nil {
		draft.ID, draft.Name = id, firstName(name, draft.Name)
		return finishPreset(c, p, draft, transfer.Lists, transfer.BlobAssets)
	}
	p.issue(nil, "invalid-transfer", err.Error())
	return Catalog{}, p, nil
}

func (c Catalog) blobImportState(id, data string) string {
	if current, ok := c.BlobAssets[id]; ok {
		if current != data {
			return "conflict"
		}
		return "reuse"
	}
	return "new"
}

func finishPreset(c Catalog, p PresetPreview, draft Draft, lists map[string]NamedList, blobs map[string]string) (Catalog, PresetPreview, error) {
	if len(p.Issues) != 0 {
		return Catalog{}, p, nil
	}
	next, err := c.withImportedDraft(draft, lists, blobs)
	if err != nil {
		var option *nativeOptionError
		if errors.As(err, &option) {
			token := option.token
			if original, ok := p.projected[token.start]; ok {
				token = original
			}
			p.issue(&token, "invalid-option", option.err.Error())
		} else {
			p.issue(nil, "invalid-strategy", err.Error())
		}
		return Catalog{}, p, nil
	}
	record := next.Records[draft.ID]
	p.Record = &record
	return next, p, nil
}

func decodePresetTransfer(data []byte) (Transfer, error) {
	var t Transfer
	invalid := errors.New("invalid Solovey strategy JSON: expected one transfer with no duplicate or unknown fields")
	if !utf8.Valid(data) || !json.Valid(data) {
		return t, invalid
	}
	shape := json.NewDecoder(bytes.NewReader(data))
	shape.UseNumber()
	if !uniquePresetJSON(shape, 0) || shape.More() {
		return t, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&t); err != nil || decoder.Decode(new(any)) != io.EOF {
		return t, invalid
	}
	return t, nil
}

func uniquePresetJSON(decoder *json.Decoder, depth int) bool {
	if depth > 24 { // The Transfer schema needs fewer than twelve levels.
		return false
	}
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delim, nested := token.(json.Delim)
	if !nested {
		return true
	}
	keys := map[string]struct{}{}
	for decoder.More() {
		if delim == '{' {
			keyToken, err := decoder.Token()
			key, ok := keyToken.(string)
			if err != nil || !ok {
				return false
			}
			key = strings.ToLower(key) // encoding/json also accepts case-insensitive struct keys.
			if _, duplicate := keys[key]; duplicate {
				return false
			}
			keys[key] = struct{}{}
		}
		if !uniquePresetJSON(decoder, depth+1) {
			return false
		}
	}
	_, err = decoder.Token()
	return err == nil
}
