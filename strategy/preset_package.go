package strategy

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-host/outbounds"
)

const (
	MaxPresetBytes       = MaxCatalogBytes
	MaxNativeReviewBytes = 64 << 10
	maxPresetEntries     = 128
)

// ExportPackage carries the exact existing transfer, including shared IDs
// and raw comments. It does not include engine code or filesystem paths.
func (c Catalog) ExportPackage(id string) ([]byte, error) {
	transfer, err := c.Export(id)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(transfer)
	if err != nil {
		return nil, err
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
	if len(data) > MaxPresetBytes || output.Len() > MaxPresetBytes {
		return nil, errors.New("the Strategy does not fit the preset package budget; use a catalog backup")
	}
	return output.Bytes(), nil
}

func readPresetPackage(data []byte) (map[string][]byte, error) {
	if !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return nil, errors.New("expected a data-only ZIP package")
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(archive.File) == 0 || len(archive.File) > maxPresetEntries {
		return nil, fmt.Errorf("invalid preset package; only ZIP with 1 to %d files is accepted", maxPresetEntries)
	}
	files := make(map[string][]byte, len(archive.File))
	remaining := uint64(MaxPresetBytes)
	for _, entry := range archive.File {
		name := entry.Name
		limit, ok := presetEntryLimit(name)
		if !ok || !entry.Mode().IsRegular() || entry.Flags&1 != 0 {
			return nil, errors.New("ZIP contains an unsupported file, unsafe path, link, or encrypted entry")
		}
		if _, duplicate := files[name]; duplicate {
			return nil, fmt.Errorf("duplicate ZIP entry %q", name)
		}
		if entry.UncompressedSize64 > uint64(limit) || entry.UncompressedSize64 > remaining {
			return nil, fmt.Errorf("ZIP entry %q exceeds its %d-byte file limit or the %d-byte aggregate limit", name, limit, MaxPresetBytes)
		}
		input, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("cannot read ZIP entry %q", name)
		}
		content, err := io.ReadAll(io.LimitReader(input, int64(entry.UncompressedSize64)+1))
		closeErr := input.Close()
		if err != nil || closeErr != nil || uint64(len(content)) != entry.UncompressedSize64 || !utf8.Valid(content) {
			return nil, fmt.Errorf("invalid or non-text ZIP entry %q", name)
		}
		remaining -= uint64(len(content))
		files[name] = content
	}
	return files, nil
}

func presetEntryLimit(name string) (int, bool) {
	switch name {
	case "strategy.json":
		return MaxPresetBytes, true
	case "strategy.txt":
		return MaxNativeReviewBytes, true
	}
	if presetAssetID(name, "lists/", ".txt") != "" {
		return maxNativeListBytes * 2, true // Also allow CRLF and list comments.
	}
	if presetAssetID(name, "blobs/", ".hex") != "" {
		return MaxNativeBlobBytes*2 + 2, true // Optional trailing newline.
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
