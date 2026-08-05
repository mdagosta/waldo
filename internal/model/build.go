package model

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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
	Root     string
	Now      func() time.Time
	NewID    func() (string, error)
	Resolver training.Resolver
	Progress func(Progress)
}

func (builder Builder) Build(ctx context.Context, compose Compose) (Inspection, error) {
	if err := compose.Validate(); err != nil {
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
	modelPath := filepath.Join(builder.Root, compose.Name)
	if _, err := os.Stat(modelPath); err == nil {
		return Inspection{}, fmt.Errorf("model %q already exists; use a future explicit continuation workflow rather than rebuilding it", compose.Name)
	} else if !os.IsNotExist(err) {
		return Inspection{}, err
	}
	plan, stages, err := preflight(compose, builder.Progress)
	if err != nil {
		return Inspection{}, err
	}
	architectureJSON, err := json.Marshal(plan.Architecture)
	if err != nil {
		return Inspection{}, err
	}
	resolver := builder.Resolver
	if resolver == nil {
		resolver = builtinResolver()
	}
	selection, err := resolver.Resolve(ctx, training.ResolveRequest{
		ArchitectureSHA256: plan.ArchitectureSHA256,
		Architecture:       architectureJSON,
		Objectives:         planObjectives(plan),
	})
	if err != nil {
		return Inspection{}, fmt.Errorf("resolve training backend: %w", err)
	}
	if err := validateSelection(selection, plan); err != nil {
		return Inspection{}, err
	}
	plan.Execution = selection.Execution
	backend := selection.Backend
	planHash, err := hashJSON(plan)
	if err != nil {
		return Inspection{}, err
	}
	record := ModelRecord{
		Kind: "waldo-model", Schema: ModelSchema, ID: planHash, Name: compose.Name,
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
			ID: runID, ModelID: record.ID, Stage: stage.Plan.Name, StageType: stage.Plan.Type, Ordinal: ordinal + 1,
			Objective: stage.Plan.Objective, Execution: plan.Execution,
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
		artifactPrefix := "artifacts"
		observation, backendErr := backend.Run(ctx, training.Request{
			RunID: runID, Stage: stage.Plan.Name, Objective: stage.Plan.Objective,
			ArchitectureSHA256: plan.ArchitectureSHA256, Architecture: architectureJSON, BOM: stage.BOM,
			Inputs: stage.Inputs, Parameters: stage.Plan.Parameters,
			ArtifactDirectory: filepath.Join(runDirectory, artifactPrefix), ArtifactPrefix: artifactPrefix,
			Report: func(event training.Event) {
				builder.report(Progress{Phase: "training", Stage: stage.Plan.Name, RunID: runID, State: RunRunning, Message: event.Message})
			},
		})
		if backendErr == nil {
			if err := validateBackendObservation(runDirectory, stage.Plan, observation); err != nil {
				backendErr = fmt.Errorf("invalid backend observation: %w", err)
			}
		}
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
	return Inspect(builder.Root, compose.Name)
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
	return training.ResolverFunc(func(_ context.Context, _ training.ResolveRequest) (training.Selection, error) {
		backend := training.Fake{}
		descriptor := backend.Descriptor()
		return training.Selection{
			Backend: backend,
			Execution: training.Execution{
				Backend: descriptor.Identity, Framework: descriptor.Framework, Runtime: "builtin",
				Host: training.Host{OS: runtime.GOOS, Architecture: runtime.GOARCH}, Nodes: 1, WorldSize: 1,
			},
		}, nil
	})
}

func validateSelection(selection training.Selection, plan Plan) error {
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
	for _, stage := range plan.Stages {
		if !supported[stage.Objective] {
			return fmt.Errorf("training backend %s@%s does not support objective %s", descriptor.Identity.Name, descriptor.Identity.Revision, stage.Objective)
		}
	}
	return nil
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
