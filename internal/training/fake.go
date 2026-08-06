package training

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/openwaldo/waldo/internal/corpus"
)

const FakeRevision = "builtin-fake-schema-1-r2"

// Fake proves orchestration and provenance. Its artifact explicitly states
// that it is not trained weights and must never be accepted by a real backend.
type Fake struct{}

func (Fake) Descriptor() Descriptor {
	return Descriptor{
		Identity:  Identity{Name: "fake", Revision: FakeRevision},
		Framework: "fake",
		Capabilities: Capabilities{
			Objectives: []string{"causal-language-modeling"}, CheckpointResume: true,
		},
	}
}

func (Fake) Run(ctx context.Context, request Request) (Observation, error) {
	if err := ctx.Err(); err != nil {
		return Observation{}, err
	}
	if request.Resume != nil && request.Resume.Step > request.Parameters.Steps {
		return Observation{}, fmt.Errorf("fake resume step %d is beyond target step %d", request.Resume.Step, request.Parameters.Steps)
	}
	payload, err := json.MarshalIndent(struct {
		Kind         string             `json:"kind"`
		Schema       int                `json:"schema"`
		Warning      string             `json:"warning"`
		Architecture string             `json:"architecture_sha256"`
		BOM          corpus.BOM         `json:"corpus_bom"`
		Parameters   ResolvedParameters `json:"parameters"`
	}{
		Kind: "waldo-fake-artifact", Schema: 1,
		Warning:      "simulation only; this file contains no trained model weights",
		Architecture: request.ArchitectureSHA256, BOM: request.BOM, Parameters: request.Parameters,
	}, "", "  ")
	if err != nil {
		return Observation{}, err
	}
	if request.Records == nil {
		return Observation{}, fmt.Errorf("fake backend received no canonical record stream")
	}
	var records int64
	if err := request.Records.Stream(ctx, func(Record) error {
		records++
		return nil
	}); err != nil {
		return Observation{}, err
	}
	expectedTrainingRecords := (request.BOM.Totals.Docs - request.EvaluationSet.Records) * request.Parameters.Epochs
	if records != expectedTrainingRecords {
		return Observation{}, fmt.Errorf("canonical training stream contains %d records, expected %d after held-out selection", records, expectedTrainingRecords)
	}
	var evaluationRecords int64
	if request.EvaluationRecords != nil {
		if err := request.EvaluationRecords.Stream(ctx, func(Record) error { evaluationRecords++; return nil }); err != nil {
			return Observation{}, err
		}
	}
	if evaluationRecords != request.EvaluationSet.Records {
		return Observation{}, fmt.Errorf("canonical evaluation stream contains %d records, run BOM pins %d", evaluationRecords, request.EvaluationSet.Records)
	}
	payload = append(payload, '\n')
	artifactName := "fake-model.json"
	outputPath := filepath.Join(request.ArtifactDirectory, artifactName)
	if err := writeArtifactAtomic(outputPath, payload); err != nil {
		return Observation{}, fmt.Errorf("write fake artifact: %w", err)
	}
	digest := sha256.Sum256(payload)
	capacity := request.Parameters.PlannedTokenCapacity
	if capacity > request.BOM.Totals.Tokens {
		capacity = request.BOM.Totals.Tokens
	}
	loss := 1.0
	if request.Report != nil {
		request.Report(Event{Kind: "progress", Message: "simulated training profile completed", Step: request.Parameters.Steps, Tokens: capacity, Loss: &loss})
	}
	var checkpoints []Checkpoint
	if request.Parameters.CheckpointEvery > 0 {
		checkpointName := fmt.Sprintf("checkpoints/step-%06d.json", request.Parameters.Steps)
		checkpointPayload := []byte(fmt.Sprintf("{\"kind\":\"waldo-fake-checkpoint\",\"schema\":1,\"step\":%d}\n", request.Parameters.Steps))
		checkpointPath := filepath.Join(request.ArtifactDirectory, filepath.FromSlash(checkpointName))
		if err := writeArtifactAtomic(checkpointPath, checkpointPayload); err != nil {
			return Observation{}, err
		}
		checkpointDigest := sha256.Sum256(checkpointPayload)
		checkpoint := Checkpoint{Step: request.Parameters.Steps, Tokens: capacity, Artifacts: []Artifact{{Path: filepath.ToSlash(filepath.Join(request.ArtifactPrefix, checkpointName)), SHA256: hex.EncodeToString(checkpointDigest[:]), Bytes: int64(len(checkpointPayload))}}}
		checkpoints = append(checkpoints, checkpoint)
		if request.Report != nil {
			request.Report(Event{Kind: "checkpoint", Message: "simulated checkpoint persisted", Step: checkpoint.Step, Tokens: checkpoint.Tokens, Checkpoint: &checkpoint})
		}
	}
	var evaluations []Evaluation
	if request.Parameters.EvaluateEvery > 0 && request.EvaluationSet.Records > 0 {
		evaluation := Evaluation{Step: request.Parameters.Steps, Tokens: capacity, Metrics: map[string]float64{"heldout_loss": loss, "heldout_perplexity": math.Exp(loss)}}
		evaluations = append(evaluations, evaluation)
		if request.Report != nil {
			request.Report(Event{Kind: "evaluation", Message: "simulated evaluation completed", Step: evaluation.Step, Tokens: evaluation.Tokens, Evaluation: &evaluation})
		}
	}
	return Observation{
		Simulated: true, Steps: request.Parameters.Steps, ConsumedTokens: capacity, FinalLoss: &loss,
		Checkpoints: checkpoints, Evaluations: evaluations,
		Artifacts: []Artifact{{Path: filepath.ToSlash(filepath.Join(request.ArtifactPrefix, artifactName)), SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(payload))}},
	}, nil
}

func writeArtifactAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".waldo-artifact-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}
