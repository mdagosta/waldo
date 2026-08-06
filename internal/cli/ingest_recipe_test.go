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

	"github.com/openwaldo/waldo/internal/config"
	waldoindex "github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/ingest"
	"github.com/openwaldo/waldo/internal/lookaside"
)

func TestIndexIngestRecipeDryRunDoesNotExecuteCommands(t *testing.T) {
	recipePath := writeCLIRecipe(t)
	root := emptyCLIIndex(t)
	runner := &cliRecipeRunner{}
	originalRunner := ingestRecipeRunner
	ingestRecipeRunner = runner
	t.Cleanup(func() { ingestRecipeRunner = originalRunner })
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "absent.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{"index", "ingest", recipePath, filepath.Join(root, "core", "recipe-corpus"), "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if runner.calls != 0 || !strings.Contains(stdout.String(), "no commands were executed") || !strings.Contains(stdout.String(), "fetch ->") {
		t.Fatalf("runner calls=%d stdout=%q", runner.calls, stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"index", "ingest", recipePath, filepath.Join(root, "core", "recipe-corpus"), "--title", "Override", "--dry-run"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "recipe input owns corpus metadata") {
		t.Fatalf("override code=%d stderr=%q", code, stderr.String())
	}
}

func TestIndexIngestRecipePublishesAuditableManifestAndPurgesInputs(t *testing.T) {
	recipePath := writeCLIRecipe(t)
	root := emptyCLIIndex(t)
	staging := t.TempDir()
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := config.Save(config.Config{
		Lookaside: config.Lookaside{Scratch: t.TempDir(), Publish: &config.Publish{URL: "s3://openwaldo/lookaside/v1", Workers: 2}},
		Ingest:    config.Ingest{Staging: staging},
	}); err != nil {
		t.Fatal(err)
	}
	runner := &cliRecipeRunner{}
	originalRunner := ingestRecipeRunner
	ingestRecipeRunner = runner
	t.Cleanup(func() { ingestRecipeRunner = originalRunner })
	remote := &cliPublisher{}
	originalPublisher := newIngestPublisher
	newIngestPublisher = func(context.Context, config.Publish) (lookaside.Publisher, error) { return remote, nil }
	t.Cleanup(func() { newIngestPublisher = originalPublisher })

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--json", "index", "ingest", recipePath, filepath.Join(root, "core", "recipe-corpus")}, &stdout, &stderr)
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
	manifest, err := waldoindex.LoadManifest(filepath.Join(output.Contribution.Root, "core", "recipe-corpus", "recipe-corpus.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ComposedBy != nil || !strings.Contains(manifest.ConvertedBy.Collector, ":recipe.yaml#sha256=") {
		t.Fatalf("collector = %q, composed_by = %+v", manifest.ConvertedBy.Collector, manifest.ComposedBy)
	}
	if len(manifest.Sources) != 1 || len(manifest.Sources[0].Files) != 0 {
		t.Fatalf("sources = %+v", manifest.Sources)
	}
	if len(manifest.Shards) != 1 || manifest.Shards[0].Docs != 1 || manifest.Shards[0].Tokens <= 0 {
		t.Fatalf("shards = %+v", manifest.Shards)
	}
	recipeWorkspaces, err := os.ReadDir(filepath.Join(staging, "recipes"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(recipeWorkspaces) != 0 {
		t.Fatalf("recipe workspaces were not purged: %v", recipeWorkspaces)
	}
}

type cliRecipeRunner struct {
	calls int
}

func (runner *cliRecipeRunner) Run(_ context.Context, _ string, _ []string, directory string, _ []string, _, _ io.Writer) error {
	runner.calls++
	return os.WriteFile(filepath.Join(directory, "document.txt"), []byte("recipe corpus document"), 0o644)
}

func writeCLIRecipe(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	script := filepath.Join(root, "fetch.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	recipePath := filepath.Join(root, "recipe.yaml")
	contents := `kind: waldo-ingest-recipe
schema: 1
title: Recipe Corpus
description: Produced by a reviewed ingest recipe.
license: CC0-1.0
source:
  name: recipe-source
  url: https://example.test/recipe
  category: public-dataset
steps:
  - name: fetch
    exec: ./fetch.sh
    args: [fixture]
`
	if err := os.WriteFile(recipePath, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return recipePath
}
