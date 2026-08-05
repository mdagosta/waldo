// Package provenance owns durable records that connect OpenWALDO BOMs,
// training observations, and model artifacts.
package provenance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/openwaldo/waldo/internal/corpus"
	"github.com/openwaldo/waldo/internal/lookaside"
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
	if err := document.Validate(); err != nil {
		return err
	}
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

// LoadCorpusExport reads EXPORT.json from a directory, or reads an explicitly
// named export document.
func LoadCorpusExport(location string) (CorpusExport, string, error) {
	path := location
	info, err := os.Stat(location)
	if err != nil {
		return CorpusExport{}, "", err
	}
	if info.IsDir() {
		path = filepath.Join(location, "EXPORT.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return CorpusExport{}, "", err
	}
	var document CorpusExport
	if err := json.Unmarshal(data, &document); err != nil {
		return CorpusExport{}, "", fmt.Errorf("%s: %w", path, err)
	}
	if err := document.Validate(); err != nil {
		return CorpusExport{}, "", fmt.Errorf("%s: %w", path, err)
	}
	return document, path, nil
}

// Validate checks all self-contained relationships and totals in an export
// document without reading its exported files.
func (document CorpusExport) Validate() error {
	if document.Kind != "waldo-corpus-export" || document.Schema != CorpusExportSchema {
		return fmt.Errorf("unsupported WALDO corpus export identity %q schema %d", document.Kind, document.Schema)
	}
	if _, err := time.Parse(time.RFC3339, document.Generated); err != nil {
		return fmt.Errorf("export generated time %q: %w", document.Generated, err)
	}
	if document.Format != "native" && document.Format != "jsonl" {
		return fmt.Errorf("unsupported export format %q", document.Format)
	}
	if err := document.BOM.Validate(); err != nil {
		return err
	}
	if len(document.Files) != len(document.BOM.Shards) {
		return fmt.Errorf("export has %d files for %d selected shards", len(document.Files), len(document.BOM.Shards))
	}
	paths := map[string]bool{}
	for position, file := range document.Files {
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(file.Path)))
		if file.Path == "" || strings.Contains(file.Path, "\\") || filepath.IsAbs(file.Path) || clean != file.Path || clean == ".." || strings.HasPrefix(clean, "../") || !strings.HasPrefix(clean, "data/") || paths[clean] {
			return fmt.Errorf("invalid or duplicate export path %q", file.Path)
		}
		paths[clean] = true
		shard := document.BOM.Shards[position]
		if file.Manifest != shard.Manifest || file.ObjectSHA256 != shard.SHA256 || file.ObjectBytes != shard.Bytes {
			return fmt.Errorf("export file %s does not match a selected lookaside object", file.Path)
		}
		if !validHash(file.SHA256) || file.Bytes <= 0 || file.License != shard.License || file.Docs != shard.Docs || file.Tokens != shard.Tokens {
			return fmt.Errorf("export file %s has invalid identity or totals", file.Path)
		}
		switch document.Format {
		case "native":
			if file.Format != shard.Format || file.SHA256 != shard.SHA256 || file.Bytes != shard.Bytes {
				return fmt.Errorf("native export file %s does not preserve its lookaside object", file.Path)
			}
		case "jsonl":
			if file.Format != "jsonl" {
				return fmt.Errorf("JSONL export file %s declares format %q", file.Path, file.Format)
			}
		}
	}
	return nil
}

type ExportVerification struct {
	Path  string `json:"path"`
	Files int64  `json:"files"`
	Bytes int64  `json:"bytes"`
}

// VerifyCorpusExport validates a document and hashes every exported file.
func VerifyCorpusExport(location string) (CorpusExport, ExportVerification, error) {
	document, path, err := LoadCorpusExport(location)
	if err != nil {
		return CorpusExport{}, ExportVerification{}, err
	}
	root := filepath.Dir(path)
	report := ExportVerification{Path: path}
	for _, file := range document.Files {
		filePath := filepath.Join(root, filepath.FromSlash(file.Path))
		if err := lookaside.VerifyFile(filePath, file.SHA256, file.Bytes); err != nil {
			return CorpusExport{}, ExportVerification{}, fmt.Errorf("verify export file %s: %w", file.Path, err)
		}
		report.Files++
		report.Bytes += file.Bytes
	}
	return document, report, nil
}

func validHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := range len(value) {
		if c := value[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// CheckCorpusExportDestination permits a fresh or interrupted export and a
// resume of the same completed OpenWALDO BOM. It refuses to mix two selections in
// one directory because stale data files would otherwise look current.
func CheckCorpusExportDestination(destination string, bom corpus.BOM, format string) error {
	path := filepath.Join(destination, "EXPORT.json")
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	existing, _, err := LoadCorpusExport(path)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(existing.BOM, bom) {
		return fmt.Errorf("%s contains a different OpenWALDO BOM; choose another output directory", path)
	}
	if existing.Format != format {
		return fmt.Errorf("%s contains a %s export, not %s; choose another output directory", path, existing.Format, format)
	}
	return nil
}
