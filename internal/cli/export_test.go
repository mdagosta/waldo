package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo-new/internal/config"
	"github.com/openwaldo/waldo-new/internal/record"
	"github.com/openwaldo/waldo-new/internal/shard"
	"github.com/parquet-go/parquet-go"
)

func TestIndexExportEndToEnd(t *testing.T) {
	root := t.TempDir()
	text := "small native shard"
	var parquetData bytes.Buffer
	writer := parquet.NewGenericWriter[shard.Row](&parquetData)
	if _, err := writer.Write([]shard.Row{{
		SHA256: record.TextHash(text), Kind: record.KindPretrain, Text: text,
		Source: "fixture", License: "CC0-1.0", Tokens: 3,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	content := parquetData.Bytes()
	digestArray := sha256.Sum256(content)
	digest := hex.EncodeToString(digestArray[:])
	source := filepath.Join(root, "source.parquet")
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(root, "index.json"), `{
  "kind": "index", "schema": 2, "path": "",
  "entries": [{"name": "books", "type": "dir"}]
}`)
	writeCLIFile(t, filepath.Join(root, "books", "index.json"), `{
  "kind": "index", "schema": 2, "path": "books",
  "entries": [{"name": "books.json", "type": "manifest"}]
}`)
	manifest := fmt.Sprintf(`{
  "kind": "manifest", "schema": 1, "name": "books", "title": "Books",
  "description": "Small books.", "license": "CC0-1.0",
  "sources": [{"name": "source", "source": "Fixture", "url": "https://example.test", "sha256": %q}],
  "converted_by": {"tool": "test", "version": "1", "profile": "text", "recipe": "test/v1", "tokenizer": "byte"},
  "shards": [{"url": %q, "sha256": %q, "sources": ["source"], "docs": 1, "tokens": 3, "bytes": %d}]
}`, strings.Repeat("a", 64), source, digest, len(content))
	writeCLIFile(t, filepath.Join(root, "books", "books.json"), manifest)

	cache := filepath.Join(t.TempDir(), "cache")
	models := filepath.Join(t.TempDir(), "models")
	destination := filepath.Join(t.TempDir(), "export")
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := config.Save(config.Config{Lookaside: config.Lookaside{Scratch: cache}, Model: config.Model{Root: models}}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"index", "export", filepath.Join(root, "books"), destination}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "EXPORT.json") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(destination, "EXPORT.json")); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(destination, "data", "books", "*.parquet"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("exported parquet files = %v", matches)
	}
	assertNoCacheFiles(t, cache)
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"bom", "verify", destination}, &stdout, &stderr); code != 0 {
		t.Fatalf("bom verify code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "verified OpenWALDO BOM") {
		t.Fatalf("bom verify output = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"bom", "show", destination}, &stdout, &stderr); code != 0 {
		t.Fatalf("bom show code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "OpenWALDO corpus export") || !strings.Contains(stdout.String(), "native") {
		t.Fatalf("bom show output = %q", stdout.String())
	}

	recipe := filepath.Join(t.TempDir(), "smoke.yaml")
	recipeData := fmt.Sprintf(`kind: waldo-model-recipe
schema: 1
name: smoke
architecture:
  family: decoder-transformer
  context_tokens: 128
  vocabulary_size: 256
  hidden_size: 64
  intermediate_size: 192
  layers: 2
  attention_heads: 4
  key_value_heads: 2
  tie_embeddings: true
  parameter_dtype: float32
  tokenizer:
    name: byte
    revision: sha256:fixture
stages:
  - name: pretrain
    type: pre-training
    objective: causal-language-modeling
    corpus: %q
    parameters:
      steps: 2
      batch_size: 1
      sequence_length: 64
      learning_rate: 0.001
      seed: 7
`, destination)
	if err := os.WriteFile(recipe, []byte(recipeData), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"model", "build", recipe}, &stdout, &stderr); code != 0 {
		t.Fatalf("model build code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "simulated training") || !strings.Contains(stderr.String(), "preflight/pretrain") {
		t.Fatalf("model build stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"model", "inspect", "smoke"}, &stdout, &stderr); code != 0 {
		t.Fatalf("model inspect code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "complete") || !strings.Contains(stdout.String(), "simulated") {
		t.Fatalf("model inspect stdout = %q", stdout.String())
	}
	for _, name := range []string{"PLAN.json", "MODEL.json", "MODEL-BOM.json"} {
		if _, err := os.Stat(filepath.Join(models, "smoke", name)); err != nil {
			t.Fatal(err)
		}
	}

	disclosureOutput := filepath.Join(t.TempDir(), "eu-gpai.json")
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"bom", "export", "smoke", disclosureOutput, "--format", "eu-gpai"}, &stdout, &stderr); code != 1 {
		t.Fatalf("complete disclosure code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "export blocked") {
		t.Fatalf("blocked disclosure stderr = %q", stderr.String())
	}
	if _, err := os.Stat(disclosureOutput); !os.IsNotExist(err) {
		t.Fatalf("blocked disclosure wrote output: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"bom", "export", "smoke", disclosureOutput, "--format=eu-gpai", "--allow-incomplete"}, &stdout, &stderr); code != 0 {
		t.Fatalf("draft disclosure code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	disclosureData, err := os.ReadFile(disclosureOutput)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(disclosureData), `"status": "incomplete-draft"`) || !strings.Contains(string(disclosureData), `"field": "provider.profile"`) || !strings.Contains(string(disclosureData), `"field": "training.observed-consumption"`) {
		t.Fatalf("draft disclosure = %s", disclosureData)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"bom", "export", "smoke", "--format", "eu-gpai", "--allow-incomplete"}, &stdout, &stderr); code != 0 {
		t.Fatalf("stdout disclosure code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "{\n") || !strings.Contains(stdout.String(), `"kind": "waldo-eu-gpai-training-content"`) || !strings.Contains(stdout.String(), `"status": "incomplete-draft"`) {
		t.Fatalf("stdout disclosure = %q", stdout.String())
	}

	jsonlDestination := filepath.Join(t.TempDir(), "jsonl-export")
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"index", "export", filepath.Join(root, "books"), "--format=jsonl", jsonlDestination}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("JSONL Run() code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	jsonlMatches, err := filepath.Glob(filepath.Join(jsonlDestination, "data", "books", "*.jsonl"))
	if err != nil || len(jsonlMatches) != 1 {
		t.Fatalf("exported JSONL files = %v, error = %v", jsonlMatches, err)
	}
	jsonl, err := os.ReadFile(jsonlMatches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(jsonl), `"text":"small native shard"`) {
		t.Fatalf("JSONL = %q", jsonl)
	}
	assertNoCacheFiles(t, cache)
}

func assertNoCacheFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			t.Fatalf("cache file remains after successful command: %s", path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestParseExportOptions(t *testing.T) {
	options, err := parseExportOptions([]string{"core", "science", "--format=jsonl", "--license", "CC0-*, CC-BY-*", "--exclude-license=CC-BY-NC-*", "--force", "dest"})
	if err != nil {
		t.Fatal(err)
	}
	if len(options.Paths) != 2 || len(options.Include) != 2 || len(options.Exclude) != 1 || options.Output != "dest" || options.Format != "jsonl" || !options.Force {
		t.Fatalf("parseExportOptions() = %+v", options)
	}
}

func TestParseBOMExportOptions(t *testing.T) {
	options, err := parseBOMExportOptions([]string{"smoke", "report.json", "--format=eu-gpai", "--provider", "provider.json", "--allow-incomplete", "--force"})
	if err != nil {
		t.Fatal(err)
	}
	if options.Model != "smoke" || options.Output != "report.json" || options.Provider != "provider.json" || !options.AllowIncomplete || !options.Force {
		t.Fatalf("parseBOMExportOptions() = %+v", options)
	}
	if _, err := parseBOMExportOptions([]string{"smoke", "report.docx", "--format", "eu-gpai"}); err == nil || !strings.Contains(err.Error(), ".json") {
		t.Fatalf("document output error = %v", err)
	}
	stdoutOptions, err := parseBOMExportOptions([]string{"smoke", "--format", "eu-gpai", "--allow-incomplete"})
	if err != nil || stdoutOptions.Model != "smoke" || stdoutOptions.Output != "" || !stdoutOptions.AllowIncomplete {
		t.Fatalf("stdout parse = %+v, %v", stdoutOptions, err)
	}
	if _, err := parseBOMExportOptions([]string{"smoke", "--format", "eu-gpai", "--force"}); err == nil || !strings.Contains(err.Error(), "requires an output file") {
		t.Fatalf("stdout force error = %v", err)
	}
}

func TestIndexExportRejectsRemovedOutputOption(t *testing.T) {
	_, err := parseExportOptions([]string{"core", "--output", "dest"})
	if err == nil || !strings.Contains(err.Error(), "unknown index export option") {
		t.Fatalf("parseExportOptions error = %v", err)
	}
}

func writeCLIFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
