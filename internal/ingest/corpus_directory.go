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
)

const CorpusDirectoryKind = "waldo-corpus-directory"

type CorpusDirectory struct {
	Kind    string                  `json:"kind"`
	Schema  int                     `json:"schema"`
	Corpus  SourceDirectoryCorpus   `json:"corpus"`
	Source  *DirectorySource        `json:"source,omitempty"`
	Sources []string                `json:"sources,omitempty"`
	Fetcher json.RawMessage         `json:"fetcher,omitempty"`
	Raw     *DirectoryRaw           `json:"raw,omitempty"`
	Root    string                  `json:"-"`
	Loaded  []LoadedDirectorySource `json:"-"`
}

type DirectorySource struct {
	ID        string            `json:"id"`
	License   string            `json:"license"`
	Source    SourceMetadata    `json:"source"`
	Input     InputProfile      `json:"input,omitempty"`
	Artifacts []json.RawMessage `json:"artifacts,omitempty"`
}

type DirectoryRaw struct {
	FileCount  int64  `json:"file_count"`
	ByteCount  int64  `json:"byte_count"`
	TreeSHA256 string `json:"tree_sha256"`
}

type LoadedDirectorySource struct {
	Directory string
	Source    DirectorySource
	Raw       DirectoryRaw
	Files     []string
}

type sourceDirectoryWire struct {
	Kind    string          `json:"kind"`
	Schema  int             `json:"schema"`
	Source  DirectorySource `json:"source"`
	Fetcher json.RawMessage `json:"fetcher,omitempty"`
	Raw     DirectoryRaw    `json:"raw"`
}

func LoadCorpusDirectory(path string) (CorpusDirectory, bool, error) {
	root, ok, err := regularDirectory(path)
	if err != nil || !ok {
		return CorpusDirectory{}, false, err
	}
	data, recognized, err := identifiedManifest(filepath.Join(root, "manifest.json"), CorpusDirectoryKind)
	if err != nil || !recognized {
		return CorpusDirectory{}, recognized, err
	}
	var corpus CorpusDirectory
	if err := decodeStrictJSON(data, &corpus); err != nil {
		return CorpusDirectory{}, true, fmt.Errorf("malformed corpus manifest: %w", err)
	}
	corpus.Root = root
	if err := corpus.load(); err != nil {
		return CorpusDirectory{}, true, err
	}
	return corpus, true, nil
}

func (corpus *CorpusDirectory) load() error {
	if corpus.Kind != CorpusDirectoryKind || corpus.Schema != SourceDirectorySchema {
		return fmt.Errorf("unsupported corpus directory identity %q schema %d", corpus.Kind, corpus.Schema)
	}
	if strings.TrimSpace(corpus.Corpus.ID) == "" || strings.TrimSpace(corpus.Corpus.Title) == "" || strings.TrimSpace(corpus.Corpus.Description) == "" {
		return fmt.Errorf("corpus manifest id, title, and description are required")
	}
	if (corpus.Source == nil) == (len(corpus.Sources) == 0) {
		return fmt.Errorf("corpus manifest requires either source or sources")
	}
	if corpus.Source != nil {
		if corpus.Raw == nil {
			return fmt.Errorf("single-source corpus manifest requires raw evidence")
		}
		if corpus.Source.ID == "" {
			corpus.Source.ID = corpus.Corpus.ID
		}
		if corpus.Source.ID != corpus.Corpus.ID {
			return fmt.Errorf("single-source id must equal corpus id")
		}
		if err := validateDirectorySource(*corpus.Source); err != nil {
			return fmt.Errorf("source %q: %w", corpus.Source.ID, err)
		}
		files, err := sourceBoundaryFiles(corpus.Root)
		if err != nil {
			return err
		}
		corpus.Loaded = []LoadedDirectorySource{{Directory: corpus.Root, Source: *corpus.Source, Raw: *corpus.Raw, Files: files}}
		return nil
	}
	if corpus.Raw != nil {
		return fmt.Errorf("multi-source corpus manifest must not contain raw evidence")
	}
	seen := map[string]bool{}
	declared := map[string]bool{"manifest.json": true}
	for position, name := range corpus.Sources {
		if !sourceIDPattern.MatchString(name) || seen[name] {
			return fmt.Errorf("source directory %d has invalid or duplicate name %q", position+1, name)
		}
		seen[name] = true
		declared[name] = true
		directory := filepath.Join(corpus.Root, name)
		if _, ok, err := regularDirectory(directory); err != nil || !ok {
			if err == nil {
				err = fmt.Errorf("not a non-symlink directory")
			}
			return fmt.Errorf("source directory %q: %w", name, err)
		}
		loaded, err := loadNestedSourceDirectory(directory, name)
		if err != nil {
			return err
		}
		corpus.Loaded = append(corpus.Loaded, loaded)
	}
	entries, err := os.ReadDir(corpus.Root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !declared[entry.Name()] {
			return fmt.Errorf("undeclared entry in multi-source corpus: %s", entry.Name())
		}
	}
	return nil
}

func loadNestedSourceDirectory(directory, expectedID string) (LoadedDirectorySource, error) {
	data, recognized, err := identifiedManifest(filepath.Join(directory, "manifest.json"), SourceDirectoryKind)
	if err != nil {
		return LoadedDirectorySource{}, err
	}
	if !recognized {
		return LoadedDirectorySource{}, fmt.Errorf("source directory %q has no %s manifest", expectedID, SourceDirectoryKind)
	}
	var wire sourceDirectoryWire
	if err := decodeStrictJSON(data, &wire); err != nil {
		return LoadedDirectorySource{}, fmt.Errorf("source directory %q: %w", expectedID, err)
	}
	if wire.Schema != SourceDirectorySchema {
		return LoadedDirectorySource{}, fmt.Errorf("source directory %q has unsupported schema %d", expectedID, wire.Schema)
	}
	if wire.Source.ID == "" {
		wire.Source.ID = expectedID
	}
	if wire.Source.ID != expectedID {
		return LoadedDirectorySource{}, fmt.Errorf("source directory %q declares id %q", expectedID, wire.Source.ID)
	}
	if err := validateDirectorySource(wire.Source); err != nil {
		return LoadedDirectorySource{}, fmt.Errorf("source %q: %w", expectedID, err)
	}
	files, err := sourceBoundaryFiles(directory)
	if err != nil {
		return LoadedDirectorySource{}, fmt.Errorf("source %q: %w", expectedID, err)
	}
	return LoadedDirectorySource{Directory: directory, Source: wire.Source, Raw: wire.Raw, Files: files}, nil
}

func validateDirectorySource(source DirectorySource) error {
	if strings.TrimSpace(source.Input.Format) == "" {
		return fmt.Errorf("input.format is required")
	}
	return validateSourceDirectorySource(SourceDirectorySource{
		ID: source.ID, License: source.License, Source: source.Source, Input: source.Input, Artifacts: source.Artifacts,
	})
}

func sourceBoundaryFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("source entry is a symlink: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("source entry is not a regular file: %s", path)
		}
		if entry.Name() == "manifest.json" {
			if filepath.Dir(path) == root {
				return nil
			}
			boundary, err := isWALDOBoundaryManifest(path, info.Size())
			if err != nil {
				return err
			}
			if boundary {
				return fmt.Errorf("conflicting nested WALDO manifest: %s", path)
			}
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("source directory contains no raw files: %s", root)
	}
	return files, nil
}

func isWALDOBoundaryManifest(path string, size int64) (bool, error) {
	if size > sourceManifestMaximum {
		return false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var header struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(data, &header) != nil {
		return false, nil
	}
	return header.Kind == CorpusDirectoryKind || header.Kind == SourceDirectoryKind, nil
}

func regularDirectory(path string) (string, bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return abs, info.IsDir() && info.Mode()&os.ModeSymlink == 0, nil
}

func identifiedManifest(path, kind string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, true, fmt.Errorf("manifest must be a regular non-symlink file: %s", path)
	}
	if info.Size() > sourceManifestMaximum {
		return nil, true, fmt.Errorf("manifest exceeds %d bytes: %s", sourceManifestMaximum, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, true, err
	}
	var header struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, true, fmt.Errorf("malformed manifest %s: %w", path, err)
	}
	return data, header.Kind == kind, nil
}

func decodeStrictJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func (corpus CorpusDirectory) InputPaths() []string {
	var paths []string
	for _, source := range corpus.Loaded {
		paths = append(paths, source.Files...)
	}
	return paths
}

func (corpus CorpusDirectory) Apply(request *PlanRequest) {
	request.Title = corpus.Corpus.Title
	request.Description = corpus.Corpus.Description
	request.Sources = make([]PlanSourceRequest, 0, len(corpus.Loaded))
	for _, loaded := range corpus.Loaded {
		name := loaded.Source.Source.Name
		if name == "" {
			name = loaded.Source.ID
		}
		request.Sources = append(request.Sources, PlanSourceRequest{
			ID: loaded.Source.ID, License: loaded.Source.License,
			Source:    loaded.Source.Source.AsPlanSource(loaded.Source.ID, name),
			InputRoot: loaded.Directory, Profile: loaded.Source.Input,
		})
	}
}

func (corpus CorpusDirectory) VerifyProbe(probe Probe) error {
	if probe.Kind != "waldo-ingest-probe" || probe.Schema != 1 {
		return fmt.Errorf("invalid corpus-directory probe")
	}
	claimed := 0
	for _, source := range corpus.Loaded {
		var entries []Artifact
		for _, artifact := range probe.Artifacts {
			relative, err := filepath.Rel(source.Directory, artifact.Path)
			if err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				entries = append(entries, artifact)
			}
		}
		claimed += len(entries)
		if err := verifyDirectoryRaw(source.Directory, source.Raw, entries); err != nil {
			return fmt.Errorf("source %q: %w", source.Source.ID, err)
		}
	}
	if claimed != len(probe.Artifacts) {
		return fmt.Errorf("probe contains inputs outside source manifest boundaries")
	}
	return nil
}

func verifyDirectoryRaw(root string, expected DirectoryRaw, artifacts []Artifact) error {
	type inventoryEntry struct {
		path   string
		sha256 string
		bytes  int64
	}
	entries := make([]inventoryEntry, 0, len(artifacts))
	var totalBytes int64
	for _, artifact := range artifacts {
		relative, err := filepath.Rel(root, artifact.Path)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("input %s is outside source directory", artifact.Path)
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
	actual := fmt.Sprintf("%x", hash.Sum(nil))
	if int64(len(entries)) != expected.FileCount || totalBytes != expected.ByteCount || actual != expected.TreeSHA256 {
		return fmt.Errorf("raw tree does not match manifest: files %d/%d, bytes %d/%d, sha256 %s/%s",
			len(entries), expected.FileCount, totalBytes, expected.ByteCount, actual, expected.TreeSHA256)
	}
	return nil
}
