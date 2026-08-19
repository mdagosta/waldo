// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/openwaldo/waldo/internal/index"
)

const (
	SourceDirectoryKind   = "waldo-source-directory"
	SourceDirectorySchema = 1
	sourceManifestMaximum = 4 << 20
)

type SourceDirectoryManifest struct {
	Kind        string                  `json:"kind"`
	Schema      int                     `json:"schema"`
	RetrievedAt string                  `json:"retrieved_at"`
	Corpus      SourceDirectoryCorpus   `json:"corpus"`
	Sources     []SourceDirectorySource `json:"sources"`
	Fetcher     json.RawMessage         `json:"fetcher"`
	Raw         SourceDirectoryRaw      `json:"raw"`
	Root        string                  `json:"-"`
}

type SourceDirectoryCorpus struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Destination is accepted only for compatibility with early handoffs.
	// Fetching does not choose an index destination and WALDO ignores it.
	Destination string `json:"destination,omitempty"`
}

type SourceDirectorySource struct {
	ID        string            `json:"id"`
	Path      string            `json:"path"`
	License   string            `json:"license"`
	Source    RecipeSource      `json:"source"`
	Input     InputProfile      `json:"input"`
	Artifacts []json.RawMessage `json:"artifacts"`
}

type SourceDirectoryRaw struct {
	Path       string `json:"path"`
	FileCount  int64  `json:"file_count"`
	ByteCount  int64  `json:"byte_count"`
	TreeSHA256 string `json:"tree_sha256"`
}

func LoadSourceDirectory(path string) (SourceDirectoryManifest, bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return SourceDirectoryManifest{}, false, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return SourceDirectoryManifest{}, false, nil
		}
		return SourceDirectoryManifest{}, false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return SourceDirectoryManifest{}, false, nil
	}
	manifestPath := filepath.Join(abs, "manifest.json")
	manifestInfo, err := os.Lstat(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return SourceDirectoryManifest{}, false, nil
		}
		return SourceDirectoryManifest{}, false, err
	}
	if !manifestInfo.Mode().IsRegular() || manifestInfo.Mode()&os.ModeSymlink != 0 {
		return SourceDirectoryManifest{}, true, fmt.Errorf("source manifest must be a regular non-symlink file")
	}
	if manifestInfo.Size() > sourceManifestMaximum {
		return SourceDirectoryManifest{}, true, fmt.Errorf("source manifest exceeds %d bytes", sourceManifestMaximum)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return SourceDirectoryManifest{}, true, err
	}
	var header struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return SourceDirectoryManifest{}, true, fmt.Errorf("malformed source manifest: %w", err)
	}
	if header.Kind != SourceDirectoryKind {
		return SourceDirectoryManifest{}, false, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest SourceDirectoryManifest
	if err := decoder.Decode(&manifest); err != nil {
		return SourceDirectoryManifest{}, true, fmt.Errorf("malformed source manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values are not allowed")
		}
		return SourceDirectoryManifest{}, true, fmt.Errorf("malformed source manifest: %w", err)
	}
	manifest.Root = abs
	if err := manifest.Validate(); err != nil {
		return SourceDirectoryManifest{}, true, err
	}
	return manifest, true, nil
}

func (manifest SourceDirectoryManifest) Validate() error {
	if manifest.Kind != SourceDirectoryKind || manifest.Schema != SourceDirectorySchema {
		return fmt.Errorf("unsupported source directory identity %q schema %d", manifest.Kind, manifest.Schema)
	}
	if strings.TrimSpace(manifest.Corpus.ID) == "" || strings.TrimSpace(manifest.Corpus.Title) == "" ||
		strings.TrimSpace(manifest.Corpus.Description) == "" {
		return fmt.Errorf("source manifest corpus id, title, and description are required")
	}
	if manifest.Raw.Path != "raw" {
		return fmt.Errorf("source manifest raw.path must be %q", "raw")
	}
	rawRoot := filepath.Join(manifest.Root, manifest.Raw.Path)
	if err := requireSourceDirectory(rawRoot); err != nil {
		return err
	}
	if len(manifest.Sources) == 0 {
		return fmt.Errorf("source manifest requires at least one source")
	}
	seen := map[string]bool{}
	seenPaths := map[string]bool{}
	for position, source := range manifest.Sources {
		if !recipeStepName.MatchString(source.ID) || seen[source.ID] {
			return fmt.Errorf("source %d has invalid or duplicate id %q", position+1, source.ID)
		}
		seen[source.ID] = true
		cleanPath := filepath.ToSlash(filepath.Clean(source.Path))
		if source.Path == "" {
			cleanPath = ""
		}
		if filepath.IsAbs(source.Path) || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") || cleanPath != source.Path {
			return fmt.Errorf("source %q has unsafe raw path %q", source.ID, source.Path)
		}
		if len(manifest.Sources) > 1 && cleanPath == "" {
			return fmt.Errorf("source %q requires a distinct raw path in a multi-source manifest", source.ID)
		}
		if seenPaths[cleanPath] {
			return fmt.Errorf("source %q has duplicate raw path %q", source.ID, source.Path)
		}
		seenPaths[cleanPath] = true
		for previous := range seenPaths {
			if previous != cleanPath && (strings.HasPrefix(previous, cleanPath+"/") || strings.HasPrefix(cleanPath, previous+"/")) {
				return fmt.Errorf("source %q has overlapping raw path %q", source.ID, source.Path)
			}
		}
		if err := validateSourceDirectorySource(source); err != nil {
			return fmt.Errorf("source %q: %w", source.ID, err)
		}
		if err := requireSourceDirectory(filepath.Join(rawRoot, filepath.FromSlash(source.Path))); err != nil {
			return fmt.Errorf("source %q: %w", source.ID, err)
		}
	}
	return nil
}

func validateSourceDirectorySource(source SourceDirectorySource) error {
	if strings.TrimSpace(source.License) == "" {
		return fmt.Errorf("license is required")
	}
	if strings.TrimSpace(source.Source.URL) == "" || strings.TrimSpace(source.Source.Category) == "" {
		return fmt.Errorf("source url and category are required")
	}
	category, ok := index.CanonicalSourceCategory(source.Source.Category)
	if !ok {
		return fmt.Errorf("unsupported source category %q", source.Source.Category)
	}
	if err := index.ValidateSourceProvenance(index.Source{
		Category: category, CollectedFrom: source.Source.CollectedFrom, CollectedTo: source.Source.CollectedTo,
		LicenseEvidence: source.Source.LicenseEvidence, Content: source.Source.Content, Acquisition: source.Source.Acquisition,
	}); err != nil {
		return err
	}
	return source.Input.Validate()
}

func (manifest SourceDirectoryManifest) VerifyProbe(probe Probe) error {
	if probe.Kind != "waldo-ingest-probe" || probe.Schema != 1 {
		return fmt.Errorf("invalid source-directory probe")
	}
	rawRoot := filepath.Join(manifest.Root, manifest.Raw.Path)
	type inventoryEntry struct {
		path   string
		sha256 string
		bytes  int64
	}
	entries := make([]inventoryEntry, 0, len(probe.Artifacts))
	var totalBytes int64
	for _, artifact := range probe.Artifacts {
		relative, err := filepath.Rel(rawRoot, artifact.Path)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("probed input %s is outside raw directory", artifact.Path)
		}
		relative = filepath.ToSlash(relative)
		if strings.ContainsAny(relative, "\t\n") {
			return fmt.Errorf("raw filename contains a tab or newline: %s", relative)
		}
		entries = append(entries, inventoryEntry{path: relative, sha256: artifact.SHA256, bytes: artifact.Bytes})
		totalBytes += artifact.Bytes
	}
	slices.SortFunc(entries, func(left, right inventoryEntry) int { return strings.Compare(left.path, right.path) })
	hash := sha256.New()
	for _, entry := range entries {
		fmt.Fprintf(hash, "%s\t%d\t%s\n", entry.sha256, entry.bytes, entry.path)
	}
	actualTree := fmt.Sprintf("%x", hash.Sum(nil))
	if int64(len(entries)) != manifest.Raw.FileCount || totalBytes != manifest.Raw.ByteCount || actualTree != manifest.Raw.TreeSHA256 {
		return fmt.Errorf("raw tree does not match source manifest: files %d/%d, bytes %d/%d, sha256 %s/%s",
			len(entries), manifest.Raw.FileCount, totalBytes, manifest.Raw.ByteCount, actualTree, manifest.Raw.TreeSHA256)
	}
	return nil
}

func requireSourceDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is not a non-symlink directory", path)
	}
	return nil
}

func (manifest SourceDirectoryManifest) InputPaths() []string {
	paths := make([]string, 0, len(manifest.Sources))
	for _, source := range manifest.Sources {
		paths = append(paths, filepath.Join(manifest.Root, manifest.Raw.Path, filepath.FromSlash(source.Path)))
	}
	return paths
}

func (manifest SourceDirectoryManifest) Apply(request *PlanRequest) {
	request.Title = manifest.Corpus.Title
	request.Description = manifest.Corpus.Description
	request.Sources = make([]PlanSourceRequest, 0, len(manifest.Sources))
	for _, source := range manifest.Sources {
		name := source.Source.Name
		if name == "" {
			name = source.ID
		}
		request.Sources = append(request.Sources, PlanSourceRequest{
			ID: source.ID, License: source.License,
			Source:    source.Source.AsPlanSource(source.ID, name),
			InputRoot: filepath.Join(manifest.Root, manifest.Raw.Path, filepath.FromSlash(source.Path)),
			Profile:   source.Input,
		})
	}
}
