// Package provenance owns durable records that connect OpenWALDO BOMs,
// training observations, and model artifacts.
package provenance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/openwaldo/waldo-new/internal/corpus"
)

const CorpusExportSchema = 1

type CorpusExport struct {
	Kind      string                `json:"kind"`
	Schema    int                   `json:"schema"`
	Generated string                `json:"generated"`
	Format    string                `json:"format"`
	BOM       corpus.BOM            `json:"bom"`
	Files     []corpus.ExportedFile `json:"files"`
}

func NewCorpusExport(bom corpus.BOM, format string, files []corpus.ExportedFile, generated time.Time) CorpusExport {
	return CorpusExport{
		Kind:      "waldo-corpus-export",
		Schema:    CorpusExportSchema,
		Generated: generated.UTC().Format(time.RFC3339),
		Format:    format,
		BOM:       bom,
		Files:     append([]corpus.ExportedFile(nil), files...),
	}
}

func WriteCorpusExport(destination string, document CorpusExport) error {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(destination, ".waldo-export-bom-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	path := filepath.Join(destination, "EXPORT.json")
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	committed = true
	return nil
}

// CheckCorpusExportDestination permits a fresh or interrupted export and a
// resume of the same completed OpenWALDO BOM. It refuses to mix two selections in
// one directory because stale data files would otherwise look current.
func CheckCorpusExportDestination(destination string, bom corpus.BOM, format string) error {
	path := filepath.Join(destination, "EXPORT.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var existing CorpusExport
	if err := json.Unmarshal(data, &existing); err != nil {
		return fmt.Errorf("%s is not a readable WALDO export: %w", path, err)
	}
	if existing.Kind != "waldo-corpus-export" || existing.Schema != CorpusExportSchema {
		return fmt.Errorf("%s is not a supported WALDO corpus export", path)
	}
	if !reflect.DeepEqual(existing.BOM, bom) {
		return fmt.Errorf("%s contains a different OpenWALDO BOM; choose another output directory", path)
	}
	if existing.Format != format {
		return fmt.Errorf("%s contains a %s export, not %s; choose another output directory", path, existing.Format, format)
	}
	return nil
}
