package strategy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/MalenkiySolovey/solovey-host/outbounds"
)

const guiPresetFormat = "zapretgui-portable-preset"

// PresetOrigin describes the uploaded input, not the current runtime revision
// or a signature from the original publisher.
type PresetOrigin struct {
	Format   string `json:"format"`
	Version  int    `json:"version"`
	Engine   string `json:"engine"`
	FileName string `json:"fileName"`
	SHA256   string `json:"sha256"`
}

func (p PresetOrigin) valid() bool {
	return p.Format == guiPresetFormat && p.Version == 1 && p.Engine == "winws2" &&
		guiTextFileName(p.FileName) && validSHA256(p.SHA256)
}

func validSHA256(s string) bool {
	if len(s) != sha256.Size*2 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

type guiPresetManifest struct {
	Format   string          `json:"format"`
	Version  int             `json:"version"`
	Engine   string          `json:"engine"`
	FileName string          `json:"preset_file_name"`
	Lists    []guiPresetList `json:"lists"`
}

type guiPresetList struct {
	FileName string `json:"file_name"`
	Kind     string `json:"kind"`
	Storage  string `json:"storage"`
	Path     string `json:"archive_path"`
	SHA256   string `json:"sha256"`
}

type guiPresetSource struct {
	origin PresetOrigin
	lists  map[string]guiPresetList
}

func guiPresetEntryLimit(path string) (int, bool) {
	switch path {
	case "manifest.json":
		return MaxNativeReviewBytes, true
	case "preset.txt":
		return MaxNativeReviewBytes, true
	}
	if name, ok := strings.CutPrefix(path, "lists/"); ok && guiTextFileName(name) {
		return maxNativeListBytes * 2, true
	}
	return 0, false
}

func guiTextFileName(name string) bool {
	return len(name) > len(".txt") && len(name) <= 255 && strings.HasSuffix(strings.ToLower(name), ".txt") &&
		strings.TrimSpace(name) == name && !strings.ContainsAny(name, `/\<>:"|?*,`) && !strings.ContainsFunc(name, unicode.IsControl)
}

func (c Catalog) prepareGUIPreset(id, name string, data []byte, files map[string][]byte) (Catalog, PresetPreview, error) {
	var manifest guiPresetManifest
	if err := decodePresetJSON(files["manifest.json"], &manifest); err != nil || manifest.Format != guiPresetFormat || manifest.Version != 1 {
		return Catalog{}, PresetPreview{}, errors.New("expected a ZapretGUI portable preset manifest, version 1, with unique known fields")
	}
	if manifest.Engine != "winws2" {
		return Catalog{}, PresetPreview{}, errors.New("only Zapret2 (winws2) portable archives are supported; winws1 uses a different engine grammar")
	}
	source, ok := files["preset.txt"]
	if !ok || !guiTextFileName(manifest.FileName) || len(manifest.Lists) != len(files)-2 {
		return Catalog{}, PresetPreview{}, errors.New("ZapretGUI archive needs a named preset.txt and exactly its manifest-listed files")
	}
	origin := PresetOrigin{guiPresetFormat, manifest.Version, manifest.Engine, manifest.FileName, fmt.Sprintf("%x", sha256.Sum256(data))}
	gui := guiPresetSource{origin: origin, lists: make(map[string]guiPresetList, len(manifest.Lists))}
	for _, list := range manifest.Lists {
		key := strings.ToLower(list.FileName)
		_, duplicate := gui.lists[key]
		if duplicate || !guiTextFileName(list.FileName) || list.Path != "lists/"+list.FileName ||
			(list.Kind != "hostlist" && list.Kind != "ipset") || (list.Storage != "standalone" && list.Storage != "overlay") {
			return Catalog{}, PresetPreview{}, errors.New("ZapretGUI archive contains an invalid or duplicate list declaration")
		}
		content, exists := files[list.Path]
		if !exists || list.SHA256 != guiListSHA256(content) {
			return Catalog{}, PresetPreview{}, fmt.Errorf("ZapretGUI archive list %q is missing or does not match its SHA-256", list.Path)
		}
		gui.lists[key] = list
	}
	delete(files, "manifest.json")
	delete(files, "preset.txt")
	return c.prepareNativeSource(id, firstName(name, manifest.FileName[:len(manifest.FileName)-len(".txt")]), string(source), files, &gui)
}

func guiListSHA256(data []byte) string {
	// ZapretGUI v1 hashes text after normalizing line endings and the final newline.
	text := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimRight(text, "\n")+"\n")))
}

func (p *nativePresetPlan) mapGUIList(token sourceToken, value string, kind ListKind, slot *string) {
	path := strings.ReplaceAll(value, `\`, "/")
	name, hasPrefix := strings.CutPrefix(path, "lists/")
	if !hasPrefix || !guiTextFileName(name) {
		p.preview.issue(&token, "unsafe-list-path", "use one relative lists/<file>.txt dependency from the archive; host paths are never read")
		return
	}
	key := strings.ToLower(name)
	id := key[:len(key)-len(".txt")]
	if !outbounds.ValidZapretStrategyRef(id) {
		digest := sha256.Sum256([]byte(key))
		id = "gui-list-" + hex.EncodeToString(digest[:16])
	}
	list, found := p.gui.lists[key]
	state := "missing"
	var err error
	switch {
	case !found:
		err = fmt.Errorf("archive does not include %q; export or supply the full effective list as a standalone file", path)
	case list.Kind != strings.ToLower(string(kind)):
		p.used[list.Path] = true
		state, err = "conflict", fmt.Errorf("archive declares %q as %s, not %s", list.Path, list.Kind, kind)
	case list.Storage == "overlay":
		p.used[list.Path] = true
		err = fmt.Errorf("%q contains only additions to an unverified base list; supply the full effective list as a standalone file", list.Path)
	default:
		state, err = p.prepareList(list.Path, id, kind)
	}
	p.preview.Dependencies = append(p.preview.Dependencies, PresetDependency{path, id, string(kind), state})
	if err != nil {
		p.preview.issue(&token, "list-dependency", err.Error())
		return
	}
	*slot = id
	p.edits[token.start] = ""
}
