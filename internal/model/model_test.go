package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openwaldo/waldo-new/internal/corpus"
	"github.com/openwaldo/waldo-new/internal/index"
	"github.com/openwaldo/waldo-new/internal/provenance"
	"github.com/openwaldo/waldo-new/internal/training"
)

type backendFunc func(context.Context, training.Request) (training.Observation, error)

func (function backendFunc) Run(ctx context.Context, request training.Request) (training.Observation, error) {
	return function(ctx, request)
}

func TestLoadRecipeIsStrictAndResolvesCorpusRelativeToRecipe(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "smoke.yaml")
	if err := os.WriteFile(path, []byte(recipeYAML("exports/corpus", "")), 0o644); err != nil {
		t.Fatal(err)
	}
	recipe, loaded, err := LoadRecipe(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != path || recipe.Stages[0].Corpus != filepath.Join(directory, "exports", "corpus") {
		t.Fatalf("loaded = %q, corpus = %q", loaded, recipe.Stages[0].Corpus)
	}
	if err := os.WriteFile(path, []byte(recipeYAML("exports/corpus", "surprise: true\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadRecipe(path); err == nil {
		t.Fatal("LoadRecipe accepted an unknown field")
	}
}

func TestBuildPersistsPlanRunAndModelBOMs(t *testing.T) {
	root := t.TempDir()
	export := writeModelExport(t, t.TempDir())
	recipe := validRecipe(export)
	clock := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	builder := Builder{Root: root, Now: func() time.Time { return clock }, NewID: func() (string, error) { return "run0001", nil }}
	inspection, err := builder.Build(context.Background(), recipe)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Model.ID != inspection.Model.PlanSHA256 || len(inspection.Model.Runs) != 1 || inspection.Model.Runs[0].State != RunComplete {
		t.Fatalf("model = %+v", inspection.Model)
	}
	if len(inspection.Runs) != 1 || inspection.Runs[0].Observation == nil || !inspection.Runs[0].Observation.Simulated {
		t.Fatalf("runs = %+v", inspection.Runs)
	}
	pin := inspection.Model.Runs[0]
	runDirectory := filepath.Join(inspection.Path, "runs", runDirectoryName(pin))
	bomBytes, err := os.ReadFile(filepath.Join(runDirectory, "RUN-BOM.json"))
	if err != nil {
		t.Fatal(err)
	}
	var runBOM RunBOM
	if err := readJSON(filepath.Join(runDirectory, "RUN-BOM.json"), &runBOM); err != nil {
		t.Fatal(err)
	}
	wantBOMHash, err := hashJSON(runBOM)
	if err != nil {
		t.Fatal(err)
	}
	if len(bomBytes) == 0 || pin.BOMSHA256 != wantBOMHash || runBOM.CorpusBOM.Subject != "corpus" {
		t.Fatalf("pin = %+v, run BOM = %+v", pin, runBOM)
	}
	artifact := pin.Artifacts[0]
	artifactPath := filepath.Join(runDirectory, filepath.FromSlash(artifact.Path))
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if artifact.SHA256 != hex.EncodeToString(digest[:]) || !strings.Contains(string(data), "no trained model weights") {
		t.Fatalf("artifact = %+v, contents = %q", artifact, data)
	}
	if _, err := builder.Build(context.Background(), recipe); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second Build() error = %v", err)
	}
}

func TestBuildPreflightsAllExportsBeforeCreatingModel(t *testing.T) {
	root := t.TempDir()
	recipe := validRecipe(writeModelExport(t, t.TempDir()))
	recipe.Stages = append(recipe.Stages, Stage{Name: "second", Type: "fine-tuning", Objective: "causal-language-modeling", Corpus: filepath.Join(t.TempDir(), "missing"), Parameters: recipe.Stages[0].Parameters})
	if _, err := (Builder{Root: root}).Build(context.Background(), recipe); err == nil {
		t.Fatal("Build succeeded")
	}
	if _, err := os.Stat(filepath.Join(root, recipe.Name)); !os.IsNotExist(err) {
		t.Fatalf("model exists after preflight failure: %v", err)
	}
}

func TestBuildRejectsCorruptExportBeforeCreatingModel(t *testing.T) {
	root := t.TempDir()
	export := writeModelExport(t, t.TempDir())
	document, path, err := provenance.LoadCorpusExport(export)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), filepath.FromSlash(document.Files[0].Path)), []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Builder{Root: root}).Build(context.Background(), validRecipe(export)); err == nil {
		t.Fatal("Build accepted corrupt export")
	}
	if _, err := os.Stat(filepath.Join(root, "smoke")); !os.IsNotExist(err) {
		t.Fatalf("model exists after corrupt preflight: %v", err)
	}
}

func TestBuildPersistsFailureAndInterruption(t *testing.T) {
	for _, test := range []struct {
		name  string
		err   error
		state RunState
	}{
		{name: "failed", err: errors.New("trainer exited"), state: RunFailed},
		{name: "interrupted", err: context.Canceled, state: RunInterrupted},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			recipe := validRecipe(writeModelExport(t, t.TempDir()))
			builder := Builder{
				Root: root, NewID: func() (string, error) { return "run0001", nil },
				ResolveBackend: func(training.Identity) (training.Backend, error) {
					return backendFunc(func(context.Context, training.Request) (training.Observation, error) {
						return training.Observation{}, test.err
					}), nil
				},
			}
			if _, err := builder.Build(context.Background(), recipe); err == nil {
				t.Fatal("Build succeeded")
			}
			inspection, err := Inspect(root, recipe.Name)
			if err != nil {
				t.Fatal(err)
			}
			if inspection.Model.Runs[0].State != test.state || inspection.Runs[0].State != test.state || inspection.Runs[0].Error != test.err.Error() {
				t.Fatalf("inspection = %+v", inspection)
			}
		})
	}
}

func validRecipe(export string) Recipe {
	return Recipe{
		Kind: "waldo-model-recipe", Schema: 1, Name: "smoke",
		Architecture: Architecture{
			Family: "decoder-transformer", ContextTokens: 128, VocabularySize: 256,
			HiddenSize: 64, IntermediateSize: 192, Layers: 2, AttentionHeads: 4, KeyValueHeads: 2,
			TieEmbeddings: true, ParameterDType: "float32", Tokenizer: Tokenizer{Name: "byte", Revision: "sha256:example"},
		},
		Backend: training.Identity{Name: "fake", Revision: training.FakeRevision},
		Stages: []Stage{{Name: "pretrain", Type: "pre-training", Objective: "causal-language-modeling", Corpus: export, Parameters: training.Parameters{
			Steps: 2, BatchSize: 1, SequenceLength: 64, LearningRate: 0.001, Seed: 7,
		}}},
	}
}

func recipeYAML(corpusPath, suffix string) string {
	return "kind: waldo-model-recipe\n" +
		"schema: 1\nname: smoke\n" +
		"architecture:\n  family: decoder-transformer\n  context_tokens: 128\n  vocabulary_size: 256\n  hidden_size: 64\n  intermediate_size: 192\n  layers: 2\n  attention_heads: 4\n  key_value_heads: 2\n  tie_embeddings: true\n  parameter_dtype: float32\n  tokenizer:\n    name: byte\n    revision: sha256:example\n" +
		"backend:\n  name: fake\n  revision: " + training.FakeRevision + "\n" +
		"stages:\n  - name: pretrain\n    type: pre-training\n    objective: causal-language-modeling\n    corpus: " + corpusPath + "\n    parameters:\n      steps: 2\n      batch_size: 1\n      sequence_length: 64\n      learning_rate: 0.001\n      seed: 7\n" + suffix
}

func writeModelExport(t *testing.T, parent string) string {
	t.Helper()
	directory, err := os.MkdirTemp(parent, "export-")
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("canonical parquet fixture")
	digestArray := sha256.Sum256(data)
	digest := hex.EncodeToString(digestArray[:])
	relative := "data/example/example.parquet"
	path := filepath.Join(directory, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	conversion := index.Conversion{Tool: "test", Version: "1", Profile: "text", Recipe: "test/v1", Tokenizer: "byte"}
	measures := index.Measures{Shards: 1, Docs: 1, Tokens: 128, Bytes: int64(len(data))}
	bom := corpus.BOM{
		Kind: "openwaldo-bom", Schema: 1, Subject: "corpus", Paths: []string{"example"},
		Manifests: []corpus.ManifestPin{{
			Path: "example/example.json", SHA256: strings.Repeat("a", 64), Name: "example", Title: "Example",
			Description: "Model fixture.", License: "CC0-1.0", Format: "parquet", RecordSchema: 1,
			ConvertedBy: conversion, Sources: []index.Source{{Name: "fixture", Source: "Fixture", URL: "https://example.test", SHA256: strings.Repeat("b", 64)}},
			Totals: measures, Licenses: map[string]index.Measures{"CC0-1.0": measures},
		}},
		Shards: []corpus.ShardPin{{
			Manifest: "example/example.json", URL: "https://objects.example/" + digest, SHA256: digest,
			Format: "parquet", RecordSchema: 1, License: "CC0-1.0", ConvertedBy: conversion,
			Docs: 1, Tokens: 128, Bytes: int64(len(data)),
		}},
		Totals: measures, Licenses: map[string]index.Measures{"CC0-1.0": measures},
	}
	files := []corpus.ExportedFile{{
		Path: relative, Manifest: "example/example.json", ObjectSHA256: digest, SHA256: digest,
		Format: "parquet", License: "CC0-1.0", Docs: 1, Tokens: 128,
		ObjectBytes: int64(len(data)), Bytes: int64(len(data)),
	}}
	document := provenance.NewCorpusExport(bom, "native", files, time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC))
	if err := provenance.WriteCorpusExport(directory, document); err != nil {
		t.Fatal(err)
	}
	return directory
}
