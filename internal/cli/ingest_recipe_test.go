// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

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

func TestIndexUpdateRecipeRebuildsWithoutReadingOldShardAndMigratesYAML(t *testing.T) {
	recipePath := writeCLIProfileRecipe(t)
	root := t.TempDir()
	writeCLIFile(t, filepath.Join(root, "index.json"), `{"kind":"index","schema":1,"path":"","entries":[{"name":"core","type":"dir"}]}`)
	writeCLIFile(t, filepath.Join(root, "core", "index.json"), `{"kind":"index","schema":1,"path":"core","entries":[{"name":"example.json","type":"manifest"}]}`)
	writeCLIFile(t, filepath.Join(root, "core", "example.json"), `{
  "kind":"manifest","schema":1,"name":"example","title":"Old Example","description":"Old corpus.","license":"CC0-1.0",
  "sources":[{"name":"old","source":"Old","url":"https://unreachable.example/raw","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}],
  "converted_by":{"tool":"old","version":"1","profile":"text","recipe":"old/v1","tokenizer":"byte"},
  "record_schema":1,
  "shards":[{"url":"https://unreachable.example/object","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","sources":["old"],"docs":99,"tokens":999,"bytes":123456}]
}`)
	staging := t.TempDir()
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := config.Save(config.Config{
		Index:     root,
		Lookaside: config.Lookaside{Scratch: t.TempDir(), Publish: &config.Publish{URL: "s3://openwaldo/lookaside/v1", Workers: 2}},
		Ingest:    config.Ingest{Staging: staging},
	}); err != nil {
		t.Fatal(err)
	}
	runner := &cliRecipeRunner{
		outputName: "dialogue.jsonl",
		outputData: []byte(`{"prompt":"How are you?","reply":"Well, thank you."}` + "\n"),
	}
	originalRunner := ingestRecipeRunner
	ingestRecipeRunner = runner
	t.Cleanup(func() { ingestRecipeRunner = originalRunner })
	remote := &cliPublisher{}
	originalPublisher := newIngestPublisher
	newIngestPublisher = func(context.Context, config.Publish) (lookaside.Publisher, error) { return remote, nil }
	t.Cleanup(func() { newIngestPublisher = originalPublisher })

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--json", "index", "update", recipePath, filepath.Join(root, "core", "example.json"), "--rebuild-shards"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var output struct {
		Assembly     ingest.AssemblyResult     `json:"assembly"`
		Contribution ingest.ContributionResult `json:"contribution"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Assembly.RetainedDocs != 1 || len(output.Contribution.Files) != 2 || !containsString(output.Contribution.Removed, "core/example.json") || !containsString(output.Contribution.Removed, "core/index.json") {
		t.Fatalf("output = %+v", output)
	}
	manifest, err := waldoindex.LoadManifest(filepath.Join(output.Contribution.Root, "core", "example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "example" || manifest.Title != "Recipe Corpus" || len(manifest.Shards) != 1 || manifest.Shards[0].Docs != 1 || manifest.Shards[0].SHA256 == strings.Repeat("b", 64) {
		t.Fatalf("rebuilt manifest = %+v", manifest)
	}
	var state ingest.RecipeUpdateState
	if err := json.Unmarshal(runner.updateState, &state); err != nil {
		t.Fatal(err)
	}
	if state.Mode != "rebuild-shards" || state.Manifest != "core/example.json" || state.Shards != 1 || state.Docs != 99 || len(state.Sources) != 1 {
		t.Fatalf("update state = %+v", state)
	}
}

type cliRecipeRunner struct {
	calls       int
	updateState []byte
	outputName  string
	outputData  []byte
}

func (runner *cliRecipeRunner) Run(_ context.Context, _ string, _ []string, directory string, environment []string, _, _ io.Writer) error {
	runner.calls++
	for _, value := range environment {
		if strings.HasPrefix(value, "WALDO_UPDATE_STATE=") {
			data, err := os.ReadFile(strings.TrimPrefix(value, "WALDO_UPDATE_STATE="))
			if err != nil {
				return err
			}
			runner.updateState = data
		}
	}
	name := runner.outputName
	data := runner.outputData
	if name == "" {
		name = "document.txt"
		data = []byte("recipe corpus document")
	}
	return os.WriteFile(filepath.Join(directory, name), data, 0o644)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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

func writeCLIProfileRecipe(t *testing.T) string {
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
input:
  type: dialogue-pair
  fields:
    text: [prompt]
    response: reply
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
