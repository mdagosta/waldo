package model

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/openwaldo/waldo-new/internal/training"
)

type Progress struct {
	Phase   string   `json:"phase"`
	Stage   string   `json:"stage,omitempty"`
	RunID   string   `json:"run_id,omitempty"`
	State   RunState `json:"state,omitempty"`
	Message string   `json:"message"`
}

type Builder struct {
	Root           string
	Now            func() time.Time
	NewID          func() (string, error)
	ResolveBackend func(training.Identity) (training.Backend, error)
	Progress       func(Progress)
}

func (builder Builder) Build(ctx context.Context, recipe Recipe) (Inspection, error) {
	if err := recipe.Validate(); err != nil {
		return Inspection{}, err
	}
	if builder.Root == "" {
		return Inspection{}, fmt.Errorf("model root is required")
	}
	now := builder.Now
	if now == nil {
		now = time.Now
	}
	newID := builder.NewID
	if newID == nil {
		newID = randomID
	}
	resolve := builder.ResolveBackend
	if resolve == nil {
		resolve = resolveBuiltinBackend
	}
	backend, err := resolve(recipe.Backend)
	if err != nil {
		return Inspection{}, err
	}
	modelPath := filepath.Join(builder.Root, recipe.Name)
	if _, err := os.Stat(modelPath); err == nil {
		return Inspection{}, fmt.Errorf("model %q already exists; use a future explicit continuation workflow rather than rebuilding it", recipe.Name)
	} else if !os.IsNotExist(err) {
		return Inspection{}, err
	}
	plan, stages, err := preflight(recipe, builder.Progress)
	if err != nil {
		return Inspection{}, err
	}
	planHash, err := hashJSON(plan)
	if err != nil {
		return Inspection{}, err
	}
	record := ModelRecord{
		Kind: "waldo-model", Schema: ModelSchema, ID: planHash, Name: recipe.Name,
		PlanSHA256: planHash, ArchitectureSHA256: plan.ArchitectureSHA256,
		Architecture: plan.Architecture, Forecast: plan.Forecast,
		Created: formatTime(now()), Updated: formatTime(now()),
	}
	if err := initializeModel(builder.Root, modelPath, plan, record); err != nil {
		return Inspection{}, err
	}
	builder.report(Progress{Phase: "model", Message: "persisted immutable build plan and initial model OpenWALDO BOM"})

	for ordinal, stage := range stages {
		runID, err := newID()
		if err != nil {
			return Inspection{}, err
		}
		pin := RunPin{ID: runID, Stage: stage.Plan.Name, Ordinal: ordinal + 1, State: RunPlanned}
		runDirectory := filepath.Join(modelPath, "runs", runDirectoryName(pin))
		runBOM := RunBOM{
			Kind: "openwaldo-bom", Schema: RunBOMSchema, Subject: "training-run",
			ID: runID, ModelID: record.ID, Stage: stage.Plan.Name, Ordinal: ordinal + 1,
			Objective: stage.Plan.Objective, Backend: plan.Backend,
			ArchitectureSHA256: plan.ArchitectureSHA256, CorpusBOMSHA256: stage.Plan.CorpusBOMSHA256,
			CorpusBOM: stage.BOM, Files: stage.Files, Parameters: stage.Plan.Parameters,
		}
		runBOMHash, err := hashJSON(runBOM)
		if err != nil {
			return Inspection{}, err
		}
		pin.BOMSHA256 = runBOMHash
		run := RunRecord{
			Kind: "waldo-training-run", Schema: RunSchema, ID: runID,
			State: RunPlanned, BOMSHA256: runBOMHash, Planned: formatTime(now()),
		}
		if err := writeJSONAtomic(filepath.Join(runDirectory, "RUN-BOM.json"), runBOM); err != nil {
			return Inspection{}, err
		}
		if err := writeJSONAtomic(filepath.Join(runDirectory, "RUN.json"), run); err != nil {
			return Inspection{}, err
		}
		record.Runs = append(record.Runs, pin)
		if err := persistModel(modelPath, &record, now()); err != nil {
			return Inspection{}, err
		}
		builder.report(Progress{Phase: "run", Stage: pin.Stage, RunID: runID, State: RunPlanned, Message: "persisted run OpenWALDO BOM"})

		run.State = RunRunning
		run.Started = formatTime(now())
		if err := persistRunAndPin(modelPath, runDirectory, &record, pin, run, now()); err != nil {
			return Inspection{}, err
		}
		builder.report(Progress{Phase: "run", Stage: pin.Stage, RunID: runID, State: RunRunning, Message: "training backend started"})
		artifactPath := filepath.ToSlash(filepath.Join("artifacts", "fake-model.json"))
		observation, backendErr := backend.Run(ctx, training.Request{
			RunID: runID, ArchitectureSHA256: plan.ArchitectureSHA256, BOM: stage.BOM,
			Inputs: stage.Inputs, Parameters: stage.Plan.Parameters,
			OutputPath: filepath.Join(runDirectory, filepath.FromSlash(artifactPath)), ArtifactPath: artifactPath,
		})
		run.Finished = formatTime(now())
		if backendErr != nil {
			run.State = RunFailed
			if errors.Is(backendErr, context.Canceled) || errors.Is(backendErr, context.DeadlineExceeded) {
				run.State = RunInterrupted
			}
			run.Error = backendErr.Error()
			if err := persistRunAndPin(modelPath, runDirectory, &record, pin, run, now()); err != nil {
				return Inspection{}, errors.Join(backendErr, err)
			}
			builder.report(Progress{Phase: "run", Stage: pin.Stage, RunID: runID, State: run.State, Message: run.Error})
			return Inspection{}, fmt.Errorf("stage %s: %w", pin.Stage, backendErr)
		}
		run.State = RunComplete
		run.Observation = &observation
		if err := persistRunAndPin(modelPath, runDirectory, &record, pin, run, now()); err != nil {
			return Inspection{}, err
		}
		builder.report(Progress{Phase: "run", Stage: pin.Stage, RunID: runID, State: RunComplete, Message: "persisted simulated observations and artifact hashes"})
	}
	return Inspect(builder.Root, recipe.Name)
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

func resolveBuiltinBackend(identity training.Identity) (training.Backend, error) {
	if identity.Name != "fake" || identity.Revision != training.FakeRevision {
		return nil, fmt.Errorf("training backend %s@%s is unavailable; Phase 4 supports fake@%s", identity.Name, identity.Revision, training.FakeRevision)
	}
	return training.Fake{}, nil
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
