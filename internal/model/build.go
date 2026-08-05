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
	"reflect"
	"sort"
	"time"

	"github.com/openwaldo/waldo-new/internal/training"
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
	records, err := training.NewCanonicalRecordSource(prepared.Inputs, resolvedParameters)
	if err != nil {
		return Inspection{}, fmt.Errorf("stage %s record stream: %w", stage.Name, err)
	}
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

	runID, err := builder.identifier()()
	if err != nil {
		return Inspection{}, err
	}
	ordinal := len(inspection.Model.Runs) + 1
	pin := RunPin{ID: runID, Stage: stage.Name, Ordinal: ordinal, State: RunPlanned, Backend: selection.Execution.Backend, Simulated: selection.Execution.Backend.Name == training.BackendFake}
	runDirectory := filepath.Join(inspection.Path, "runs", runDirectoryName(pin))
	bomHash, err := hashJSON(prepared.BOM)
	if err != nil {
		return Inspection{}, err
	}
	runBOM := RunBOM{
		Kind: "openwaldo-bom", Schema: RunBOMSchema, Subject: "training-run",
		ID: runID, ModelID: inspection.Model.ID, Stage: stage.Name, StageType: stage.Type,
		Ordinal: ordinal, Objective: stage.Objective, Execution: selection.Execution,
		ArchitectureSHA256: inspection.Model.ArchitectureSHA256,
		CorpusBOMSHA256:    bomHash, CorpusBOM: prepared.BOM, Parameters: resolvedParameters,
		Initialization: initialization,
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

	run.State = RunRunning
	run.Started = formatTime(now())
	if err := persistRunAndPin(inspection.Path, runDirectory, &record, pin, run, now()); err != nil {
		return Inspection{}, err
	}
	builder.report(Progress{Phase: "run", Stage: pin.Stage, RunID: runID, State: RunRunning, Message: "training backend started"})
	artifactPrefix := "artifacts"
	observation, backendErr := selection.Backend.Run(ctx, training.Request{
		RunID: runID, Stage: stage.Name, Objective: stage.Objective,
		ArchitectureSHA256: inspection.Model.ArchitectureSHA256,
		Architecture:       architectureJSON, BOM: prepared.BOM, Inputs: prepared.Inputs,
		Parameters: resolvedParameters, Records: records, Initialization: initialization,
		ArtifactDirectory: filepath.Join(runDirectory, artifactPrefix),
		ArtifactPrefix:    artifactPrefix,
		Report: func(event training.Event) {
			builder.report(Progress{Phase: "training", Stage: stage.Name, RunID: runID, State: RunRunning, Message: event.Message, Training: &event})
		},
	})
	if backendErr == nil {
		planned := PlannedStage{Name: stage.Name, Parameters: stage.Parameters, PlannedTokens: resolvedParameters.PlannedTokenCapacity}
		if err := validateBackendObservation(runDirectory, planned, observation); err != nil {
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
		if err := persistRunAndPin(inspection.Path, runDirectory, &record, pin, run, now()); err != nil {
			return Inspection{}, errors.Join(backendErr, err)
		}
		builder.report(Progress{Phase: "run", Stage: pin.Stage, RunID: runID, State: run.State, Message: run.Error})
		return Inspection{}, fmt.Errorf("stage %s: %w", pin.Stage, backendErr)
	}
	run.State = RunComplete
	run.Observation = &observation
	if err := persistRunAndPin(inspection.Path, runDirectory, &record, pin, run, now()); err != nil {
		return Inspection{}, err
	}
	builder.report(Progress{Phase: "run", Stage: pin.Stage, RunID: runID, State: RunComplete, Message: "persisted training observations and artifact hashes"})
	return Inspect(builder.Root, name)
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
			return &training.Initialization{SourceRunID: run.ID, Artifact: artifact, Path: path}, nil
		}
	}
	return nil, nil
}

// Compose creates a model and executes every prepared stage. Work is built in
// a sibling temporary directory so --replace never destroys a valid model on
// parse, preflight, resolver, or backend failure.
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
	if _, err := os.Stat(destination); err == nil && !replace {
		return Inspection{}, fmt.Errorf("model %q already exists; use --replace to recreate it", name)
	} else if err != nil && !os.IsNotExist(err) {
		return Inspection{}, err
	}
	temporaryRoot, err := os.MkdirTemp(builder.Root, ".waldo-compose-*")
	if err != nil {
		return Inspection{}, err
	}
	defer os.RemoveAll(temporaryRoot)
	temporaryBuilder := builder
	temporaryBuilder.Root = temporaryRoot
	if _, err := temporaryBuilder.Initialize(name, compose.Architecture); err != nil {
		return Inspection{}, err
	}
	for _, stage := range stages {
		if _, err := temporaryBuilder.Train(ctx, name, stage); err != nil {
			return Inspection{}, err
		}
	}
	newPath := filepath.Join(temporaryRoot, name)
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
	return Inspect(builder.Root, name)
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
