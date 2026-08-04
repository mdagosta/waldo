package corpus

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/openwaldo/waldo-new/internal/lookaside"
)

type ExportedFile struct {
	Path     string `json:"path"`
	Manifest string `json:"manifest"`
	SHA256   string `json:"sha256"`
	Format   string `json:"format"`
	License  string `json:"license"`
	Docs     int64  `json:"docs"`
	Tokens   int64  `json:"tokens"`
	Bytes    int64  `json:"bytes"`
	Existing bool   `json:"-"`
}

// ExportNative copies verified materialized objects into a portable directory
// without linking them to the cache. A user editing an export therefore cannot
// corrupt the shared verified-object cache.
func ExportNative(materialized Materialized, destination string, force bool) ([]ExportedFile, error) {
	if destination == "" {
		return nil, fmt.Errorf("export destination is required")
	}
	abs, err := filepath.Abs(destination)
	if err != nil {
		return nil, err
	}
	files := make([]ExportedFile, 0, len(materialized.Objects))
	seenPaths := map[string]bool{}
	for _, object := range materialized.Objects {
		relative := exportPath(object.Shard)
		if seenPaths[relative] {
			return nil, fmt.Errorf("export path collision at %s", relative)
		}
		seenPaths[relative] = true
		destinationPath := filepath.Join(abs, filepath.FromSlash(relative))
		existing := false
		if info, statErr := os.Stat(destinationPath); statErr == nil && !info.IsDir() {
			if verifyErr := lookaside.VerifyFile(destinationPath, object.Shard.SHA256, object.Shard.Bytes); verifyErr == nil {
				existing = true
			} else if !force {
				return nil, fmt.Errorf("%s already exists but does not match the selected object: %w (use --force to replace it)", destinationPath, verifyErr)
			}
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return nil, statErr
		}
		if !existing {
			if err := copyVerified(object.Path, destinationPath, object.Shard.SHA256, object.Shard.Bytes); err != nil {
				return nil, err
			}
		}
		files = append(files, ExportedFile{
			Path: relative, Manifest: object.Shard.Manifest, SHA256: object.Shard.SHA256,
			Format: object.Shard.Format, License: object.Shard.License,
			Docs: object.Shard.Docs, Tokens: object.Shard.Tokens, Bytes: object.Shard.Bytes,
			Existing: existing,
		})
	}
	return files, nil
}

func exportPath(shard ShardPin) string {
	manifest := filepath.ToSlash(shard.Manifest)
	dir := strings.TrimSuffix(manifest, filepath.Ext(manifest))
	base := strings.TrimSuffix(filepath.Base(manifest), filepath.Ext(manifest))
	// The common tree shape repeats the corpus name as directory and manifest.
	// Avoid a third repetition in the exported filename.
	if filepath.Base(filepath.Dir(manifest)) == base {
		dir = filepath.Dir(manifest)
	}
	filename := base + "-" + shard.SHA256[:12] + formatExtension(shard.Format)
	return filepath.ToSlash(filepath.Join("data", dir, filename))
}

func formatExtension(format string) string {
	switch format {
	case "", "parquet":
		return ".parquet"
	case "jsonl.zst", "zst", "zstd":
		return ".jsonl.zst"
	case "jsonl.gz", "gz", "gzip":
		return ".jsonl.gz"
	default:
		return ".bin"
	}
}

func copyVerified(source, destination, digest string, expectedBytes int64) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".waldo-export-*")
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
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hasher), input)
	if err != nil {
		return err
	}
	if written != expectedBytes {
		return fmt.Errorf("copy %s: size mismatch: got %d bytes, want %d", source, written, expectedBytes)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != digest {
		return fmt.Errorf("copy %s: sha256 mismatch: got %s, want %s", source, got, digest)
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
	if err := os.Rename(temporaryPath, destination); err != nil {
		return err
	}
	committed = true
	return nil
}
