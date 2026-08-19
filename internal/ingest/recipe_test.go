// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/index"
)

func TestLoadRecipeStrictlyResolvesAndHashesExecutables(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fetch.sh")
	writeExecutable(t, script, "#!/bin/sh\n")
	recipePath := filepath.Join(root, "example.yaml")
	writeRecipeFixture(t, recipePath, `
kind: waldo-ingest-recipe
schema: 1
title: Example
description: Example recipe corpus.
license: CC0-1.0
source:
  name: example-source
  url: https://example.test/data
  category: public-dataset
steps:
  - name: fetch
    exec: ./fetch.sh
    args: [one, two]
`)
	loaded, found, err := LoadRecipe(recipePath)
	if err != nil {
		t.Fatal(err)
	}
	if !found || loaded.Recipe.Title != "Example" || len(loaded.Executables) != 1 || len(loaded.SHA256) != 64 || len(loaded.Executables[0].SHA256) != 64 {
		t.Fatalf("loaded = %+v, found = %v", loaded, found)
	}
	if loaded.Executables[0].Path != script || strings.Join(loaded.Executables[0].Args, ",") != "one,two" {
		t.Fatalf("executable = %+v", loaded.Executables[0])
	}

	ordinary := filepath.Join(root, "ordinary.txt")
	if err := os.WriteFile(ordinary, []byte("ordinary training text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, found, err := LoadRecipe(ordinary); err != nil || found {
		t.Fatalf("ordinary LoadRecipe() found=%v err=%v", found, err)
	}
}

func TestLoadRecipeAcceptsDeclarativeInputProfile(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fetch.sh")
	writeExecutable(t, script, "#!/bin/sh\n")
	recipePath := filepath.Join(root, "example.yaml")
	writeRecipeFixture(t, recipePath, `
kind: waldo-ingest-recipe
schema: 1
title: Cases
license: CC0-1.0
source: {url: https://example.test, category: public-dataset}
record_maximum_bytes: 134217728
input:
  type: record-map
  nul: space
  fields:
    text: [casebody.head_matter, "casebody.opinions[].text"]
    id: id
    license: metadata.license
steps: [{name: fetch, exec: ./fetch.sh}]
`)
	loaded, found, err := LoadRecipe(recipePath)
	if err != nil || !found {
		t.Fatalf("LoadRecipe() found=%v err=%v", found, err)
	}
	if loaded.Recipe.Input.Type != ProfileRecordMap || loaded.Recipe.Input.NUL != "space" || len(loaded.Recipe.Input.Fields.Text) != 2 || loaded.Recipe.RecordMaximumBytes != 128<<20 {
		t.Fatalf("input = %+v", loaded.Recipe.Input)
	}
}

func TestLoadSchemaOneRecipePreservesGeneralSourceEvidence(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fetch.sh")
	writeExecutable(t, script, "#!/bin/sh\n")
	recipePath := filepath.Join(root, "dataset.yaml")
	writeRecipeFixture(t, recipePath, `
kind: waldo-ingest-recipe
schema: 1
title: Dataset
license: Apache-2.0
source:
  name: example
  version: 0123456789abcdef
  url: https://huggingface.co/datasets/example/data
  category: public-dataset
  collected_from: 2026-08-08T10:00:00Z
  collected_to: 2026-08-08T10:05:00Z
  license_evidence:
    declaration: Apache License 2.0
    url: https://huggingface.co/datasets/example/data/blob/0123456789abcdef/README.md
  content:
    types: [instruction-response text]
    languages: [en]
    from: 2020
    to: 2025-12
    selection: Pinned train split in upstream source order.
    copyrighted: unknown
  acquisition:
    basis: Public dataset release at the pinned revision.
steps: [{name: fetch, exec: ./fetch.sh}]
`)
	loaded, found, err := LoadRecipe(recipePath)
	if err != nil || !found {
		t.Fatalf("LoadRecipe() found=%v err=%v", found, err)
	}
	source := loaded.Recipe.Source
	if source.LicenseEvidence == nil || source.LicenseEvidence.Declaration != "Apache License 2.0" || source.Content == nil || source.Content.Selection == "" || source.Acquisition == nil || source.Acquisition.Basis == "" {
		t.Fatalf("source evidence = %+v", source)
	}
	prepared, err := PrepareRecipe(context.Background(), loaded, "core/dataset", t.TempDir(), &fixtureRecipeRunner{}, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	recipeEvidence := loaded.Evidence
	plan, err := NewPlan(prepared.Probe, PlanRequest{
		Destination: "core/dataset", Title: loaded.Recipe.Title, License: loaded.Recipe.License,
		Source: source.AsPlanSource("", source.Name), InputRoot: prepared.Inputs, RecipeEvidence: &recipeEvidence,
	})
	if err != nil {
		t.Fatal(err)
	}
	assembly, err := AssembleTextObjects(context.Background(), plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildManifest(plan, assembly, "https://example.test/objects")
	if err != nil {
		t.Fatal(err)
	}
	if persisted := manifest.Sources[0]; persisted.LicenseEvidence == nil || persisted.Content == nil || persisted.Acquisition == nil || persisted.Content.Selection != source.Content.Selection {
		t.Fatalf("persisted source evidence = %+v", persisted)
	}
}

func TestLoadRecipeRejectsUnknownNestedSourceEvidence(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fetch.sh")
	writeExecutable(t, script, "#!/bin/sh\n")
	for name, recipe := range map[string]string{
		"schema-1": `
kind: waldo-ingest-recipe
schema: 1
title: Invalid
license: CC0-1.0
source:
  url: https://example.test
  category: public-dataset
  content:
    types: [text]
    corpus_specific_guess: forbidden
steps: [{name: fetch, exec: ./fetch.sh}]
		`,
		"schema-2": `
kind: waldo-ingest-recipe
schema: 2
title: Invalid
sources:
  - id: example
    license: CC0-1.0
    source:
      url: https://example.test
      category: public-dataset
      license_evidence:
        declaration: Public domain
        corpus_specific_guess: forbidden
    steps: [{name: fetch, exec: ./fetch.sh}]
		`,
	} {
		t.Run(name, func(t *testing.T) {
			recipePath := filepath.Join(root, name+".yaml")
			writeRecipeFixture(t, recipePath, recipe)
			if _, found, err := LoadRecipe(recipePath); !found || err == nil || !strings.Contains(err.Error(), "field corpus_specific_guess not found") {
				t.Fatalf("LoadRecipe() found=%v err=%v", found, err)
			}
		})
	}
}

func TestMultiSourceRecipeCombinesProjectsAndLicensesInOneShard(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fetch.sh")
	writeExecutable(t, script, "#!/bin/sh\n")
	recipePath := filepath.Join(root, "code.yaml")
	writeRecipeFixture(t, recipePath, `
kind: waldo-ingest-recipe
schema: 2
title: Open Source Code
sources:
  - id: linux
    license: GPL-2.0-only
    source:
      name: linux
      url: https://example.test/linux
      category: public-dataset
      collected_to: 2026-08-08
      license_evidence:
        declaration: GNU General Public License version 2 only
        url: https://example.test/linux/COPYING
      content:
        types: [source code]
        languages: [C]
        selection: Tracked C and header files at the pinned revision.
    steps: [{name: fetch, exec: ./fetch.sh}]
  - id: kubernetes
    license: Apache-2.0
    source: {name: kubernetes, url: https://example.test/kubernetes, category: public-dataset}
    steps: [{name: fetch, exec: ./fetch.sh}]
`)
	loaded, found, err := LoadRecipe(recipePath)
	if err != nil || !found {
		t.Fatalf("LoadRecipe() found=%v err=%v", found, err)
	}
	runner := &fixtureRecipeRunner{}
	prepared, err := PrepareRecipe(context.Background(), loaded, "code/open-source", t.TempDir(), runner, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(prepared.Probe, PlanRequest{
		Destination: "code/open-source", Title: loaded.Recipe.Title,
		Description: "Multiple projects.", Sources: prepared.SourceRequests(),
	})
	if err != nil {
		t.Fatal(err)
	}
	assembly, err := AssembleTextObjects(context.Background(), plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(assembly.Objects) != 1 || strings.Join(assembly.Objects[0].Sources, ",") != "kubernetes,linux" || strings.Join(assembly.Objects[0].Licenses, ",") != "Apache-2.0,GPL-2.0-only" {
		t.Fatalf("assembly = %+v", assembly)
	}
	manifest, err := BuildManifest(plan, assembly, "s3://openwaldo/lookaside/v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Sources) != 2 || len(manifest.Shards) != 1 || strings.Join(manifest.Shards[0].Sources, ",") != "kubernetes,linux" || strings.Join(manifest.Shards[0].Licenses, ",") != "Apache-2.0,GPL-2.0-only" {
		t.Fatalf("manifest = %+v", manifest)
	}
	if manifest.Sources[0].LicenseEvidence == nil || manifest.Sources[0].LicenseEvidence.Declaration == "" || manifest.Sources[0].Content == nil || manifest.Sources[0].Content.Selection == "" || manifest.Sources[0].CollectedTo != "2026-08-08" {
		t.Fatalf("manifest source evidence = %+v", manifest.Sources[0])
	}
}

func TestSourceCodeRecipeTreatsJSONArrayJavaScriptAsText(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fixture.js")
	writeFixture(t, path, `["const a = 1;", "const b = 2;"]`)
	probe, err := ProbePaths(context.Background(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if probe.Artifacts[0].Format != "json" {
		t.Fatalf("probe format = %q, want json before source context", probe.Artifacts[0].Format)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "code/example",
		Title:       "Example Code",
		Sources: []PlanSourceRequest{{
			ID: "example", License: "MIT", InputRoot: root,
			Source: PlanSource{Name: "example", URL: "https://example.test", Category: "public-dataset", Content: &index.Content{Types: []string{"source code"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Inputs[0]; got.Adapter != "text" || got.Artifact.Format != "text" || !slices.Contains(got.Artifact.Evidence, "source-code-context") {
		t.Fatalf("planned input = %+v", got)
	}
	if len(plan.TextFallbacks) != 0 {
		t.Fatalf("source-code interpretation was incorrectly recorded as a fallback: %+v", plan.TextFallbacks)
	}
}

func TestRecipeRejectsUnsupportedTextualFormat(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.html")
	writeFixture(t, path, "<!doctype html><html><body>training text</body></html>")
	probe, err := ProbePaths(context.Background(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewPlan(probe, PlanRequest{
		Destination: "web/example", Title: "Example", License: "CC0-1.0",
		Source:         PlanSource{Name: "example", URL: "https://example.test", Category: "public-dataset"},
		RecipeEvidence: &index.IngestRecipeEvidence{Path: "example.yaml", SHA256: strings.Repeat("a", 64), Steps: []index.RecipeStepEvidence{{Name: "fetch", Executable: "fetch.sh", SHA256: strings.Repeat("b", 64)}}},
	})
	if err == nil || !strings.Contains(err.Error(), `unsupported raw format "html"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestRecipePlanSkipsEmptyTrackedFiles(t *testing.T) {
	root := t.TempDir()
	empty := filepath.Join(root, "empty.c")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	content := filepath.Join(root, "content.c")
	writeFixture(t, content, "int main(void) { return 0; }\n")
	probe, err := ProbePaths(context.Background(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "code/example", Title: "Example", License: "MIT",
		Source:         PlanSource{Name: "example", URL: "https://example.test", Category: "public-dataset"},
		RecipeEvidence: &index.IngestRecipeEvidence{Path: "example.yaml", SHA256: strings.Repeat("a", 64), Steps: []index.RecipeStepEvidence{{Name: "fetch", Executable: "git.sh", SHA256: strings.Repeat("b", 64)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Inputs) != 1 || plan.Inputs[0].Artifact.Path != content {
		t.Fatalf("inputs = %+v", plan.Inputs)
	}
}

func TestLoadRecipeRejectsUnknownFieldsAndNonExecutableCommands(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fetch.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recipePath := filepath.Join(root, "example.yaml")
	writeRecipeFixture(t, recipePath, `
kind: waldo-ingest-recipe
schema: 1
title: Example
license: CC0-1.0
source: {url: https://example.test, category: public-dataset}
steps: [{name: fetch, exec: ./fetch.sh}]
`)
	if _, found, err := LoadRecipe(recipePath); !found || err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("non-executable LoadRecipe() found=%v err=%v", found, err)
	}
	writeExecutable(t, script, "#!/bin/sh\n")
	writeRecipeFixture(t, recipePath, `
kind: waldo-ingest-recipe
schema: 1
title: Example
license: CC0-1.0
unknown: true
source: {url: https://example.test, category: public-dataset}
steps: [{name: fetch, exec: ./fetch.sh}]
`)
	if _, found, err := LoadRecipe(recipePath); !found || err == nil || !strings.Contains(err.Error(), "field unknown") {
		t.Fatalf("unknown-field LoadRecipe() found=%v err=%v", found, err)
	}
}

func TestPrepareRecipeReusesVerifiedOutputAndPurges(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fetch.sh")
	writeExecutable(t, script, "#!/bin/sh\n")
	recipePath := filepath.Join(root, "example.yaml")
	writeRecipeFixture(t, recipePath, `
kind: waldo-ingest-recipe
schema: 1
title: Example
license: CC0-1.0
source: {url: https://example.test, category: public-dataset}
steps: [{name: fetch, exec: ./fetch.sh}]
`)
	loaded, _, err := LoadRecipe(recipePath)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fixtureRecipeRunner{}
	prepared, err := PrepareRecipe(context.Background(), loaded, "core/example", t.TempDir(), runner, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 || prepared.Probe.Totals.Artifacts != 1 || prepared.Probe.Artifacts[0].Format != "text" {
		t.Fatalf("runner=%+v prepared=%+v", runner, prepared)
	}
	resumed, err := PrepareRecipe(context.Background(), loaded, "core/example", filepath.Dir(filepath.Dir(prepared.Workspace)), runner, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 || resumed.Workspace != prepared.Workspace {
		t.Fatalf("prepared recipe was not reused: calls=%d resumed=%+v", runner.calls, resumed)
	}
	statePath := filepath.Join(prepared.Workspace, recipeJournal)
	state, exists, err := loadRecipeState(statePath)
	if err != nil || !exists || state.Probe == nil {
		t.Fatalf("load prepared state: exists=%v state=%+v err=%v", exists, state, err)
	}
	state.Probe.Artifacts[0].Format = "unknown"
	state.Probe.Artifacts[0].Evidence = []string{"old-detector"}
	if err := writeRecipeState(statePath, state); err != nil {
		t.Fatal(err)
	}
	refreshed, err := PrepareRecipe(context.Background(), loaded, "core/example", filepath.Dir(filepath.Dir(prepared.Workspace)), runner, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 || refreshed.Probe.Artifacts[0].Format != "text" {
		t.Fatalf("probe metadata was not safely refreshed: calls=%d refreshed=%+v", runner.calls, refreshed)
	}
	if err := os.WriteFile(filepath.Join(prepared.Inputs, "document.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareRecipe(context.Background(), loaded, "core/example", filepath.Dir(filepath.Dir(prepared.Workspace)), runner, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("changed prepared output error = %v", err)
	}
	if err := PurgePreparedRecipe(prepared); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(prepared.Workspace); !os.IsNotExist(err) {
		t.Fatalf("workspace remains after purge: %v", err)
	}
}

func TestPrepareRecipeExecutesCommandInTemporaryInputDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fetcher contract requires a POSIX execution environment")
	}
	root := t.TempDir()
	script := filepath.Join(root, "fetch.sh")
	writeExecutable(t, script, "#!/bin/sh\nset -eu\nprintf 'from executable fetcher\\n' > \"$WALDO_FETCH_DIR/fetched.txt\"\n")
	recipePath := filepath.Join(root, "example.yaml")
	writeRecipeFixture(t, recipePath, `
kind: waldo-ingest-recipe
schema: 1
title: Example
license: CC0-1.0
source: {url: https://example.test, category: public-dataset}
steps: [{name: fetch, exec: ./fetch.sh}]
`)
	loaded, _, err := LoadRecipe(recipePath)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareRecipe(context.Background(), loaded, "core/example", t.TempDir(), ExecCommandRunner{}, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(prepared.Inputs, "fetched.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "from executable fetcher\n" {
		t.Fatalf("fetched output = %q", data)
	}
}

func TestLoadRecipeResolvesBareExecThroughPATH(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(bin, "fixture-fetch")
	writeExecutable(t, command, "#!/bin/sh\nset -eu\nprintf 'from PATH command\\n' > \"$WALDO_FETCH_DIR/path.txt\"\n")
	t.Setenv("PATH", bin)
	recipePath := filepath.Join(root, "example.yaml")
	writeRecipeFixture(t, recipePath, `
kind: waldo-ingest-recipe
schema: 1
title: Example
license: CC0-1.0
source: {url: https://example.test, category: public-dataset}
steps: [{name: fetch, exec: fixture-fetch}]
`)
	loaded, found, err := LoadRecipe(recipePath)
	if err != nil || !found {
		t.Fatalf("LoadRecipe() found=%v err=%v", found, err)
	}
	if loaded.Executables[0].Exec != "fixture-fetch" || loaded.Executables[0].Path != command {
		t.Fatalf("resolved executable = %+v", loaded.Executables[0])
	}
	prepared, err := PrepareRecipe(context.Background(), loaded, "core/example", t.TempDir(), ExecCommandRunner{}, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(prepared.Inputs, "path.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "from PATH command\n" {
		t.Fatalf("PATH command output = %q", data)
	}
}

func TestLoadRecipeRejectsLegacyRunAndMissingPATHCommand(t *testing.T) {
	root := t.TempDir()
	recipePath := filepath.Join(root, "example.yaml")
	base := `
kind: waldo-ingest-recipe
schema: 1
title: Example
license: CC0-1.0
source: {url: https://example.test, category: public-dataset}
steps: [{name: fetch, %s}]
`
	writeRecipeFixture(t, recipePath, fmt.Sprintf(base, "run: fetch.sh"))
	if _, found, err := LoadRecipe(recipePath); !found || err == nil || !strings.Contains(err.Error(), "field run not found") {
		t.Fatalf("legacy run LoadRecipe() found=%v err=%v", found, err)
	}
	t.Setenv("PATH", t.TempDir())
	writeRecipeFixture(t, recipePath, fmt.Sprintf(base, "exec: missing-fetch-command"))
	if _, found, err := LoadRecipe(recipePath); !found || err == nil || !strings.Contains(err.Error(), "not found in PATH") {
		t.Fatalf("missing PATH command LoadRecipe() found=%v err=%v", found, err)
	}
}

func TestLoadRecipeRejectsRetiredComposeIdentity(t *testing.T) {
	root := t.TempDir()
	recipePath := filepath.Join(root, "retired.yaml")
	writeRecipeFixture(t, recipePath, `
kind: waldo-ingest-compose
schema: 1
title: Retired
license: CC0-1.0
source: {url: https://example.test, category: public-dataset}
steps: [{name: fetch, exec: ./fetch.sh}]
`)
	if _, found, err := LoadRecipe(recipePath); !found || err == nil || !strings.Contains(err.Error(), `use "waldo-ingest-recipe"`) {
		t.Fatalf("retired identity LoadRecipe() found=%v err=%v", found, err)
	}
}

type fixtureRecipeRunner struct {
	calls int
}

func (runner *fixtureRecipeRunner) Run(_ context.Context, _ string, _ []string, directory string, environment []string, _, _ io.Writer) error {
	runner.calls++
	foundDirectory := false
	for _, value := range environment {
		if value == "WALDO_FETCH_DIR="+directory {
			foundDirectory = true
		}
	}
	if !foundDirectory {
		return os.ErrInvalid
	}
	return os.WriteFile(filepath.Join(directory, "document.txt"), []byte("recipe training text"), 0o644)
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeRecipeFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
