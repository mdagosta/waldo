// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/openwaldo/waldo/internal/config"
	waldoindex "github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/ingest"
)

func TestIndexIngestUpdateRebuildsFromAuthoritativeInput(t *testing.T) {
	root := emptyCLIIndex(t)
	lookasideRoot := t.TempDir()
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := config.Save(config.Config{
		Index: root,
		Lookaside: config.Lookaside{
			Scratch: t.TempDir(), Cache: t.TempDir(),
			Publish: &config.Publish{URL: (&url.URL{Scheme: "file", Path: filepath.ToSlash(lookasideRoot)}).String(), Workers: 2},
		},
		Ingest: config.Ingest{Staging: t.TempDir()},
	}); err != nil {
		t.Fatal(err)
	}
	initial := filepath.Join(t.TempDir(), "initial.txt")
	if err := os.WriteFile(initial, []byte("already indexed"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--json", "index", "ingest", initial, filepath.Join(root, "core", "example"), "--title", "Example", "--description", "Example corpus.", "--license", "CC0-1.0", "--source", "https://example.test/data", "--source-category", "public-dataset"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("ingest code=%d stderr=%q", code, stderr.String())
	}
	var created struct {
		Contribution ingest.ContributionResult `json:"contribution"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	applyTestContribution(t, root, created.Contribution)

	updateInput := t.TempDir()
	if err := os.WriteFile(filepath.Join(updateInput, "old.txt"), []byte("already indexed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(updateInput, "new.txt"), []byte("genuinely new record"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	manifestPath := filepath.Join(root, "core", "example", "example.yaml")
	code = Run([]string{"--json", "index", "ingest", updateInput, manifestPath, "--update", "--title", "Example", "--description", "Example corpus.", "--license", "CC0-1.0", "--source", "https://example.test/data", "--source-category", "public-dataset"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("update code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var updated struct {
		Assembly     ingest.AssemblyResult     `json:"assembly"`
		Contribution ingest.ContributionResult `json:"contribution"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Assembly.InputDocs != 2 || updated.Assembly.RetainedDocs != 2 || updated.Assembly.DuplicateDocs != 0 || len(updated.Assembly.Objects) != 1 {
		t.Fatalf("assembly = %+v", updated.Assembly)
	}
	manifest, err := waldoindex.LoadManifest(filepath.Join(updated.Contribution.Root, "core", "example", "example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Shards) != 1 || len(manifest.Sources) != 1 || manifest.Shards[0].Docs != 2 {
		t.Fatalf("updated manifest = %+v", manifest)
	}
}

func TestIndexIngestUpdateRebuildUsesFetcherHandoffManifest(t *testing.T) {
	handoff := t.TempDir()
	raw := filepath.Join(handoff, "raw")
	if err := os.MkdirAll(raw, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("Replacement training text.\n")
	if err := os.WriteFile(filepath.Join(raw, "document.txt"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	fileHash := sha256.Sum256(content)
	inventory := fmt.Sprintf("%x\t%d\tdocument.txt\n", fileHash, len(content))
	treeHash := sha256.Sum256([]byte(inventory))
	manifest := fmt.Sprintf(`{
  "kind":"waldo-source-directory","schema":1,
  "corpus":{"id":"books","title":"Replacement Books","description":"Fetcher handoff."},
  "sources":[{"id":"books","path":"","license":"CC0-1.0","source":{"name":"Example","url":"https://example.test/data","category":"public-dataset","license_evidence":{"declaration":"CC0-1.0"}},"input":{"format":"text"},"artifacts":[]}],
  "fetcher":{},"raw":{"path":"raw","file_count":1,"byte_count":%d,"tree_sha256":"%x"}
}`, len(content), treeHash)
	if err := os.WriteFile(filepath.Join(handoff, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	root := fixtureCLIIndex(t)
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := config.Save(config.Config{Index: root}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--json", "index", "ingest", handoff, filepath.Join(root, "books", "books.json"), "--update", "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("update code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var output struct {
		Plan ingest.Plan `json:"plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Plan.Title != "Replacement Books" || output.Plan.Update == nil || output.Plan.Update.Mode != "rebuild-shards" || len(output.Plan.Inputs) != 1 {
		t.Fatalf("update plan = %+v", output.Plan)
	}
}

func applyTestContribution(t *testing.T, root string, contribution ingest.ContributionResult) {
	t.Helper()
	for _, relative := range contribution.Files {
		data, err := os.ReadFile(filepath.Join(contribution.Root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, relative := range contribution.Removed {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(relative))); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}
