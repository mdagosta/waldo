package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo-new/internal/config"
	"github.com/openwaldo/waldo-new/internal/lookaside"
)

type cliPublisher struct{ objects map[string]int64 }

func (publisher *cliPublisher) BaseURL() string { return "s3://openwaldo/lookaside/v1" }
func (publisher *cliPublisher) Publish(_ context.Context, source, digest string, size int64, progress func(lookaside.PublishProgress)) (lookaside.PublishedObject, error) {
	if err := lookaside.VerifyFile(source, digest, size); err != nil {
		return lookaside.PublishedObject{}, err
	}
	if publisher.objects == nil {
		publisher.objects = map[string]int64{}
	}
	publisher.objects[digest] = size
	if progress != nil {
		progress(lookaside.PublishProgress{Written: size, Total: size})
	}
	return lookaside.PublishedObject{URL: publisher.BaseURL() + "/" + digest[:2] + "/" + digest[2:4] + "/" + digest, SHA256: digest, Bytes: size}, nil
}
func (publisher *cliPublisher) Verify(_ context.Context, digest string, size int64) (lookaside.PublishedObject, error) {
	if publisher.objects[digest] != size {
		return lookaside.PublishedObject{}, fmt.Errorf("missing object")
	}
	return lookaside.PublishedObject{SHA256: digest, Bytes: size}, nil
}

func TestIndexAddDryRunProducesImmutablePlan(t *testing.T) {
	input := filepath.Join(t.TempDir(), "document.md")
	if err := os.WriteFile(input, []byte("# Example\n\nTraining text.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"index", "ingest", input, "core/example",
		"--title", "Example", "--license", "CC0-1.0",
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
			Writer struct {
				RecordSchema int `json:"record_schema"`
			} `json:"writer"`
			Inputs []struct {
				Adapter string `json:"adapter"`
			} `json:"inputs"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Identity) != 64 || output.Plan.Kind != "waldo-ingest-plan" || output.Plan.Writer.RecordSchema != 1 || len(output.Plan.Inputs) != 1 || output.Plan.Inputs[0].Adapter != "markdown" {
		t.Fatalf("index ingest output = %+v", output)
	}
}

func TestIndexIngestRejectsFormerToOption(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"index", "ingest", "input", "destination", "--to", "other",
		"--title", "Example", "--license", "CC0-1.0",
		"--source", "https://example.test/data", "--source-category", "public-dataset", "--dry-run",
	}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "unknown index ingest option") {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
}

func TestIndexIngestExecutionRequiresWritableLookaside(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"index", "ingest", "/does/not/need/to/exist", "core/example",
		"--title", "Example", "--license", "CC0-1.0",
		"--source", "https://example.test/data", "--source-category", "public-dataset",
	}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "needs a writable lookaside") {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
}

func TestIndexIngestPublishesAndBuildsContributionOverlay(t *testing.T) {
	input := filepath.Join(t.TempDir(), "document.txt")
	if err := os.WriteFile(input, []byte("training document"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.json"), []byte("{\n  \"kind\": \"index\",\n  \"schema\": 2,\n  \"path\": \"\",\n  \"entries\": [{\"name\": \"core\", \"type\": \"dir\"}]\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "core", "index.json"), []byte("{\n  \"kind\": \"index\",\n  \"schema\": 2,\n  \"path\": \"core\",\n  \"entries\": []\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	staging := t.TempDir()
	cache := t.TempDir()
	t.Setenv("WALDO_CACHE", cache)
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	originalPublisher := newIngestPublisher
	remote := &cliPublisher{}
	newIngestPublisher = func(context.Context, config.Publish) (lookaside.Publisher, error) { return remote, nil }
	t.Cleanup(func() { newIngestPublisher = originalPublisher })
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--index", root, "--json", "index", "ingest", input, "core/example",
		"--title", "Example", "--description", "Example corpus.",
		"--license", "CC0-1.0", "--source", "https://example.test/data",
		"--source-category", "public-dataset", "--staging", staging,
		"--object-base", "s3://openwaldo/lookaside/v1",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
	var output struct {
		Assembly struct {
			RetainedDocs int64 `json:"retained_docs"`
		} `json:"assembly"`
		Publication struct {
			Objects []struct {
				SHA256 string `json:"sha256"`
			} `json:"objects"`
		} `json:"publication"`
		Contribution struct {
			Root  string   `json:"root"`
			Files []string `json:"files"`
		} `json:"contribution"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Assembly.RetainedDocs != 1 || len(output.Publication.Objects) != 1 || len(output.Contribution.Files) != 3 {
		t.Fatalf("output = %+v", output)
	}
	if remote.objects[output.Publication.Objects[0].SHA256] == 0 {
		t.Fatal("published object is absent from fake lookaside")
	}
	if _, err := os.Stat(filepath.Join(output.Contribution.Root, "core", "example", "example.json")); err != nil {
		t.Fatal(err)
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
	for _, want := range []string{"list", "show", "summary", "verify", "ingest", "update", "export", "remove"} {
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

func TestLookasideConfigurePersistsPublisher(t *testing.T) {
	configurationPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("WALDO_CONFIG", configurationPath)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"lookaside", "configure", "--publish", "s3://bucket/lookaside/v1/", "--publish-region", "us-west-2", "--upload-workers", "6", "--keep-local"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
	configuration, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	publish := configuration.Lookaside.Publish
	if publish == nil || publish.URL != "s3://bucket/lookaside/v1" || publish.Region != "us-west-2" || publish.Workers != 6 || !publish.KeepLocal {
		t.Fatalf("publish configuration = %+v", publish)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"lookaside", "status"}, &stdout, &stderr); code != 0 {
		t.Fatalf("status code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "s3://bucket/lookaside/v1") || !strings.Contains(stdout.String(), "6 workers") {
		t.Fatalf("lookaside status = %q", stdout.String())
	}
}
