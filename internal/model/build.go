// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/openwaldo/waldo/internal/training"
)

type Progress struct {
	Phase    string          `json:"phase"`
	Stage    string          `json:"stage,omitempty"`
	RunID    string          `json:"run_id,omitempty"`
	State    RunState        `json:"state,omitempty"`
	Message  string          `json:"message"`
	Training *training.Event `json:"training,omitempty"`
}

type Builder struct {
	Root     string
	Now      func() time.Time
	NewID    func() (string, error)
	Resolver training.Resolver
	Progress func(Progress)
}

func ValidateName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("model name must match %s", validName.String())
	}
	return nil
}

// Initialize creates an untrained model with an immutable architecture.
func (builder Builder) Initialize(name string, architecture Architecture) (Inspection, error) {
	if builder.Root == "" {
		return Inspection{}, fmt.Errorf("model root is required")
	}
	if err := ValidateName(name); err != nil {
		return Inspection{}, err
	}
	if err := architecture.Validate(); err != nil {
		return Inspection{}, err
	}
	modelPath := filepath.Join(builder.Root, name)
	if _, err := os.Stat(modelPath); err == nil {
		return Inspection{}, fmt.Errorf("model %q already exists", name)
	} else if !os.IsNotExist(err) {
		return Inspection{}, err
	}
	plan, err := composePlan(name, Compose{Architecture: architecture})
	if err != nil {
		return Inspection{}, err
	}
	planHash, err := hashJSON(plan)
	if err != nil {
		return Inspection{}, err
	}
	now := builder.clock()
	created := now()
	record := ModelRecord{
		Kind: "waldo-model", Schema: ModelSchema, ID: planHash, Name: name,
		PlanSHA256: planHash, ArchitectureSHA256: plan.ArchitectureSHA256,
		Architecture: plan.Architecture, Forecast: plan.Forecast,
		Created: formatTime(created), Updated: formatTime(created),
	}
	if err := initializeModel(builder.Root, modelPath, plan, record); err != nil {
		return Inspection{}, err
	}
	builder.report(Progress{Phase: "model", Message: "persisted immutable model architecture and OpenWALDO BOM"})
	return Inspect(builder.Root, name)
}

// Train appends one explicit run to an existing model. The model architecture
// and identity remain unchanged; the aggregate model BOM gains a run pin.
func (builder Builder) Train(ctx context.Context, name string, prepared PreparedStage) (Inspection, error) {
	inspection, err := Inspect(builder.Root, name)
	if err != nil {
		return Inspection{}, err
	}
	stage := prepared.Stage
	prepared, err = PrepareStage(stage, prepared.BOM, prepared.Inputs)
	if err != nil {
		return Inspection{}, err
	}
	if err := validateStage(stage, inspection.Model.Architecture); err != nil {
		return Inspection{}, err
	}
	if err := prepared.BOM.Validate(); err != nil {
		return Inspection{}, fmt.Errorf("stage %s corpus OpenWALDO BOM: %w", stage.Name, err)
	}
	if len(prepared.Inputs) == 0 {
		return Inspection{}, fmt.Errorf("stage %s has no verified shard inputs", stage.Name)
	}
	resolvedParameters, err := training.ResolveParameters(stage.Parameters)
	if err != nil {
		return Inspection{}, fmt.Errorf("stage %s training profile: %w", stage.Name, err)
	}
	partition, err := training.NewRecordPartition(prepared.Inputs, resolvedParameters)
	if err != nil {
		return Inspection{}, fmt.Errorf("stage %s held-out evaluation partition: %w", stage.Name, err)
	}
	records, err := partition.TrainingRecords()
	if err != nil {
		return Inspection{}, fmt.Errorf("stage %s training record stream: %w", stage.Name, err)
	}
	evaluationRecords := partition.EvaluationRecords()
	architectureJSON, err := json.Marshal(inspection.Model.Architecture)
	if err != nil {
		return Inspection{}, err
	}
	resolver := builder.Resolver
	if resolver == nil {
		resolver = builtinResolver()
	}
	selection, err := resolver.Resolve(ctx, training.ResolveRequest{
		ArchitectureSHA256: inspection.Model.ArchitectureSHA256,
		Architecture:       architectureJSON,
		Objectives:         []string{stage.Objective},
	})
	if err != nil {
		return Inspection{}, fmt.Errorf("resolve training backend: %w", err)
	}
	if err := validateSelection(selection, []string{stage.Objective}); err != nil {
		return Inspection{}, err
	}
	var initialization *training.Initialization
	if selection.Execution.Backend.Name != "fake" {
		initialization, err = resolveInitialization(inspection)
		if err != nil {
			return Inspection{}, err
		}
	}

	bomHash, err := hashJSON(prepared.BOM)
	if err != nil {
		return Inspection{}, err
	}
	if candidate, ok := resumableRun(inspection, stage, resolvedParameters, partition.Evaluation, bomHash, selection.Execution); ok {
		return builder.resumeTraining(ctx, name, inspection, candidate, stage, prepared, records, evaluationRecords, architectureJSON, selection)
	}

	runID, err := builder.identifier()()
	if err != nil {
		return Inspection{}, err
	}
	ordinal := len(inspection.Model.Runs) + 1
	pin := RunPin{ID: runID, Stage: stage.Name, Ordinal: ordinal, State: RunPlanned, Backend: selection.Execution.Backend, Simulated: selection.Execution.Backend.Name == training.BackendFake}
	runDirectory := filepath.Join(inspection.Path, "runs", runDirectoryName(pin))
	runBOM := RunBOM{
		Kind: "openwaldo-bom", Schema: RunBOMSchema, Subject: "training-run",
		ID: runID, ModelID: inspection.Model.ID, Stage: stage.Name, StageType: stage.Type,
		Ordinal: ordinal, Objective: stage.Objective, Execution: selection.Execution,
		ArchitectureSHA256: inspection.Model.ArchitectureSHA256,
		CorpusBOMSHA256:    bomHash, CorpusBOM: prepared.BOM, Parameters: resolvedParameters,
		EvaluationSet: &partition.Evaluation, Initialization: initialization,
	}
	runBOMHash, err := hashJSON(runBOM)
	if err != nil {
		return Inspection{}, err
	}
	pin.BOMSHA256 = runBOMHash
	now := builder.clock()
	run := RunRecord{Kind: "waldo-training-run", Schema: RunSchema, ID: runID, State: RunPlanned, BOMSHA256: runBOMHash, Planned: formatTime(now())}
	if err := writeJSONAtomic(filepath.Join(runDirectory, "RUN-BOM.json"), runBOM); err != nil {
		return Inspection{}, err
	}
	if err := writeJSONAtomic(filepath.Join(runDirectory, "RUN.json"), run); err != nil {
		return Inspection{}, err
	}
	record := inspection.Model
	record.Runs = append(record.Runs, pin)
	if err := persistModel(inspection.Path, &record, now()); err != nil {
		return Inspection{}, err
	}
	builder.report(Progress{Phase: "run", Stage: pin.Stage, RunID: runID, State: RunPlanned, Message: "persisted run OpenWALDO BOM"})
	return builder.executeTrainingAttempt(ctx, name, inspection.Path, &record, pin, run, runBOM, stage, prepared, records, evaluationRecords, architectureJSON, selection, nil)
}

func resumableRun(inspection Inspection, stage Stage, parameters training.ResolvedParameters, evaluation training.EvaluationSet, corpusHash string, execution training.Execution) (int, bool) {
	if len(inspection.Runs) == 0 {
		return 0, false
	}
	index := len(inspection.Runs) - 1
	run := inspection.Runs[index]
	bom := inspection.RunBOMs[index]
	if run.State != RunInterrupted || bom.Stage != stage.Name || bom.StageType != stage.Type || bom.Objective != stage.Objective || bom.CorpusBOMSHA256 != corpusHash || !reflect.DeepEqual(bom.Parameters, parameters) || bom.EvaluationSet == nil || !reflect.DeepEqual(*bom.EvaluationSet, evaluation) || !reflect.DeepEqual(bom.Execution, execution) {
		return 0, false
	}
	return index, true
}

func (builder Builder) resumeTraining(ctx context.Context, name string, inspection Inspection, index int, stage Stage, prepared PreparedStage, records, evaluationRecords training.RecordSource, architectureJSON json.RawMessage, selection training.Selection) (Inspection, error) {
	pin := inspection.Model.Runs[index]
	run := inspection.Runs[index]
	runBOM := inspection.RunBOMs[index]
	if run.Progress != nil && len(run.Progress.Checkpoints) > 0 && !selection.Backend.Descriptor().Capabilities.CheckpointResume {
		return Inspection{}, fmt.Errorf("stage %s has a resumable checkpoint, but backend %s@%s cannot restore optimizer state", stage.Name, pin.Backend.Name, pin.Backend.Revision)
	}
	var resume *training.ResumePoint
	if pin.Resume != nil {
		resume = cloneResumePoint(pin.Resume)
	} else if run.Progress != nil && len(run.Progress.Checkpoints) > 0 {
		checkpoint := run.Progress.Checkpoints[len(run.Progress.Checkpoints)-1]
		resume = &training.ResumePoint{Step: checkpoint.Step, Tokens: checkpoint.Tokens, Checkpoint: checkpoint}
	}
	if resume != nil {
		runDirectory := filepath.Join(inspection.Path, "runs", runDirectoryName(pin))
		for _, artifact := range resume.Checkpoint.Artifacts {
			path := filepath.Join(runDirectory, filepath.FromSlash(artifact.Path))
			if err := verifyArtifactFile(path, artifact); err != nil {
				return Inspection{}, fmt.Errorf("resume run %s: %w", pin.ID, err)
			}
			resume.Paths = append(resume.Paths, path)
		}
	}
	if resume == nil && runBOM.Initialization != nil {
		resolved, err := resolveInitialization(inspection)
		if err != nil {
			return Inspection{}, err
		}
		if resolved == nil || resolved.SourceType != runBOM.Initialization.SourceType || resolved.SourceID != runBOM.Initialization.SourceID || resolved.SourceRunID != runBOM.Initialization.SourceRunID || !reflect.DeepEqual(resolved.Artifact, runBOM.Initialization.Artifact) {
			return Inspection{}, fmt.Errorf("resume run %s: pinned initialization is no longer the current verified source", pin.ID)
		}
		runBOM.Initialization = resolved
	}
	record := inspection.Model
	builder.report(Progress{Phase: "run", Stage: pin.Stage, RunID: pin.ID, State: RunInterrupted, Message: fmt.Sprintf("resuming existing run from step %d", resumeStep(resume))})
	return builder.executeTrainingAttempt(ctx, name, inspection.Path, &record, pin, run, runBOM, stage, prepared, records, evaluationRecords, architectureJSON, selection, resume)
}

func (builder Builder) executeTrainingAttempt(ctx context.Context, name, modelPath string, record *ModelRecord, pin RunPin, run RunRecord, runBOM RunBOM, stage Stage, prepared PreparedStage, records, evaluationRecords training.RecordSource, architectureJSON json.RawMessage, selection training.Selection, resume *training.ResumePoint) (Inspection, error) {
	now := builder.clock()
	runDirectory := filepath.Join(modelPath, "runs", runDirectoryName(pin))
	run.State = RunRunning
	run.Finished = ""
	run.Error = ""
	if run.Started == "" {
		run.Started = formatTime(now())
	}
	run.Attempts = append(run.Attempts, RunAttempt{Ordinal: len(run.Attempts) + 1, Started: formatTime(now()), State: RunRunning, ResumeStep: resumeStep(resume)})
	if err := persistRunAndPin(modelPath, runDirectory, record, pin, run, now()); err != nil {
		return Inspection{}, err
	}
	builder.report(Progress{Phase: "run", Stage: pin.Stage, RunID: pin.ID, State: RunRunning, Message: "training backend started"})

	var progressMutex sync.Mutex
	var progressErr error
	report := func(event training.Event) {
		progressMutex.Lock()
		defer progressMutex.Unlock()
		if progressErr == nil {
			progressErr = persistTrainingEvent(modelPath, runDirectory, record, pin, &run, event, now())
		}
		builder.report(Progress{Phase: "training", Stage: stage.Name, RunID: pin.ID, State: RunRunning, Message: event.Message, Training: &event})
	}
	artifactPrefix := "artifacts"
	observation, backendErr := selection.Backend.Run(ctx, training.Request{
		RunID: pin.ID, Stage: stage.Name, Objective: stage.Objective,
		ArchitectureSHA256: record.ArchitectureSHA256,
		Architecture:       architectureJSON, BOM: prepared.BOM, Inputs: prepared.Inputs,
		Parameters: runBOM.Parameters, Records: records, EvaluationRecords: evaluationRecords, EvaluationSet: evaluationSetValue(runBOM.EvaluationSet), Initialization: initializationForAttempt(runBOM.Initialization, resume), Resume: resume,
		ArtifactDirectory: filepath.Join(runDirectory, artifactPrefix), ArtifactPrefix: artifactPrefix, Report: report,
	})
	progressMutex.Lock()
	if progressErr != nil {
		backendErr = errors.Join(backendErr, fmt.Errorf("persist training progress: %w", progressErr))
	}
	progressMutex.Unlock()
	if backendErr == nil {
		observation = mergeProgress(run.Progress, observation)
		planned := PlannedStage{Name: stage.Name, Parameters: stage.Parameters, PlannedTokens: runBOM.Parameters.PlannedTokenCapacity}
		if err := validateBackendObservation(runDirectory, planned, observation); err != nil {
			backendErr = fmt.Errorf("invalid backend observation: %w", err)
		} else if set := runBOM.EvaluationSet; set != nil && set.Records > 0 && runBOM.Parameters.EvaluateEvery > 0 {
			if len(observation.Evaluations) == 0 {
				backendErr = fmt.Errorf("invalid backend observation: held-out evaluation was configured but no metrics were reported")
			} else {
				for index, evaluation := range observation.Evaluations {
					if _, ok := evaluation.Metrics["heldout_loss"]; !ok {
						backendErr = fmt.Errorf("invalid backend observation: evaluation %d does not report heldout_loss", index+1)
						break
					}
				}
			}
		}
	}
	run.Finished = formatTime(now())
	attempt := &run.Attempts[len(run.Attempts)-1]
	attempt.Finished = run.Finished
	if backendErr != nil {
		run.State = RunFailed
		if errors.Is(backendErr, context.Canceled) || errors.Is(backendErr, context.DeadlineExceeded) {
			run.State = RunInterrupted
		}
		run.Error = backendErr.Error()
		attempt.State = run.State
		attempt.Error = run.Error
		if err := persistRunAndPin(modelPath, runDirectory, record, pin, run, now()); err != nil {
			return Inspection{}, errors.Join(backendErr, err)
		}
		builder.report(Progress{Phase: "run", Stage: pin.Stage, RunID: pin.ID, State: run.State, Message: run.Error})
		return Inspection{}, fmt.Errorf("stage %s: %w", pin.Stage, backendErr)
	}
	run.State = RunComplete
	attempt.State = RunComplete
	run.Observation = &observation
	run.Progress = nil
	clearResumePin(record, pin.ID)
	if err := persistRunAndPin(modelPath, runDirectory, record, pin, run, now()); err != nil {
		return Inspection{}, err
	}
	builder.report(Progress{Phase: "run", Stage: pin.Stage, RunID: pin.ID, State: RunComplete, Message: "persisted training observations and artifact hashes"})
	return Inspect(builder.Root, name)
}

func resumeStep(resume *training.ResumePoint) int64 {
	if resume == nil {
		return 0
	}
	return resume.Step
}

func evaluationSetValue(value *training.EvaluationSet) training.EvaluationSet {
	if value == nil {
		return training.EvaluationSet{}
	}
	return *value
}

func initializationForAttempt(initialization *training.Initialization, resume *training.ResumePoint) *training.Initialization {
	if resume != nil {
		return nil
	}
	return initialization
}

func cloneResumePoint(value *training.ResumePoint) *training.ResumePoint {
	if value == nil {
		return nil
	}
	clone := *value
	clone.Checkpoint.Artifacts = append([]training.Artifact(nil), value.Checkpoint.Artifacts...)
	clone.Paths = append([]string(nil), value.Paths...)
	return &clone
}

func clearResumePin(record *ModelRecord, runID string) {
	for index := range record.Runs {
		if record.Runs[index].ID == runID {
			record.Runs[index].Resume = nil
			return
		}
	}
}

func persistTrainingEvent(modelPath, runDirectory string, record *ModelRecord, pin RunPin, run *RunRecord, event training.Event, now time.Time) error {
	if event.Kind != "checkpoint" && event.Kind != "evaluation" {
		return nil
	}
	if run.Progress == nil {
		run.Progress = &training.Progress{}
	}
	run.Progress.Steps = max(run.Progress.Steps, event.Step)
	run.Progress.ConsumedTokens = max(run.Progress.ConsumedTokens, event.Tokens)
	if event.Loss != nil {
		loss := *event.Loss
		run.Progress.LastLoss = &loss
	}
	if event.Checkpoint != nil {
		checkpoint := *event.Checkpoint
		checkpoint.Artifacts = append([]training.Artifact(nil), event.Checkpoint.Artifacts...)
		if len(run.Progress.Checkpoints) > 0 && checkpoint.Step <= run.Progress.Checkpoints[len(run.Progress.Checkpoints)-1].Step {
			if reflect.DeepEqual(checkpoint, run.Progress.Checkpoints[len(run.Progress.Checkpoints)-1]) {
				return nil
			}
			return fmt.Errorf("checkpoint step %d does not advance durable progress", checkpoint.Step)
		}
		for _, artifact := range checkpoint.Artifacts {
			clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(artifact.Path)))
			if artifact.Path == "" || filepath.IsAbs(filepath.FromSlash(artifact.Path)) || clean != artifact.Path || !strings.HasPrefix(clean, "artifacts/checkpoints/") {
				return fmt.Errorf("checkpoint step %d artifact path %q is not canonical beneath artifacts/checkpoints/", checkpoint.Step, artifact.Path)
			}
			if err := verifyArtifactFile(filepath.Join(runDirectory, filepath.FromSlash(artifact.Path)), artifact); err != nil {
				return fmt.Errorf("checkpoint step %d: %w", checkpoint.Step, err)
			}
		}
		run.Progress.Checkpoints = append(run.Progress.Checkpoints, checkpoint)
		resume := &training.ResumePoint{Step: checkpoint.Step, Tokens: checkpoint.Tokens, Checkpoint: checkpoint}
		for index := range record.Runs {
			if record.Runs[index].ID == pin.ID {
				record.Runs[index].Resume = resume
				break
			}
		}
	}
	if event.Evaluation != nil {
		evaluation := *event.Evaluation
		evaluation.Metrics = cloneMetrics(event.Evaluation.Metrics)
		if len(run.Progress.Evaluations) > 0 && evaluation.Step <= run.Progress.Evaluations[len(run.Progress.Evaluations)-1].Step {
			if reflect.DeepEqual(evaluation, run.Progress.Evaluations[len(run.Progress.Evaluations)-1]) {
				return nil
			}
			return fmt.Errorf("evaluation step %d does not advance durable progress", evaluation.Step)
		}
		run.Progress.Evaluations = append(run.Progress.Evaluations, evaluation)
	}
	return persistRunAndPin(modelPath, runDirectory, record, pin, *run, now)
}

func mergeProgress(progress *training.Progress, observation training.Observation) training.Observation {
	if progress == nil {
		return observation
	}
	checkpoints := append([]training.Checkpoint(nil), progress.Checkpoints...)
	for _, checkpoint := range observation.Checkpoints {
		if len(checkpoints) == 0 || checkpoint.Step > checkpoints[len(checkpoints)-1].Step {
			checkpoints = append(checkpoints, checkpoint)
		}
	}
	evaluations := append([]training.Evaluation(nil), progress.Evaluations...)
	for _, evaluation := range observation.Evaluations {
		if len(evaluations) == 0 || evaluation.Step > evaluations[len(evaluations)-1].Step {
			evaluations = append(evaluations, evaluation)
		}
	}
	observation.Checkpoints = checkpoints
	observation.Evaluations = evaluations
	return observation
}

func cloneMetrics(metrics map[string]float64) map[string]float64 {
	result := make(map[string]float64, len(metrics))
	for name, value := range metrics {
		result[name] = value
	}
	return result
}

func resolveInitialization(inspection Inspection) (*training.Initialization, error) {
	for index := len(inspection.Runs) - 1; index >= 0; index-- {
		run := inspection.Runs[index]
		if run.State != RunComplete || run.Observation == nil || run.Observation.Simulated {
			continue
		}
		for _, artifact := range run.Observation.Artifacts {
			if artifact.Path != "artifacts/model.safetensors" {
				continue
			}
			var pin RunPin
			for _, candidate := range inspection.Model.Runs {
				if candidate.ID == run.ID {
					pin = candidate
					break
				}
			}
			if pin.ID == "" {
				return nil, fmt.Errorf("initialize from run %s: model run pin is missing", run.ID)
			}
			path := filepath.Join(inspection.Path, "runs", runDirectoryName(pin), filepath.FromSlash(artifact.Path))
			if err := verifyArtifactFile(path, artifact); err != nil {
				return nil, fmt.Errorf("initialize from run %s: %w", run.ID, err)
			}
			return &training.Initialization{SourceType: "run", SourceID: run.ID, SourceRunID: run.ID, Artifact: artifact, Path: path}, nil
		}
	}
	if inspection.Origin != nil {
		for _, artifact := range inspection.Origin.Artifacts {
			if artifact.Role != "weights" {
				continue
			}
			path := filepath.Join(inspection.Path, filepath.FromSlash(artifact.Path))
			trainingArtifact := training.Artifact{Path: artifact.Path, SHA256: artifact.SHA256, Bytes: artifact.Bytes}
			if err := verifyArtifactFile(path, trainingArtifact); err != nil {
				return nil, fmt.Errorf("initialize from model origin %s: %w", inspection.Model.OriginBOMSHA256, err)
			}
			return &training.Initialization{SourceType: "origin", SourceID: inspection.Model.OriginBOMSHA256, Artifact: trainingArtifact, Path: path}, nil
		}
		return nil, fmt.Errorf("model origin %s has no weights artifact", inspection.Model.OriginBOMSHA256)
	}
	return nil, nil
}

type composeTransaction struct {
	Kind          string                    `json:"kind"`
	Schema        int                       `json:"schema"`
	Name          string                    `json:"name"`
	Compose       Compose                   `json:"compose"`
	CorpusBOMs    []composeTransactionStage `json:"corpus_boms"`
	Replace       bool                      `json:"replace"`
	TargetModelID string                    `json:"target_model_id,omitempty"`
	TargetSHA256  string                    `json:"target_sha256,omitempty"`
}

type composeTransactionStage struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// Compose creates a model and executes every prepared stage. Work is built in
// a content-identified durable sibling workspace so interruption can resume
// while --replace continues to leave the published model untouched.
func (builder Builder) Compose(ctx context.Context, name string, compose Compose, stages []PreparedStage, replace bool) (Inspection, error) {
	if err := compose.Validate(); err != nil {
		return Inspection{}, err
	}
	if err := ValidateName(name); err != nil {
		return Inspection{}, err
	}
	if len(stages) != len(compose.Stages) {
		return Inspection{}, fmt.Errorf("compose has %d stages but %d prepared stages", len(compose.Stages), len(stages))
	}
	for index := range stages {
		if !reflect.DeepEqual(stages[index].Stage, compose.Stages[index]) {
			return Inspection{}, fmt.Errorf("prepared stage %d does not match model compose", index+1)
		}
	}
	if err := os.MkdirAll(builder.Root, 0o755); err != nil {
		return Inspection{}, err
	}
	destination := filepath.Join(builder.Root, name)
	var target *Inspection
	if _, err := os.Stat(destination); err == nil && !replace {
		return Inspection{}, fmt.Errorf("model %q already exists; use --replace to recreate it", name)
	} else if err == nil {
		inspected, inspectErr := Inspect(builder.Root, name)
		if inspectErr != nil {
			return Inspection{}, inspectErr
		}
		target = &inspected
	} else if err != nil && !os.IsNotExist(err) {
		return Inspection{}, err
	}
	var base *Inspection
	if compose.Base != nil {
		inspectedBase, err := Inspect(builder.Root, compose.Base.Model)
		if err != nil {
			return Inspection{}, fmt.Errorf("resolve compose base model %q: %w", compose.Base.Model, err)
		}
		base = &inspectedBase
		if base.Origin == nil || base.BOM.CurrentOriginSHA256 == "" {
			return Inspection{}, fmt.Errorf("compose base model %q must have pulled origin weights as its current weights", compose.Base.Model)
		}
		if compose.Base.OriginSHA256 != "" && compose.Base.OriginSHA256 != base.Model.OriginBOMSHA256 {
			return Inspection{}, fmt.Errorf("compose base model %q origin is %s, not requested %s", compose.Base.Model, base.Model.OriginBOMSHA256, compose.Base.OriginSHA256)
		}
		if !reflect.DeepEqual(compose.Architecture, base.Model.Architecture) {
			return Inspection{}, fmt.Errorf("compose architecture does not match base model %q", compose.Base.Model)
		}
		compose.Base.OriginSHA256 = base.Model.OriginBOMSHA256
	}
	transaction := composeTransaction{Kind: "waldo-model-compose-transaction", Schema: 1, Name: name, Compose: compose, Replace: replace}
	if target != nil {
		transaction.TargetModelID = target.Model.ID
		targetHash, hashErr := hashJSON(target.Model)
		if hashErr != nil {
			return Inspection{}, hashErr
		}
		transaction.TargetSHA256 = targetHash
	}
	for _, stage := range stages {
		digest, err := hashJSON(stage.BOM)
		if err != nil {
			return Inspection{}, err
		}
		transaction.CorpusBOMs = append(transaction.CorpusBOMs, composeTransactionStage{Name: stage.Stage.Name, SHA256: digest})
	}
	transactionID, err := hashJSON(transaction)
	if err != nil {
		return Inspection{}, err
	}
	stagingRoot := filepath.Join(builder.Root, ".waldo-compose")
	if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
		return Inspection{}, err
	}
	lock, err := lockComposeTransaction(filepath.Join(stagingRoot, transactionID+".lock"))
	if err != nil {
		return Inspection{}, fmt.Errorf("compose transaction %s: %w", transactionID[:12], err)
	}
	defer unlockComposeTransaction(lock)
	workspace := filepath.Join(stagingRoot, transactionID)
	workspaceModel := filepath.Join(workspace, name)
	resumingTransaction := false
	if _, err := os.Stat(workspace); os.IsNotExist(err) {
		if err := os.Mkdir(workspace, 0o755); err != nil {
			return Inspection{}, err
		}
		if err := writeJSONAtomic(filepath.Join(workspace, "COMPOSE.json"), transaction); err != nil {
			_ = os.RemoveAll(workspace)
			return Inspection{}, err
		}
	} else if err != nil {
		return Inspection{}, err
	} else {
		resumingTransaction = true
		var existing composeTransaction
		if err := readJSON(filepath.Join(workspace, "COMPOSE.json"), &existing); err != nil || !reflect.DeepEqual(existing, transaction) {
			return Inspection{}, fmt.Errorf("compose staging %s does not match transaction %s", workspace, transactionID[:12])
		}
	}
	if resumingTransaction {
		builder.report(Progress{Phase: "compose", Message: fmt.Sprintf("resuming durable transaction %s", transactionID[:12])})
	}
	stagedBuilder := builder
	stagedBuilder.Root = workspace
	if _, err := os.Stat(workspaceModel); os.IsNotExist(err) {
		if compose.Base == nil {
			if _, err := stagedBuilder.Initialize(name, compose.Architecture); err != nil {
				_ = os.RemoveAll(workspace)
				return Inspection{}, err
			}
		} else if _, err := stagedBuilder.initializeFromOrigin(name, compose, *base); err != nil {
			_ = os.RemoveAll(workspace)
			return Inspection{}, err
		}
	} else if err != nil {
		return Inspection{}, err
	}
	staged, err := Inspect(workspace, name)
	if err != nil {
		return Inspection{}, fmt.Errorf("inspect compose staging %s: %w", workspace, err)
	}
	architectureHash, err := canonicalHash(compose.Architecture)
	if err != nil {
		return Inspection{}, err
	}
	if staged.Model.ArchitectureSHA256 != architectureHash || staged.Model.OriginBOMSHA256 != composeOriginHash(compose) {
		return Inspection{}, fmt.Errorf("compose staging %s does not match the requested architecture or origin", workspace)
	}
	if len(staged.Runs) > 0 && staged.Runs[len(staged.Runs)-1].State == RunRunning {
		if err := recoverAbandonedComposeRun(stagedBuilder, &staged); err != nil {
			return Inspection{}, err
		}
	}
	for index, stage := range stages {
		staged, err = Inspect(workspace, name)
		if err != nil {
			return Inspection{}, err
		}
		if index < len(staged.Runs) {
			if err := validateStagedComposeRun(staged, index, stage); err != nil {
				return Inspection{}, fmt.Errorf("compose staging %s: %w", workspace, err)
			}
			if staged.Runs[index].State == RunComplete {
				continue
			}
			if staged.Runs[index].State != RunInterrupted {
				_ = os.RemoveAll(workspace)
				return Inspection{}, fmt.Errorf("compose stage %s ended %s and cannot be resumed; its staging was cleared", stage.Stage.Name, staged.Runs[index].State)
			}
		}
		if _, err := stagedBuilder.Train(ctx, name, stage); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				builder.report(Progress{Phase: "compose", Message: fmt.Sprintf("retained transaction %s; repeat the exact command to resume", transactionID[:12])})
			} else {
				_ = os.RemoveAll(workspace)
			}
			return Inspection{}, err
		}
	}
	staged, err = Inspect(workspace, name)
	if err != nil {
		return Inspection{}, err
	}
	if len(staged.Runs) != len(stages) {
		return Inspection{}, fmt.Errorf("compose staging %s has %d runs for %d stages", workspace, len(staged.Runs), len(stages))
	}
	if target != nil {
		current, err := Inspect(builder.Root, name)
		currentHash := ""
		if err == nil {
			currentHash, err = hashJSON(current.Model)
		}
		if err != nil || current.Model.ID != transaction.TargetModelID || currentHash != transaction.TargetSHA256 {
			return Inspection{}, fmt.Errorf("published model %q changed while compose transaction %s was staged", name, transactionID[:12])
		}
	}
	newPath := workspaceModel
	backup := filepath.Join(builder.Root, fmt.Sprintf(".waldo-replaced-%s-%d", name, time.Now().UnixNano()))
	hadExisting := false
	if _, err := os.Stat(destination); err == nil {
		hadExisting = true
		if err := os.Rename(destination, backup); err != nil {
			return Inspection{}, fmt.Errorf("prepare replacement of model %q: %w", name, err)
		}
	}
	if err := os.Rename(newPath, destination); err != nil {
		if hadExisting {
			_ = os.Rename(backup, destination)
		}
		return Inspection{}, fmt.Errorf("publish model %q: %w", name, err)
	}
	if hadExisting {
		if err := os.RemoveAll(backup); err != nil {
			return Inspection{}, fmt.Errorf("remove replaced model backup %s: %w", backup, err)
		}
	}
	if err := os.RemoveAll(workspace); err != nil {
		return Inspection{}, fmt.Errorf("remove completed compose staging %s: %w", workspace, err)
	}
	builder.report(Progress{Phase: "compose", Message: fmt.Sprintf("published completed transaction %s", transactionID[:12])})
	return Inspect(builder.Root, name)
}

func composeOriginHash(compose Compose) string {
	if compose.Base == nil {
		return ""
	}
	return compose.Base.OriginSHA256
}

func lockComposeTransaction(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("another process owns this compose; wait for it to finish")
	}
	return file, nil
}

func unlockComposeTransaction(file *os.File) {
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}

func recoverAbandonedComposeRun(builder Builder, inspection *Inspection) error {
	position := len(inspection.Runs) - 1
	run := inspection.Runs[position]
	pin := inspection.Model.Runs[position]
	now := builder.clock()()
	run.State = RunInterrupted
	run.Finished = formatTime(now)
	run.Error = "previous compose process ended without terminal state"
	if len(run.Attempts) > 0 && run.Attempts[len(run.Attempts)-1].State == RunRunning {
		attempt := &run.Attempts[len(run.Attempts)-1]
		attempt.State = RunInterrupted
		attempt.Finished = run.Finished
		attempt.Error = run.Error
	}
	record := inspection.Model
	runDirectory := filepath.Join(inspection.Path, "runs", runDirectoryName(pin))
	if err := persistRunAndPin(inspection.Path, runDirectory, &record, pin, run, now); err != nil {
		return fmt.Errorf("recover abandoned compose run %s: %w", run.ID, err)
	}
	updated, err := Inspect(builder.Root, inspection.Model.Name)
	if err != nil {
		return err
	}
	*inspection = updated
	return nil
}

func validateStagedComposeRun(inspection Inspection, index int, prepared PreparedStage) error {
	if index >= len(inspection.RunBOMs) || inspection.Model.Runs[index].Stage != prepared.Stage.Name {
		return fmt.Errorf("run %d does not match stage %s", index+1, prepared.Stage.Name)
	}
	bom := inspection.RunBOMs[index]
	corpusHash, err := hashJSON(prepared.BOM)
	if err != nil {
		return err
	}
	parameters, err := training.ResolveParameters(prepared.Stage.Parameters)
	if err != nil {
		return err
	}
	if bom.Stage != prepared.Stage.Name || bom.StageType != prepared.Stage.Type || bom.Objective != prepared.Stage.Objective || bom.CorpusBOMSHA256 != corpusHash || !reflect.DeepEqual(bom.Parameters, parameters) {
		return fmt.Errorf("run %d immutable facts do not match stage %s", index+1, prepared.Stage.Name)
	}
	return nil
}

func (builder Builder) initializeFromOrigin(name string, compose Compose, base Inspection) (Inspection, error) {
	plan, err := composePlan(name, compose)
	if err != nil {
		return Inspection{}, err
	}
	planHash, err := hashJSON(plan)
	if err != nil {
		return Inspection{}, err
	}
	now := builder.clock()()
	record := ModelRecord{
		Kind: "waldo-model", Schema: ModelSchema, ID: planHash, Name: name,
		PlanSHA256: planHash, ArchitectureSHA256: plan.ArchitectureSHA256,
		Architecture: plan.Architecture, Forecast: plan.Forecast,
		Created: formatTime(now), Updated: formatTime(now),
		OriginBOMSHA256: base.Model.OriginBOMSHA256,
		OriginArtifacts: append([]OriginArtifact(nil), base.Model.OriginArtifacts...),
	}
	destination := filepath.Join(builder.Root, name)
	if err := initializeModel(builder.Root, destination, plan, record); err != nil {
		return Inspection{}, err
	}
	for _, artifact := range base.Origin.Artifacts {
		source := filepath.Join(base.Path, filepath.FromSlash(artifact.Path))
		target := filepath.Join(destination, filepath.FromSlash(artifact.Path))
		if err := copyOriginFile(source, target); err != nil {
			return Inspection{}, err
		}
	}
	if err := writeJSONAtomic(filepath.Join(destination, "ORIGIN-BOM.json"), base.Origin); err != nil {
		return Inspection{}, err
	}
	return Inspect(builder.Root, name)
}

func copyOriginFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	return errors.Join(copyErr, closeErr)
}

func initializeModel(root, destination string, plan Plan, record ModelRecord) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp(root, ".waldo-model-*")
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(temporary)
		}
	}()
	if err := writeJSONAtomic(filepath.Join(temporary, "PLAN.json"), plan); err != nil {
		return err
	}
	if err := writeJSONAtomic(filepath.Join(temporary, "MODEL.json"), record); err != nil {
		return err
	}
	if err := writeJSONAtomic(filepath.Join(temporary, "MODEL-BOM.json"), modelBOM(record)); err != nil {
		return err
	}
	if err := os.Rename(temporary, destination); err != nil {
		return fmt.Errorf("create model %s: %w", record.Name, err)
	}
	committed = true
	return nil
}

func persistRunAndPin(modelPath, runDirectory string, record *ModelRecord, original RunPin, run RunRecord, now time.Time) error {
	if err := writeJSONAtomic(filepath.Join(runDirectory, "RUN.json"), run); err != nil {
		return err
	}
	observationHash := ""
	var artifacts []training.Artifact
	if run.Observation != nil {
		var err error
		observationHash, err = hashJSON(run.Observation)
		if err != nil {
			return err
		}
		artifacts = append([]training.Artifact(nil), run.Observation.Artifacts...)
	}
	for i := range record.Runs {
		if record.Runs[i].ID == original.ID {
			record.Runs[i].State = run.State
			record.Runs[i].ObservationSHA256 = observationHash
			record.Runs[i].Artifacts = artifacts
			break
		}
	}
	return persistModel(modelPath, record, now)
}

func persistModel(modelPath string, record *ModelRecord, now time.Time) error {
	record.Updated = formatTime(now)
	sortPins(record.Runs)
	if err := writeJSONAtomic(filepath.Join(modelPath, "MODEL.json"), record); err != nil {
		return err
	}
	return writeJSONAtomic(filepath.Join(modelPath, "MODEL-BOM.json"), modelBOM(*record))
}

func builtinResolver() training.Resolver {
	return training.NewEnvironmentResolver(training.BackendAuto)
}

func validateSelection(selection training.Selection, objectives []string) error {
	if selection.Backend == nil {
		return fmt.Errorf("resolved training backend is nil")
	}
	descriptor := selection.Backend.Descriptor()
	if descriptor.Identity.Name == "" || descriptor.Identity.Revision == "" || descriptor.Framework == "" {
		return fmt.Errorf("resolved training backend has an incomplete descriptor")
	}
	if selection.Execution.Backend != descriptor.Identity || selection.Execution.Framework != descriptor.Framework {
		return fmt.Errorf("resolved execution does not match backend %s@%s", descriptor.Identity.Name, descriptor.Identity.Revision)
	}
	if selection.Execution.Host.OS == "" || selection.Execution.Host.Architecture == "" || selection.Execution.Nodes <= 0 || selection.Execution.WorldSize <= 0 {
		return fmt.Errorf("resolved execution has incomplete host or topology facts")
	}
	supported := make(map[string]bool, len(descriptor.Capabilities.Objectives))
	for _, objective := range descriptor.Capabilities.Objectives {
		supported[objective] = true
	}
	for _, objective := range objectives {
		if !supported[objective] {
			return fmt.Errorf("training backend %s@%s does not support objective %s", descriptor.Identity.Name, descriptor.Identity.Revision, objective)
		}
	}
	return nil
}

func (builder Builder) clock() func() time.Time {
	if builder.Now != nil {
		return builder.Now
	}
	return time.Now
}

func (builder Builder) identifier() func() (string, error) {
	if builder.NewID != nil {
		return builder.NewID
	}
	return randomID
}

func (builder Builder) report(progress Progress) {
	if builder.Progress != nil {
		builder.Progress(progress)
	}
}

func randomID() (string, error) {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func planObjectives(plan Plan) []string {
	seen := make(map[string]bool)
	for _, stage := range plan.Stages {
		seen[stage.Objective] = true
	}
	objectives := make([]string, 0, len(seen))
	for objective := range seen {
		objectives = append(objectives, objective)
	}
	sort.Strings(objectives)
	return objectives
}
