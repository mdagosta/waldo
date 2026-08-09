// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/openwaldo/waldo/internal/corpus"
	"github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/record"
	"github.com/openwaldo/waldo/internal/shard"
	"github.com/openwaldo/waldo/internal/training"
	"github.com/parquet-go/parquet-go"
)

type backendFunc func(context.Context, training.Request) (training.Observation, error)

func (backendFunc) Descriptor() training.Descriptor {
	return training.Descriptor{
		Identity: training.Identity{Name: "test", Revision: "test-schema-1"}, Framework: "test",
		Capabilities: training.Capabilities{Objectives: []string{"causal-language-modeling"}, CheckpointResume: true},
	}
}

func (function backendFunc) Run(ctx context.Context, request training.Request) (training.Observation, error) {
	return function(ctx, request)
}

func TestLoadComposeIsStrictAndKeepsIndexPathsLogical(t *testing.T) {
	path := filepath.Join(t.TempDir(), "smoke.yaml")
	if err := os.WriteFile(path, []byte(composeYAML("")), 0o644); err != nil {
		t.Fatal(err)
	}
	compose, loaded, err := LoadCompose(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != path || !reflect.DeepEqual(compose.Stages[0].Corpora, []string{"core/books", "science/papers"}) {
		t.Fatalf("loaded = %q, corpora = %v", loaded, compose.Stages[0].Corpora)
	}
	if err := os.WriteFile(path, []byte(composeYAML("backend:\n  name: fake\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadCompose(path); err == nil || !strings.Contains(err.Error(), "field backend not found") {
		t.Fatalf("LoadCompose backend error = %v", err)
	}
}

func TestInitializeAndTrainKeepStableModelIdentity(t *testing.T) {
	root := t.TempDir()
	clock := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	builder := Builder{Root: root, Now: func() time.Time { return clock }, NewID: func() (string, error) { return "run0001", nil }, Resolver: training.FakeResolver()}
	initialized, err := builder.Initialize("smoke", testArchitecture())
	if err != nil {
		t.Fatal(err)
	}
	if len(initialized.Model.Runs) != 0 || initialized.Plan.Kind != "waldo-model-plan" {
		t.Fatalf("initialized = %+v", initialized)
	}
	trained, err := builder.Train(context.Background(), "smoke", preparedFixture(t, testStage("pretrain")))
	if err != nil {
		t.Fatal(err)
	}
	if trained.Model.ID != initialized.Model.ID || trained.Model.PlanSHA256 != initialized.Model.PlanSHA256 || len(trained.Model.Runs) != 1 || trained.Model.Runs[0].State != RunComplete {
		t.Fatalf("trained model = %+v", trained.Model)
	}
	if trained.RunBOMs[0].Execution.Backend.Name != "fake" || trained.RunBOMs[0].Execution.Host.OS == "" || trained.Runs[0].Observation == nil || !trained.Runs[0].Observation.Simulated {
		t.Fatalf("run = %+v, BOM = %+v", trained.Runs[0], trained.RunBOMs[0])
	}
	if trained.RunBOMs[0].EvaluationSet == nil || trained.RunBOMs[0].EvaluationSet.Records != 1 || len(trained.Runs[0].Observation.Evaluations) != 1 || trained.Runs[0].Observation.Evaluations[0].Metrics["heldout_loss"] <= 0 {
		t.Fatalf("held-out evidence = %+v / %+v", trained.RunBOMs[0].EvaluationSet, trained.Runs[0].Observation.Evaluations)
	}
	telemetryPath := filepath.Join(trained.Path, "runs", "0001-pretrain-run0001", TelemetryFilename)
	telemetryFile, err := os.Open(telemetryPath)
	if err != nil {
		t.Fatal(err)
	}
	telemetry, err := csv.NewReader(telemetryFile).ReadAll()
	_ = telemetryFile.Close()
	if err != nil || len(telemetry) < 4 || !reflect.DeepEqual(telemetry[0], telemetryHeader) {
		t.Fatalf("telemetry rows = %v, err = %v", telemetry, err)
	}
	last := telemetry[len(telemetry)-1]
	if last[2] != "run0001" || last[5] != "run" || last[6] != string(RunComplete) || last[8] == "" || last[10] == "" {
		t.Fatalf("terminal telemetry = %v", last)
	}
	foundEvaluation := false
	for _, row := range telemetry[1:] {
		if row[5] == "evaluation" && row[12] != "" && row[13] != "" {
			foundEvaluation = true
		}
	}
	if !foundEvaluation {
		t.Fatalf("telemetry has no chartable held-out evaluation: %v", telemetry)
	}
	bomRun := trained.BOM.Runs[0]
	if trained.BOM.PathBase != "model-root" || trained.BOM.CurrentRunID != "" || bomRun.Backend.Name != "fake" || !bomRun.Simulated || bomRun.RunBOM != "runs/0001-pretrain-run0001/RUN-BOM.json" || bomRun.Artifacts[0].Role != "simulation" || bomRun.Artifacts[0].Path != "runs/0001-pretrain-run0001/artifacts/fake-model.json" {
		t.Fatalf("aggregate BOM = %+v", trained.BOM)
	}
	artifact := trained.Model.Runs[0].Artifacts[0]
	data, err := os.ReadFile(filepath.Join(trained.Path, "runs", runDirectoryName(trained.Model.Runs[0]), filepath.FromSlash(artifact.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "no trained model weights") {
		t.Fatalf("artifact = %q", data)
	}
}

func TestModelBOMIdentifiesLatestRealWeights(t *testing.T) {
	record := ModelRecord{
		ID: "model", Name: "example", PlanSHA256: "plan", ArchitectureSHA256: "architecture", Updated: "now",
		Runs: []RunPin{
			{ID: "fake", Stage: "first", Ordinal: 1, State: RunComplete, Backend: training.Identity{Name: "fake", Revision: "fake-v1"}, Simulated: true, Artifacts: []training.Artifact{{Path: "artifacts/fake-model.json", SHA256: "fake", Bytes: 1}}},
			{ID: "real", Stage: "second", Ordinal: 2, State: RunComplete, Backend: training.Identity{Name: "mlx", Revision: "mlx-v1"}, Artifacts: []training.Artifact{{Path: "artifacts/model.safetensors", SHA256: "weights", Bytes: 2}, {Path: "artifacts/config.json", SHA256: "config", Bytes: 3}}},
		},
	}
	bom := modelBOM(record)
	if bom.CurrentRunID != "real" || bom.Runs[1].Artifacts[0].Role != "weights" || bom.Runs[1].Artifacts[1].Role != "configuration" || !strings.HasPrefix(bom.Runs[1].Artifacts[0].Path, "runs/0002-second-real/") {
		t.Fatalf("BOM = %+v", bom)
	}
}

func TestInspectNormalizesLegacySchemaOneModelBOM(t *testing.T) {
	root := t.TempDir()
	builder := Builder{Root: root, NewID: func() (string, error) { return "legacy", nil }, Resolver: training.FakeResolver()}
	trained, err := builder.Initialize("smoke", testArchitecture())
	if err != nil {
		t.Fatal(err)
	}
	trained, err = builder.Train(context.Background(), "smoke", preparedFixture(t, testStage("pretrain")))
	if err != nil {
		t.Fatal(err)
	}
	record := trained.Model
	record.Runs[0].Backend = training.Identity{}
	record.Runs[0].Simulated = false
	if err := writeJSONAtomic(filepath.Join(trained.Path, "MODEL.json"), record); err != nil {
		t.Fatal(err)
	}
	legacyBOM := map[string]any{
		"kind": "openwaldo-bom", "schema": 1, "subject": "model", "model_id": record.ID,
		"name": record.Name, "plan_sha256": record.PlanSHA256, "architecture_sha256": record.ArchitectureSHA256,
		"runs": []RunPin{record.Runs[0]}, "generated": record.Updated,
	}
	if err := writeJSONAtomic(filepath.Join(trained.Path, "MODEL-BOM.json"), legacyBOM); err != nil {
		t.Fatal(err)
	}
	inspection, err := Inspect(root, "smoke")
	if err != nil {
		t.Fatal(err)
	}
	if inspection.BOM.PathBase != "model-root" || inspection.BOM.Runs[0].Backend.Name != "fake" || !inspection.BOM.Runs[0].Simulated || inspection.BOM.Runs[0].Artifacts[0].Role != "simulation" {
		t.Fatalf("normalized BOM = %+v", inspection.BOM)
	}
}

func TestInspectAcceptsLegacyRunBOMWithoutEpochs(t *testing.T) {
	root := t.TempDir()
	builder := Builder{Root: root, NewID: func() (string, error) { return "legacy-epochs", nil }, Resolver: training.FakeResolver()}
	if _, err := builder.Initialize("smoke", testArchitecture()); err != nil {
		t.Fatal(err)
	}
	trained, err := builder.Train(context.Background(), "smoke", preparedFixture(t, testStage("pretrain")))
	if err != nil {
		t.Fatal(err)
	}
	pin := trained.Model.Runs[0]
	runDirectory := filepath.Join(trained.Path, "runs", runDirectoryName(pin))
	runBOM := trained.RunBOMs[0]
	runBOM.Parameters.Epochs = 0
	legacyHash, err := hashJSON(runBOM)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(runDirectory, "RUN-BOM.json"), runBOM); err != nil {
		t.Fatal(err)
	}
	legacyData, err := os.ReadFile(filepath.Join(runDirectory, "RUN-BOM.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(legacyData, []byte(`"epochs"`)) {
		t.Fatalf("legacy run BOM unexpectedly contains epochs: %s", legacyData)
	}
	run := trained.Runs[0]
	run.BOMSHA256 = legacyHash
	if err := writeJSONAtomic(filepath.Join(runDirectory, "RUN.json"), run); err != nil {
		t.Fatal(err)
	}
	record := trained.Model
	record.Runs[0].BOMSHA256 = legacyHash
	if err := writeJSONAtomic(filepath.Join(trained.Path, "MODEL.json"), record); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(trained.Path, "MODEL-BOM.json"), modelBOM(record)); err != nil {
		t.Fatal(err)
	}
	inspection, err := Inspect(root, "smoke")
	if err != nil {
		t.Fatal(err)
	}
	if inspection.RunBOMs[0].Parameters.Epochs != 0 {
		t.Fatalf("legacy epochs = %d, want omitted zero", inspection.RunBOMs[0].Parameters.Epochs)
	}
}

func TestExportRejectsCorruptModelArtifact(t *testing.T) {
	root := t.TempDir()
	builder := Builder{Root: root, NewID: func() (string, error) { return "run0001", nil }, Resolver: training.FakeResolver()}
	if _, err := builder.Initialize("smoke", testArchitecture()); err != nil {
		t.Fatal(err)
	}
	trained, err := builder.Train(context.Background(), "smoke", preparedFixture(t, testStage("pretrain")))
	if err != nil {
		t.Fatal(err)
	}
	pin := trained.Model.Runs[0]
	artifact := filepath.Join(trained.Path, "runs", runDirectoryName(pin), filepath.FromSlash(pin.Artifacts[0].Path))
	if err := os.WriteFile(artifact, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(root, "smoke", filepath.Join(t.TempDir(), "export")); err == nil || !strings.Contains(err.Error(), "verify exported model artifacts") {
		t.Fatalf("Export error = %v", err)
	}
}

func TestTrainRejectsResolverMismatchBeforeAddingRun(t *testing.T) {
	root := t.TempDir()
	builder := Builder{Root: root}
	if _, err := builder.Initialize("smoke", testArchitecture()); err != nil {
		t.Fatal(err)
	}
	builder.Resolver = training.ResolverFunc(func(context.Context, training.ResolveRequest) (training.Selection, error) {
		selection := testSelection(training.Fake{})
		selection.Execution.Framework = "pytorch"
		return selection, nil
	})
	if _, err := builder.Train(context.Background(), "smoke", preparedFixture(t, testStage("pretrain"))); err == nil || !strings.Contains(err.Error(), "does not match backend") {
		t.Fatalf("Train error = %v", err)
	}
	inspection, err := Inspect(root, "smoke")
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Model.Runs) != 0 {
		t.Fatalf("runs = %+v", inspection.Model.Runs)
	}
}

func TestTrainPersistsFailureAndInterruption(t *testing.T) {
	for _, test := range []struct {
		name  string
		err   error
		state RunState
	}{{"failed", errors.New("trainer exited"), RunFailed}, {"interrupted", context.Canceled, RunInterrupted}} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			backend := backendFunc(func(context.Context, training.Request) (training.Observation, error) {
				return training.Observation{}, test.err
			})
			builder := Builder{Root: root, NewID: func() (string, error) { return "run0001", nil }, Resolver: training.ResolverFunc(func(context.Context, training.ResolveRequest) (training.Selection, error) {
				return testSelection(backend), nil
			})}
			if _, err := builder.Initialize("smoke", testArchitecture()); err != nil {
				t.Fatal(err)
			}
			if _, err := builder.Train(context.Background(), "smoke", preparedFixture(t, testStage("pretrain"))); err == nil {
				t.Fatal("Train succeeded")
			}
			inspection, err := Inspect(root, "smoke")
			if err != nil {
				t.Fatal(err)
			}
			if inspection.Runs[0].State != test.state || inspection.Runs[0].Error != test.err.Error() {
				t.Fatalf("run = %+v", inspection.Runs[0])
			}
		})
	}
}

func TestTrainResumesInterruptedRunFromVerifiedCheckpoint(t *testing.T) {
	root := t.TempDir()
	attempts := 0
	backend := backendFunc(func(_ context.Context, request training.Request) (training.Observation, error) {
		attempts++
		if attempts == 1 {
			path := filepath.Join(request.ArtifactDirectory, "checkpoints", "step-00000001", "state.json")
			data := []byte("{\"kind\":\"waldo-test-checkpoint\",\"schema\":1,\"step\":1}\n")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return training.Observation{}, err
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				return training.Observation{}, err
			}
			digest := sha256.Sum256(data)
			checkpoint := training.Checkpoint{Step: 1, Tokens: 64, Artifacts: []training.Artifact{{Path: "artifacts/checkpoints/step-00000001/state.json", SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(data))}}}
			request.Report(training.Event{Kind: "checkpoint", Message: "test checkpoint", Step: 1, Tokens: 64, Checkpoint: &checkpoint})
			return training.Observation{}, context.Canceled
		}
		if request.Resume == nil || request.Resume.Step != 1 || request.Resume.Tokens != 64 || len(request.Resume.Paths) != 1 {
			return training.Observation{}, fmt.Errorf("missing resume point: %+v", request.Resume)
		}
		data := []byte("resumed model weights")
		path := filepath.Join(request.ArtifactDirectory, "model.safetensors")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return training.Observation{}, err
		}
		digest := sha256.Sum256(data)
		loss := 0.5
		return training.Observation{Steps: 2, ConsumedTokens: 128, FinalLoss: &loss, Evaluations: []training.Evaluation{{Step: 2, Tokens: 128, Metrics: map[string]float64{"heldout_loss": 0.75, "heldout_perplexity": 2.117}}}, Artifacts: []training.Artifact{{Path: "artifacts/model.safetensors", SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(data))}}}, nil
	})
	ids := 0
	builder := Builder{Root: root, NewID: func() (string, error) { ids++; return "resume0001", nil }, Resolver: training.ResolverFunc(func(context.Context, training.ResolveRequest) (training.Selection, error) {
		return testSelection(backend), nil
	})}
	if _, err := builder.Initialize("smoke", testArchitecture()); err != nil {
		t.Fatal(err)
	}
	stage := preparedFixture(t, testStage("train-0001"))
	if _, err := builder.Train(context.Background(), "smoke", stage); !errors.Is(err, context.Canceled) {
		t.Fatalf("first Train error = %v", err)
	}
	interrupted, err := Inspect(root, "smoke")
	if err != nil {
		t.Fatal(err)
	}
	if len(interrupted.Model.Runs) != 1 || interrupted.Runs[0].State != RunInterrupted || interrupted.Model.Runs[0].Resume == nil || len(interrupted.Runs[0].Attempts) != 1 {
		t.Fatalf("interrupted run = %+v / %+v", interrupted.Model.Runs, interrupted.Runs)
	}
	completed, err := builder.Train(context.Background(), "smoke", stage)
	if err != nil {
		t.Fatal(err)
	}
	if ids != 1 || attempts != 2 || len(completed.Model.Runs) != 1 || completed.Runs[0].State != RunComplete || len(completed.Runs[0].Attempts) != 2 || completed.Model.Runs[0].Resume != nil {
		t.Fatalf("completed run = ids %d, attempts %d, model %+v, run %+v", ids, attempts, completed.Model.Runs, completed.Runs[0])
	}
	if len(completed.Runs[0].Observation.Checkpoints) != 1 || completed.Runs[0].Observation.Checkpoints[0].Step != 1 {
		t.Fatalf("completed observation = %+v", completed.Runs[0].Observation)
	}
}

func TestComposeReplacementIsTransactional(t *testing.T) {
	root := t.TempDir()
	compose := validCompose()
	stage := preparedFixture(t, compose.Stages[0])
	builder := Builder{Root: root, NewID: func() (string, error) { return "run0001", nil }, Resolver: training.FakeResolver()}
	first, err := builder.Compose(context.Background(), "smoke", compose, []PreparedStage{stage}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Compose(context.Background(), "smoke", compose, []PreparedStage{stage}, false); err == nil || !strings.Contains(err.Error(), "--replace") {
		t.Fatalf("second compose error = %v", err)
	}
	failingBackend := backendFunc(func(context.Context, training.Request) (training.Observation, error) {
		return training.Observation{}, errors.New("replacement failed")
	})
	builder.Resolver = training.ResolverFunc(func(context.Context, training.ResolveRequest) (training.Selection, error) {
		return testSelection(failingBackend), nil
	})
	if _, err := builder.Compose(context.Background(), "smoke", compose, []PreparedStage{stage}, true); err == nil {
		t.Fatal("replacement succeeded")
	}
	after, err := Inspect(root, "smoke")
	if err != nil {
		t.Fatal(err)
	}
	if after.Model.ID != first.Model.ID || after.Model.Runs[0].State != RunComplete {
		t.Fatalf("replacement changed original: %+v", after.Model)
	}
}

func TestComposeResumesDurableTransactionAfterInterruption(t *testing.T) {
	root := t.TempDir()
	compose := validCompose()
	stage := preparedFixture(t, compose.Stages[0])
	attempts := 0
	backend := backendFunc(func(_ context.Context, request training.Request) (training.Observation, error) {
		attempts++
		if attempts == 1 {
			return training.Observation{}, context.Canceled
		}
		data := []byte("resumed compose weights")
		path := filepath.Join(request.ArtifactDirectory, "model.safetensors")
		if err := os.MkdirAll(request.ArtifactDirectory, 0o755); err != nil {
			return training.Observation{}, err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return training.Observation{}, err
		}
		digest := sha256.Sum256(data)
		loss := 0.5
		return training.Observation{
			Steps: 2, ConsumedTokens: 128, FinalLoss: &loss,
			Evaluations: []training.Evaluation{{Step: 2, Tokens: 128, Metrics: map[string]float64{"heldout_loss": 0.75}}},
			Artifacts:   []training.Artifact{{Path: "artifacts/model.safetensors", SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(data))}},
		}, nil
	})
	ids := 0
	builder := Builder{Root: root, NewID: func() (string, error) {
		ids++
		return "compose0001", nil
	}, Resolver: training.ResolverFunc(func(context.Context, training.ResolveRequest) (training.Selection, error) {
		return testSelection(backend), nil
	})}
	if _, err := builder.Compose(context.Background(), "smoke", compose, []PreparedStage{stage}, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("first Compose error = %v", err)
	}
	interrupted, err := Inspect(root, "smoke")
	if err != nil || len(interrupted.Runs) != 1 || interrupted.Runs[0].State != RunInterrupted {
		t.Fatalf("interrupted compose model = %+v, err = %v", interrupted, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".waldo-compose"))
	if err != nil || len(entries) < 2 {
		t.Fatalf("durable staging entries = %v, err = %v", entries, err)
	}
	listed, err := List(root, nil)
	if err != nil || len(listed) != 1 || listed[0].Name != "smoke" || listed[0].State != string(RunInterrupted) || listed[0].Path != filepath.Join(root, "smoke") {
		t.Fatalf("active compose listing = %+v, err = %v", listed, err)
	}
	completed, err := builder.Compose(context.Background(), "smoke", compose, []PreparedStage{stage}, false)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || ids != 1 || len(completed.Runs) != 1 || completed.Runs[0].State != RunComplete || len(completed.Runs[0].Attempts) != 2 {
		t.Fatalf("completed compose: attempts %d ids %d run %+v", attempts, ids, completed.Runs)
	}
	entries, err = os.ReadDir(filepath.Join(root, ".waldo-compose"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("completed compose retained workspace %s", entry.Name())
		}
	}
}

func TestComposeCancellationDuringPreflightRemainsListed(t *testing.T) {
	root := t.TempDir()
	compose := validCompose()
	stage := preparedFixture(t, compose.Stages[0])
	builder := Builder{Root: root, NewID: func() (string, error) { return "preflight0001", nil }, Resolver: training.FakeResolver()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := builder.Compose(ctx, "smoke", compose, []PreparedStage{stage}, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Compose error = %v", err)
	}
	listed, err := List(root, nil)
	if err != nil || len(listed) != 1 || listed[0].Name != "smoke" || listed[0].Path != filepath.Join(root, "smoke") {
		t.Fatalf("preflight-canceled compose listing = %+v, err = %v", listed, err)
	}
	inspection, err := Inspect(root, "smoke")
	if err != nil || len(inspection.Runs) != 0 {
		t.Fatalf("preflight-canceled model = %+v, err = %v", inspection, err)
	}
	completed, err := builder.Compose(context.Background(), "smoke", compose, []PreparedStage{stage}, false)
	if err != nil || len(completed.Runs) != 1 || completed.Runs[0].State != RunComplete {
		t.Fatalf("resumed preflight compose = %+v, err = %v", completed, err)
	}
}

func TestListExportAndRemoveModels(t *testing.T) {
	root := t.TempDir()
	builder := Builder{Root: root}
	if _, err := builder.Initialize("alpha", testArchitecture()); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Initialize("beta", testArchitecture()); err != nil {
		t.Fatal(err)
	}
	listed, err := List(root, []string{"a*"})
	if err != nil || len(listed) != 1 || listed[0].Name != "alpha" {
		t.Fatalf("listed = %+v, err = %v", listed, err)
	}
	destination := filepath.Join(t.TempDir(), "alpha-export")
	if _, err := Export(root, "alpha", destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "BOM.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "MODEL-BOM.json")); !os.IsNotExist(err) {
		t.Fatalf("native export retained internal BOM name: %v", err)
	}
	if _, err := Export(root, "alpha", filepath.Join(root, "alpha", "recursive-export")); err == nil {
		t.Fatal("Export accepted a destination inside its source model")
	}
	if _, err := Inspect(t.TempDir(), destination); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(root, []string{"alpha", "beta"}); err != nil {
		t.Fatal(err)
	}
	listed, err = List(root, nil)
	if err != nil || len(listed) != 0 {
		t.Fatalf("listed = %+v, err = %v", listed, err)
	}
}

func validCompose() Compose {
	return Compose{Kind: "waldo-model-compose", Schema: 1, Architecture: testArchitecture(), Stages: []Stage{testStage("pretrain")}}
}

func testArchitecture() Architecture {
	return Architecture{Family: "decoder-transformer", ContextTokens: 128, VocabularySize: 256, HiddenSize: 64, IntermediateSize: 192, Layers: 2, AttentionHeads: 4, KeyValueHeads: 2, TieEmbeddings: true, ParameterDType: "float32", Tokenizer: Tokenizer{Name: "byte", Revision: "sha256:example"}}
}

func testStage(name string) Stage {
	return Stage{Name: name, Type: "pre-training", Objective: "causal-language-modeling", Corpora: []string{"example"}, Parameters: training.Parameters{Steps: 2, BatchSize: 1, SequenceLength: 64, LearningRate: 0.001, Seed: 7}}
}

func preparedFixture(t *testing.T, stage Stage) PreparedStage {
	t.Helper()
	text := "canonical parquet fixture"
	var encoded bytes.Buffer
	writer := parquet.NewGenericWriter[shard.Row](&encoded)
	secondText := text + " second"
	if _, err := writer.Write([]shard.Row{
		{SHA256: record.TextHash(text), Kind: record.KindPretrain, Text: text, Source: "fixture", License: "CC0-1.0", Tokens: 128},
		{SHA256: record.TextHash(secondText), Kind: record.KindPretrain, Text: secondText, Source: "fixture", License: "CC0-1.0", Tokens: 128},
	}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	digestArray := sha256.Sum256(data)
	digest := hex.EncodeToString(digestArray[:])
	path := filepath.Join(t.TempDir(), digest)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	conversion := index.Conversion{Tool: "test", Version: "1", Profile: "text", Recipe: "test/v1", Tokenizer: "byte"}
	measures := index.Measures{Shards: 1, Docs: 2, Tokens: 256, Bytes: int64(len(data))}
	bom := corpus.BOM{
		Kind: "openwaldo-bom", Schema: 1, Subject: "corpus", Paths: []string{"example"}, Licenses: map[string]index.Measures{"CC0-1.0": measures}, Totals: measures,
		Manifests: []corpus.ManifestPin{{Path: "example/example.json", SHA256: strings.Repeat("a", 64), Name: "example", Title: "Example", Description: "Model fixture.", License: "CC0-1.0", Format: "parquet", RecordSchema: 1, ConvertedBy: conversion, Sources: []index.Source{{Name: "fixture", Source: "Fixture", URL: "https://example.test", SHA256: strings.Repeat("b", 64)}}, Totals: measures, Licenses: map[string]index.Measures{"CC0-1.0": measures}}},
		Shards:    []corpus.ShardPin{{Manifest: "example/example.json", URL: "https://objects.example/" + digest, SHA256: digest, Format: "parquet", RecordSchema: 1, License: "CC0-1.0", ConvertedBy: conversion, Docs: 2, Tokens: 256, Bytes: int64(len(data))}},
	}
	prepared, err := PrepareStage(stage, bom, []training.Input{{Path: path, SHA256: digest, Bytes: int64(len(data))}})
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func composeYAML(suffix string) string {
	return "kind: waldo-model-compose\nschema: 1\n" +
		"architecture:\n  family: decoder-transformer\n  context_tokens: 128\n  vocabulary_size: 256\n  hidden_size: 64\n  intermediate_size: 192\n  layers: 2\n  attention_heads: 4\n  key_value_heads: 2\n  tie_embeddings: true\n  parameter_dtype: float32\n  tokenizer:\n    name: byte\n    revision: sha256:example\n" +
		"stages:\n  - name: pretrain\n    type: pre-training\n    objective: causal-language-modeling\n    corpora:\n      - core/books\n      - science/papers\n    parameters:\n      steps: 2\n      batch_size: 1\n      sequence_length: 64\n      learning_rate: 0.001\n      seed: 7\n" + suffix
}

func testSelection(backend training.Backend) training.Selection {
	descriptor := backend.Descriptor()
	return training.Selection{Backend: backend, Execution: training.Execution{Backend: descriptor.Identity, Framework: descriptor.Framework, Runtime: "test", Host: training.Host{OS: "test-os", Architecture: "test-arch"}, Nodes: 1, WorldSize: 1}}
}
