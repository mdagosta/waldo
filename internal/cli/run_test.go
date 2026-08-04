package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootHelpLocksCommandVocabulary(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
	help := stdout.String()
	for _, want := range []string{"index", "lookaside", "model", "bom", "config"} {
		if !strings.Contains(help, want) {
			t.Errorf("root help does not contain %q:\n%s", want, help)
		}
	}
	for _, unwanted := range []string{"store", "corpus", "compose", "fetch"} {
		if strings.Contains(help, unwanted) {
			t.Errorf("root help unexpectedly contains %q:\n%s", unwanted, help)
		}
	}
}

func TestIndexOwnsCorpusWorkflows(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"index", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{"list", "show", "summary", "verify", "add", "update", "export", "remove"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("index help does not contain %q:\n%s", want, stdout.String())
		}
	}
}

func TestModelBuildOwnsRecipes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"model", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "build") {
		t.Fatalf("model help does not contain build:\n%s", stdout.String())
	}
}

func TestLeafHelpDoesNotExecuteCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"index", "summary", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "waldo index summary") {
		t.Fatalf("leaf help does not name command:\n%s", stdout.String())
	}
}

func TestPlannedCommandIsHonest(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"model", "build"}, &stdout, &stderr); code != 1 {
		t.Fatalf("Run() code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "not reached its implementation phase") {
		t.Fatalf("stderr does not describe command status: %q", stderr.String())
	}
}

func TestUnknownCommandSuggestsScopedHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"lookaside", "explode"}, &stdout, &stderr); code != 2 {
		t.Fatalf("Run() code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "waldo lookaside --help") {
		t.Fatalf("stderr does not suggest scoped help: %q", stderr.String())
	}
}

func TestLookasideStatusUsesNamedBackend(t *testing.T) {
	cacheRoot := filepath.Join(t.TempDir(), "objects")
	t.Setenv("WALDO_CACHE", cacheRoot)
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"lookaside", "status"}, &stdout, &stderr); code != 0 {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "lookaside cache") || !strings.Contains(stdout.String(), cacheRoot) {
		t.Fatalf("lookaside status = %q", stdout.String())
	}
}

func TestLookasideConfigurePersistsMirrors(t *testing.T) {
	configuration := filepath.Join(t.TempDir(), "config.json")
	cacheRoot := filepath.Join(t.TempDir(), "objects")
	t.Setenv("WALDO_CONFIG", configuration)
	t.Setenv("WALDO_CACHE", "")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"lookaside", "configure", "--cache", cacheRoot, "--mirror", "https://mirror.example/lookaside/v1/"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"lookaside", "status"}, &stdout, &stderr); code != 0 {
		t.Fatalf("status code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), cacheRoot) || !strings.Contains(stdout.String(), "https://mirror.example/lookaside/v1") {
		t.Fatalf("lookaside status = %q", stdout.String())
	}
}
