package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo-new/internal/config"
	waldoindex "github.com/openwaldo/waldo-new/internal/index"
	"github.com/openwaldo/waldo-new/internal/ingest"
	"github.com/openwaldo/waldo-new/internal/lookaside"
)

func TestIndexIngestComposeDryRunDoesNotExecuteScripts(t *testing.T) {
	composePath := writeCLICompose(t)
	root := emptyCLIIndex(t)
	runner := &cliComposeRunner{}
	originalRunner := ingestComposeRunner
	ingestComposeRunner = runner
	t.Cleanup(func() { ingestComposeRunner = originalRunner })
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "absent.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{"index", "ingest", composePath, filepath.Join(root, "core", "composed"), "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if runner.calls != 0 || !strings.Contains(stdout.String(), "no scripts were executed") || !strings.Contains(stdout.String(), "fetch ->") {
		t.Fatalf("runner calls=%d stdout=%q", runner.calls, stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"index", "ingest", composePath, filepath.Join(root, "core", "composed"), "--title", "Override", "--dry-run"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "compose input owns corpus metadata") {
		t.Fatalf("override code=%d stderr=%q", code, stderr.String())
	}
}

func TestIndexIngestComposePublishesAuditableManifestAndPurgesInputs(t *testing.T) {
	composePath := writeCLICompose(t)
	root := emptyCLIIndex(t)
	staging := t.TempDir()
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := config.Save(config.Config{
		Lookaside: config.Lookaside{Scratch: t.TempDir(), Publish: &config.Publish{URL: "s3://openwaldo/lookaside/v1", Workers: 2}},
		Ingest:    config.Ingest{Staging: staging},
	}); err != nil {
		t.Fatal(err)
	}
	runner := &cliComposeRunner{}
	originalRunner := ingestComposeRunner
	ingestComposeRunner = runner
	t.Cleanup(func() { ingestComposeRunner = originalRunner })
	remote := &cliPublisher{}
	originalPublisher := newIngestPublisher
	newIngestPublisher = func(context.Context, config.Publish) (lookaside.Publisher, error) { return remote, nil }
	t.Cleanup(func() { newIngestPublisher = originalPublisher })

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--json", "index", "ingest", composePath, filepath.Join(root, "core", "composed")}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d", runner.calls)
	}
	var output struct {
		Contribution ingest.ContributionResult `json:"contribution"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	manifestData, err := os.ReadFile(filepath.Join(output.Contribution.Root, "core", "composed", "composed.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest waldoindex.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ComposedBy != nil || !strings.Contains(manifest.ConvertedBy.Collector, ":composed.yaml#sha256=") {
		t.Fatalf("collector = %q, composed_by = %+v", manifest.ConvertedBy.Collector, manifest.ComposedBy)
	}
	if len(manifest.Sources) != 1 || len(manifest.Sources[0].Files) != 0 {
		t.Fatalf("sources = %+v", manifest.Sources)
	}
	if len(manifest.Shards) != 1 || manifest.Shards[0].Docs != 1 || manifest.Shards[0].Tokens <= 0 {
		t.Fatalf("shards = %+v", manifest.Shards)
	}
	composeWorkspaces, err := os.ReadDir(filepath.Join(staging, "composes"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(composeWorkspaces) != 0 {
		t.Fatalf("compose workspaces were not purged: %v", composeWorkspaces)
	}
}

type cliComposeRunner struct {
	calls int
}

func (runner *cliComposeRunner) Run(_ context.Context, _ string, _ []string, directory string, _ []string, _, _ io.Writer) error {
	runner.calls++
	return os.WriteFile(filepath.Join(directory, "document.txt"), []byte("composed corpus document"), 0o644)
}

func writeCLICompose(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	script := filepath.Join(root, "fetch.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	composePath := filepath.Join(root, "composed.yaml")
	contents := `kind: waldo-ingest-compose
schema: 1
title: Composed Corpus
description: Produced by a reviewed fetch compose.
license: CC0-1.0
source:
  name: composed-source
  url: https://example.test/composed
  category: public-dataset
steps:
  - name: fetch
    run: fetch.sh
    args: [fixture]
`
	if err := os.WriteFile(composePath, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return composePath
}
