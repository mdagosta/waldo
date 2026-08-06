package ingest

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
input:
  type: record-map
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
	if loaded.Recipe.Input.Type != ProfileRecordMap || len(loaded.Recipe.Input.Fields.Text) != 2 {
		t.Fatalf("input = %+v", loaded.Recipe.Input)
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
