package strategy

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-host/outbounds"
)

const (
	// Legacy bounded JSON editor requests do not share the preset upload budget.
	MaxPresetBytes         = 256 << 10
	MaxPresetTransferBytes = 3 * ((1 << 20) / 4) // 1 MiB base64 field; HTTP framing has a separate budget.
	// Frozen ZapretGUI portable archive limits. The caller may grant less
	// workspace, independently of the compressed transport and active engine.
	MaxPresetExpandedBytes      = 128 << 20
	maxPresetMemberBytes        = 32 << 20
	DefaultPresetExpandedBytes  = 6 << 20
	MaxNativeReviewBytes        = 64 << 10
	maxPresetEntries            = 128
	compactPresetTransferSchema = "solovey.zapret-transfer/v2"
)

// ExportPackage carries the exact existing transfer, including shared IDs
// and raw comments. It does not include engine code or filesystem paths.
func (c Catalog) ExportPackage(id string) ([]byte, error) {
	return c.ExportPackageWithin(id, DefaultPresetExpandedBytes)
}

func (c Catalog) ExportPackageWithin(id string, expandedBytes int) ([]byte, error) {
	transfer, err := c.Export(id)
	if err != nil {
		return nil, err
	}
	transfer = transfer.compactPresetLists()
	data, err := json.Marshal(transfer)
	if err != nil {
		return nil, err
	}
	if limit := min(maxPresetMemberBytes, presetExpansionLimit(expandedBytes)); len(data) > limit {
		return nil, fmt.Errorf("strategy.json needs %d bytes; ZIP file or expansion budget is %d bytes; use a catalog backup", len(data), limit)
	}
	if transfer.Schema == compactPresetTransferSchema {
		if err := transfer.checkPresetListBudget(presetExpansionLimit(expandedBytes) - len(data)); err != nil {
			return nil, err
		}
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	entry, err := archive.Create("strategy.json")
	if err == nil {
		_, err = entry.Write(data)
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if output.Len() > MaxPresetTransferBytes {
		return nil, fmt.Errorf("compressed preset needs %d bytes; transfer limit is %d bytes; use a catalog backup", output.Len(), MaxPresetTransferBytes)
	}
	return output.Bytes(), nil
}

// A ZIP transfer stores shared list entries once, not again in every profile.
// Its revision still refers to the complete definition after rehydration.
func (t Transfer) compactPresetLists() Transfer {
	if len(t.Strategy.ListRefs) == 0 {
		return t
	}
	t.Schema = compactPresetTransferSchema
	t.Strategy.Definition.Profiles = slices.Clone(t.Strategy.Definition.Profiles)
	for i := range t.Strategy.Definition.Profiles {
		profile := &t.Strategy.Definition.Profiles[i]
		refs, referenced := t.Strategy.ListRefs[profile.ID]
		if !referenced || profile.Lists == nil {
			continue
		}
		lists := *profile.Lists
		fields := lists.fields()
		for index, id := range refs.ids() {
			if id != "" {
				*fields[index].entries = nil
			}
		}
		if len(lists.HostList)+len(lists.HostListExclude)+len(lists.IPSet)+len(lists.IPSetExclude) == 0 {
			profile.Lists = nil
		} else {
			profile.Lists = &lists
		}
	}
	return t
}

func (t Transfer) checkPresetListBudget(remaining int) error {
	charges := make(map[string]int, len(t.Lists))
	for _, refs := range t.Strategy.ListRefs {
		for _, id := range refs.ids() {
			if id == "" {
				continue
			}
			charge, ok := charges[id]
			if !ok {
				list, exists := t.Lists[id]
				if !exists {
					return fmt.Errorf("compact preset is missing native list %q", id)
				}
				// Allow for the field name and array framing. Valid hosts and
				// prefixes are ASCII and need no JSON escaping.
				charge = 32
				for _, entry := range list.Entries {
					charge += len(entry) + 3
				}
				charges[id] = charge
			}
			if charge > remaining {
				return fmt.Errorf("native list %q needs %d projected bytes; %d remain in the preset expansion budget", id, charge, remaining)
			}
			remaining -= charge
		}
	}
	return nil
}

func presetExpansionLimit(expandedBytes int) int {
	return max(0, min(expandedBytes, MaxPresetExpandedBytes))
}

func readPresetPackage(data []byte, expandedBytes int) (map[string][]byte, error) {
	if !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return nil, errors.New("expected a data-only ZIP package")
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(archive.File) == 0 || len(archive.File) > maxPresetEntries {
		return nil, fmt.Errorf("invalid preset package; only ZIP with 1 to %d files is accepted", maxPresetEntries)
	}
	entryLimit := presetEntryLimit
	if slices.ContainsFunc(archive.File, func(entry *zip.File) bool { return entry.Name == "manifest.json" }) {
		entryLimit = guiPresetEntryLimit
	}
	files := make(map[string][]byte, len(archive.File))
	budget := uint64(presetExpansionLimit(expandedBytes))
	remaining := budget
	for _, entry := range archive.File {
		name := entry.Name
		limit, ok := entryLimit(name)
		if !ok || !entry.Mode().IsRegular() || entry.Flags&1 != 0 {
			return nil, errors.New("ZIP contains an unsupported file, unsafe path, link, or encrypted entry")
		}
		if _, duplicate := files[name]; duplicate {
			return nil, fmt.Errorf("duplicate ZIP entry %q", name)
		}
		if entry.UncompressedSize64 > uint64(limit) {
			return nil, fmt.Errorf("ZIP entry %q needs %d bytes; file limit is %d bytes", name, entry.UncompressedSize64, limit)
		}
		if entry.UncompressedSize64 > remaining {
			return nil, fmt.Errorf("ZIP entry %q needs %d expanded bytes; %d of the %d-byte import workspace remains", name, entry.UncompressedSize64, remaining, budget)
		}
		input, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("cannot read ZIP entry %q", name)
		}
		content, err := io.ReadAll(io.LimitReader(input, int64(entry.UncompressedSize64)+1))
		closeErr := input.Close()
		if err != nil || closeErr != nil || uint64(len(content)) != entry.UncompressedSize64 ||
			(presetAssetID(name, "blobs/", ".bin") == "" && !utf8.Valid(content)) {
			return nil, fmt.Errorf("invalid ZIP entry %q; text entries must be UTF-8", name)
		}
		remaining -= uint64(len(content))
		files[name] = content
	}
	return files, nil
}

func presetEntryLimit(name string) (int, bool) {
	switch name {
	case "strategy.json":
		return maxPresetMemberBytes, true
	case "strategy.txt":
		return MaxNativeReviewBytes, true
	}
	if presetAssetID(name, "lists/", ".txt") != "" {
		return maxPresetMemberBytes, true
	}
	if presetAssetID(name, "blobs/", ".hex") != "" {
		return MaxNativeBlobBytes*2 + 2, true // Optional trailing newline.
	}
	if presetAssetID(name, "blobs/", ".bin") != "" {
		return MaxNativeBlobBytes, true
	}
	return 0, false
}

func presetAssetID(name, directory, suffix string) string {
	id, ok := strings.CutPrefix(name, directory)
	if !ok {
		return ""
	}
	id, ok = strings.CutSuffix(id, suffix)
	if !ok || !outbounds.ValidZapretStrategyRef(id) {
		return ""
	}
	return id
}
