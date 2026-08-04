package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexAddDryRunProducesImmutablePlan(t *testing.T) {
	input := filepath.Join(t.TempDir(), "document.md")
	if err := os.WriteFile(input, []byte("# Example\n\nTraining text.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"index", "add", input,
		"--to", "core/example", "--title", "Example", "--license", "CC0-1.0",
		"--source", "https://example.test/data", "--source-category", "public-dataset",
		"--dry-run", "--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
	var output struct {
		Identity string `json:"identity"`
		Plan     struct {
			Kind   string `json:"kind"`
			Inputs []struct {
				Adapter string `json:"adapter"`
			} `json:"inputs"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Identity) != 64 || output.Plan.Kind != "waldo-ingest-plan" || len(output.Plan.Inputs) != 1 || output.Plan.Inputs[0].Adapter != "markdown" {
		t.Fatalf("index add output = %+v", output)
	}
}

func TestIndexAddRequiresDryRunUntilExecutionLands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"index", "add", "/does/not/need/to/exist",
		"--to", "core/example", "--title", "Example", "--license", "CC0-1.0",
		"--source", "https://example.test/data", "--source-category", "public-dataset",
	}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "rerun with --dry-run") {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
}

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
