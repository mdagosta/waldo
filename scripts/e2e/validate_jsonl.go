package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
)

type exportedRecord struct {
	SHA256     string `json:"sha256"`
	Kind       string `json:"kind"`
	Text       string `json:"text"`
	Source     string `json:"source"`
	SourceName string `json:"source_name"`
	License    string `json:"license"`
}

type manifestRecord struct {
	Kind    string `json:"kind"`
	Schema  int    `json:"schema"`
	Name    string `json:"name"`
	License string `json:"license"`
	Sources []struct {
		Name  string `json:"name"`
		URL   string `json:"url"`
		Files []struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	} `json:"sources"`
}

func main() {
	if len(os.Args) < 8 {
		fatalf("usage: validate_jsonl <jsonl> <manifest> <source-url> <source-name> <license> <input-directory> <expected-export-file...>")
	}
	jsonl, manifestPath := os.Args[1], os.Args[2]
	sourceURL, sourceName, license, inputDirectory := os.Args[3], os.Args[4], os.Args[5], os.Args[6]
	expectedPaths := os.Args[7:]
	validateManifest(manifestPath, inputDirectory, sourceURL, sourceName, license)
	input, err := os.Open(jsonl)
	if err != nil {
		fatalf("open export: %v", err)
	}
	defer input.Close()
	decoder := json.NewDecoder(input)
	for position, expectedPath := range expectedPaths {
		expected, err := os.ReadFile(expectedPath)
		if err != nil {
			fatalf("read expected file %s: %v", expectedPath, err)
		}
		var actual exportedRecord
		if err := decoder.Decode(&actual); err != nil {
			fatalf("decode exported record %d: %v", position+1, err)
		}
		digest := sha256.Sum256(expected)
		wantHash := hex.EncodeToString(digest[:])
		wantSource := "sha256:" + wantHash
		if actual.Text != string(expected) {
			fatalf("record %d text differs from %s", position+1, expectedPath)
		}
		if actual.SHA256 != wantHash {
			fatalf("record %d sha256 is %s, want %s", position+1, actual.SHA256, wantHash)
		}
		if actual.Kind != "pretrain" || actual.Source != wantSource || actual.SourceName != sourceName || actual.License != license {
			fatalf("record %d metadata is kind=%q source=%q source_name=%q license=%q", position+1, actual.Kind, actual.Source, actual.SourceName, actual.License)
		}
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			fatalf("export contains more than %d unique records", len(expectedPaths))
		}
		fatalf("read after expected records: %v", err)
	}
	fmt.Printf("validated %d exported records byte-for-byte\n", len(expectedPaths))
}

func validateManifest(path, inputDirectory, sourceURL, sourceName, license string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fatalf("read manifest: %v", err)
	}
	var manifest manifestRecord
	if err := json.Unmarshal(data, &manifest); err != nil {
		fatalf("decode manifest: %v", err)
	}
	if manifest.Kind != "manifest" || manifest.Schema != 1 || manifest.Name != "tiny" || manifest.License != license || len(manifest.Sources) != 1 {
		fatalf("manifest identity or license is incorrect")
	}
	source := manifest.Sources[0]
	if source.Name != sourceName || source.URL != sourceURL {
		fatalf("manifest source is name=%q url=%q", source.Name, source.URL)
	}
	entries, err := os.ReadDir(inputDirectory)
	if err != nil {
		fatalf("read input directory: %v", err)
	}
	expectedFiles := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			contents, err := os.ReadFile(filepath.Join(inputDirectory, entry.Name()))
			if err != nil {
				fatalf("read input artifact %s: %v", entry.Name(), err)
			}
			digest := sha256.Sum256(contents)
			expectedFiles = append(expectedFiles, entry.Name()+"="+hex.EncodeToString(digest[:]))
		}
	}
	actualFiles := make([]string, 0, len(source.Files))
	for _, file := range source.Files {
		actualFiles = append(actualFiles, file.Name+"="+file.SHA256)
	}
	sort.Strings(expectedFiles)
	sort.Strings(actualFiles)
	if !slices.Equal(actualFiles, expectedFiles) {
		fatalf("manifest source files are %v, want %v", actualFiles, expectedFiles)
	}
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "validate_jsonl: "+format+"\n", arguments...)
	os.Exit(1)
}
