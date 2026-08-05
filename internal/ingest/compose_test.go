package ingest

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadComposeStrictlyResolvesAndHashesScripts(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fetch.sh")
	writeExecutable(t, script, "#!/bin/sh\n")
	composePath := filepath.Join(root, "example.yaml")
	writeComposeFixture(t, composePath, `
kind: waldo-ingest-compose
schema: 1
title: Example
description: Example composed corpus.
license: CC0-1.0
source:
  name: example-source
  url: https://example.test/data
  category: public-dataset
steps:
  - name: fetch
    run: fetch.sh
    args: [one, two]
`)
	loaded, found, err := LoadCompose(composePath)
	if err != nil {
		t.Fatal(err)
	}
	if !found || loaded.Compose.Title != "Example" || len(loaded.Scripts) != 1 || len(loaded.SHA256) != 64 || len(loaded.Scripts[0].SHA256) != 64 {
		t.Fatalf("loaded = %+v, found = %v", loaded, found)
	}
	if loaded.Scripts[0].Path != script || strings.Join(loaded.Scripts[0].Args, ",") != "one,two" {
		t.Fatalf("script = %+v", loaded.Scripts[0])
	}

	ordinary := filepath.Join(root, "ordinary.txt")
	if err := os.WriteFile(ordinary, []byte("ordinary training text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, found, err := LoadCompose(ordinary); err != nil || found {
		t.Fatalf("ordinary LoadCompose() found=%v err=%v", found, err)
	}
}

func TestLoadComposeRejectsUnknownFieldsAndNonExecutableScripts(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fetch.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	composePath := filepath.Join(root, "example.yaml")
	writeComposeFixture(t, composePath, `
kind: waldo-ingest-compose
schema: 1
title: Example
license: CC0-1.0
source: {url: https://example.test, category: public-dataset}
steps: [{name: fetch, run: fetch.sh}]
`)
	if _, found, err := LoadCompose(composePath); !found || err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("non-executable LoadCompose() found=%v err=%v", found, err)
	}
	writeExecutable(t, script, "#!/bin/sh\n")
	writeComposeFixture(t, composePath, `
kind: waldo-ingest-compose
schema: 1
title: Example
license: CC0-1.0
unknown: true
source: {url: https://example.test, category: public-dataset}
steps: [{name: fetch, run: fetch.sh}]
`)
	if _, found, err := LoadCompose(composePath); !found || err == nil || !strings.Contains(err.Error(), "field unknown") {
		t.Fatalf("unknown-field LoadCompose() found=%v err=%v", found, err)
	}
}

func TestPrepareComposeReusesVerifiedOutputAndPurges(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fetch.sh")
	writeExecutable(t, script, "#!/bin/sh\n")
	composePath := filepath.Join(root, "example.yaml")
	writeComposeFixture(t, composePath, `
kind: waldo-ingest-compose
schema: 1
title: Example
license: CC0-1.0
source: {url: https://example.test, category: public-dataset}
steps: [{name: fetch, run: fetch.sh}]
`)
	loaded, _, err := LoadCompose(composePath)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fixtureComposeRunner{}
	prepared, err := PrepareCompose(context.Background(), loaded, "core/example", t.TempDir(), runner, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 || prepared.Probe.Totals.Artifacts != 1 || prepared.Probe.Artifacts[0].Format != "text" {
		t.Fatalf("runner=%+v prepared=%+v", runner, prepared)
	}
	resumed, err := PrepareCompose(context.Background(), loaded, "core/example", filepath.Dir(filepath.Dir(prepared.Workspace)), runner, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 || resumed.Workspace != prepared.Workspace {
		t.Fatalf("prepared compose was not reused: calls=%d resumed=%+v", runner.calls, resumed)
	}
	if err := os.WriteFile(filepath.Join(prepared.Inputs, "document.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareCompose(context.Background(), loaded, "core/example", filepath.Dir(filepath.Dir(prepared.Workspace)), runner, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("changed prepared output error = %v", err)
	}
	if err := PurgePreparedCompose(prepared); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(prepared.Workspace); !os.IsNotExist(err) {
		t.Fatalf("workspace remains after purge: %v", err)
	}
}

func TestPrepareComposeExecutesScriptInTemporaryInputDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fetcher contract requires a POSIX execution environment")
	}
	root := t.TempDir()
	script := filepath.Join(root, "fetch.sh")
	writeExecutable(t, script, "#!/bin/sh\nset -eu\nprintf 'from executable fetcher\\n' > \"$WALDO_FETCH_DIR/fetched.txt\"\n")
	composePath := filepath.Join(root, "example.yaml")
	writeComposeFixture(t, composePath, `
kind: waldo-ingest-compose
schema: 1
title: Example
license: CC0-1.0
source: {url: https://example.test, category: public-dataset}
steps: [{name: fetch, run: fetch.sh}]
`)
	loaded, _, err := LoadCompose(composePath)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareCompose(context.Background(), loaded, "core/example", t.TempDir(), ExecCommandRunner{}, io.Discard, io.Discard)
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

type fixtureComposeRunner struct {
	calls int
}

func (runner *fixtureComposeRunner) Run(_ context.Context, _ string, _ []string, directory string, environment []string, _, _ io.Writer) error {
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
	return os.WriteFile(filepath.Join(directory, "document.txt"), []byte("composed training text"), 0o644)
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeComposeFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
