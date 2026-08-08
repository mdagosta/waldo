// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package corpus

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/openwaldo/waldo/internal/lookaside"
	"github.com/openwaldo/waldo/internal/shard"
)

type ExportedFile struct {
	Path         string   `json:"path"`
	Manifest     string   `json:"manifest"`
	ObjectSHA256 string   `json:"object_sha256"`
	SHA256       string   `json:"sha256"`
	Format       string   `json:"format"`
	License      string   `json:"license,omitempty"`
	Licenses     []string `json:"licenses,omitempty"`
	Docs         int64    `json:"docs"`
	Tokens       int64    `json:"tokens"`
	ObjectBytes  int64    `json:"object_bytes"`
	Bytes        int64    `json:"bytes"`
	Existing     bool     `json:"-"`
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
			Path: relative, Manifest: object.Shard.Manifest,
			ObjectSHA256: object.Shard.SHA256, SHA256: object.Shard.SHA256,
			Format: object.Shard.Format, License: object.Shard.License,
			Licenses: append([]string(nil), object.Shard.Licenses...),
			Docs:     object.Shard.Docs, Tokens: object.Shard.Tokens,
			ObjectBytes: object.Shard.Bytes, Bytes: object.Shard.Bytes,
			Existing: existing,
		})
	}
	return files, nil
}

// ExportJSONL converts verified native Parquet objects into canonical JSONL.
// Existing output is accepted only when conversion produces the same bytes.
func ExportJSONL(materialized Materialized, destination string, force bool) ([]ExportedFile, error) {
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
		if object.Shard.Format != "" && object.Shard.Format != "parquet" {
			return nil, fmt.Errorf("%s uses %q; canonical JSONL export currently requires Parquet shards", object.Shard.Manifest, object.Shard.Format)
		}
		relative := strings.TrimSuffix(exportPath(object.Shard), filepath.Ext(exportPath(object.Shard))) + ".jsonl"
		if seenPaths[relative] {
			return nil, fmt.Errorf("export path collision at %s", relative)
		}
		seenPaths[relative] = true
		destinationPath := filepath.Join(abs, filepath.FromSlash(relative))
		digest, bytes, existing, err := convertJSONL(object.Path, destinationPath, object.Shard.Docs, object.Shard.Tokens, force)
		if err != nil {
			return nil, err
		}
		files = append(files, ExportedFile{
			Path: relative, Manifest: object.Shard.Manifest,
			ObjectSHA256: object.Shard.SHA256, SHA256: digest,
			Format: "jsonl", License: object.Shard.License,
			Licenses: append([]string(nil), object.Shard.Licenses...),
			Docs:     object.Shard.Docs, Tokens: object.Shard.Tokens,
			ObjectBytes: object.Shard.Bytes, Bytes: bytes, Existing: existing,
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

func convertJSONL(source, destination string, expectedDocs, expectedTokens int64, force bool) (string, int64, bool, error) {
	input, err := os.Open(source)
	if err != nil {
		return "", 0, false, err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return "", 0, false, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", 0, false, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".waldo-jsonl-*")
	if err != nil {
		return "", 0, false, err
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
	stats, err := shard.WriteJSONL(io.MultiWriter(temporary, hasher), input, info.Size())
	if err != nil {
		return "", 0, false, fmt.Errorf("convert %s: %w", source, err)
	}
	if stats.Docs != expectedDocs || stats.Tokens != expectedTokens {
		return "", 0, false, fmt.Errorf("convert %s: records report %d docs and %d tokens, manifest declares %d docs and %d tokens", source, stats.Docs, stats.Tokens, expectedDocs, expectedTokens)
	}
	digest := hex.EncodeToString(hasher.Sum(nil))
	if existingInfo, statErr := os.Stat(destination); statErr == nil && !existingInfo.IsDir() {
		if verifyErr := lookaside.VerifyFile(destination, digest, stats.Bytes); verifyErr == nil {
			return digest, stats.Bytes, true, nil
		} else if !force {
			return "", 0, false, fmt.Errorf("%s already exists but is not the canonical conversion: %w (use --force to replace it)", destination, verifyErr)
		}
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return "", 0, false, statErr
	}
	if err := temporary.Chmod(0o644); err != nil {
		return "", 0, false, err
	}
	if err := temporary.Sync(); err != nil {
		return "", 0, false, err
	}
	if err := temporary.Close(); err != nil {
		return "", 0, false, err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return "", 0, false, err
	}
	committed = true
	return digest, stats.Bytes, false, nil
}
