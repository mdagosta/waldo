package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/openwaldo/waldo-new/internal/corpus"
	"github.com/openwaldo/waldo-new/internal/training"
)

const (
	PlanSchema     = 1
	ModelSchema    = 1
	RunSchema      = 1
	ModelBOMSchema = 1
	RunBOMSchema   = 1
)

type Plan struct {
	Kind               string               `json:"kind"`
	Schema             int                  `json:"schema"`
	Name               string               `json:"name"`
	ArchitectureSHA256 string               `json:"architecture_sha256"`
	Architecture       Architecture         `json:"architecture"`
	Forecast           ArchitectureForecast `json:"forecast"`
	Stages             []PlannedStage       `json:"stages,omitempty"`
}

type PlannedStage struct {
	Name            string              `json:"name"`
	Type            string              `json:"type"`
	Objective       string              `json:"objective"`
	CorpusBOMSHA256 string              `json:"corpus_bom_sha256"`
	Files           int                 `json:"files"`
	Docs            int64               `json:"docs"`
	Tokens          int64               `json:"tokens"`
	Bytes           int64               `json:"bytes"`
	Parameters      training.Parameters `json:"parameters"`
	PlannedTokens   int64               `json:"planned_token_capacity"`
}

type ModelRecord struct {
	Kind               string               `json:"kind"`
	Schema             int                  `json:"schema"`
	ID                 string               `json:"id"`
	Name               string               `json:"name"`
	PlanSHA256         string               `json:"plan_sha256"`
	ArchitectureSHA256 string               `json:"architecture_sha256"`
	Architecture       Architecture         `json:"architecture"`
	Forecast           ArchitectureForecast `json:"forecast"`
	Created            string               `json:"created"`
	Updated            string               `json:"updated"`
	Runs               []RunPin             `json:"runs"`
}

type RunState string

const (
	RunPlanned     RunState = "planned"
	RunRunning     RunState = "running"
	RunComplete    RunState = "complete"
	RunFailed      RunState = "failed"
	RunInterrupted RunState = "interrupted"
)

type RunPin struct {
	ID                string              `json:"id"`
	Stage             string              `json:"stage"`
	Ordinal           int                 `json:"ordinal"`
	BOMSHA256         string              `json:"bom_sha256"`
	State             RunState            `json:"state"`
	ObservationSHA256 string              `json:"observation_sha256,omitempty"`
	Artifacts         []training.Artifact `json:"artifacts,omitempty"`
}

type RunBOM struct {
	Kind               string                      `json:"kind"`
	Schema             int                         `json:"schema"`
	Subject            string                      `json:"subject"`
	ID                 string                      `json:"id"`
	ModelID            string                      `json:"model_id"`
	Stage              string                      `json:"stage"`
	StageType          string                      `json:"stage_type"`
	Ordinal            int                         `json:"ordinal"`
	Objective          string                      `json:"objective"`
	Execution          training.Execution          `json:"execution"`
	ArchitectureSHA256 string                      `json:"architecture_sha256"`
	CorpusBOMSHA256    string                      `json:"corpus_bom_sha256"`
	CorpusBOM          corpus.BOM                  `json:"corpus_bom"`
	Parameters         training.ResolvedParameters `json:"parameters"`
	Initialization     *training.Initialization    `json:"initialization,omitempty"`
}

type RunRecord struct {
	Kind        string                `json:"kind"`
	Schema      int                   `json:"schema"`
	ID          string                `json:"id"`
	State       RunState              `json:"state"`
	BOMSHA256   string                `json:"bom_sha256"`
	Planned     string                `json:"planned"`
	Started     string                `json:"started,omitempty"`
	Finished    string                `json:"finished,omitempty"`
	Observation *training.Observation `json:"observation,omitempty"`
	Error       string                `json:"error,omitempty"`
}

type ModelBOM struct {
	Kind               string   `json:"kind"`
	Schema             int      `json:"schema"`
	Subject            string   `json:"subject"`
	ModelID            string   `json:"model_id"`
	Name               string   `json:"name"`
	PlanSHA256         string   `json:"plan_sha256"`
	ArchitectureSHA256 string   `json:"architecture_sha256"`
	Runs               []RunPin `json:"runs"`
	Generated          string   `json:"generated"`
}

type Inspection struct {
	Path    string      `json:"path"`
	Plan    Plan        `json:"plan"`
	Model   ModelRecord `json:"model"`
	BOM     ModelBOM    `json:"bom"`
	Runs    []RunRecord `json:"runs"`
	RunBOMs []RunBOM    `json:"run_boms"`
}

func Inspect(root, nameOrPath string) (Inspection, error) {
	directory, err := modelDirectory(root, nameOrPath)
	if err != nil {
		return Inspection{}, err
	}
	var record ModelRecord
	if err := readJSON(filepath.Join(directory, "MODEL.json"), &record); err != nil {
		return Inspection{}, err
	}
	if record.Kind != "waldo-model" || record.Schema != ModelSchema || !validName.MatchString(record.Name) || record.ID == "" {
		return Inspection{}, fmt.Errorf("%s has an invalid model record", directory)
	}
	architectureHash, err := hashJSON(record.Architecture)
	if err != nil {
		return Inspection{}, err
	}
	forecast, err := record.Architecture.Forecast()
	if err != nil || architectureHash != record.ArchitectureSHA256 || !reflect.DeepEqual(forecast, record.Forecast) {
		return Inspection{}, fmt.Errorf("%s has inconsistent architecture identity or forecast", directory)
	}
	var plan Plan
	if err := readJSON(filepath.Join(directory, "PLAN.json"), &plan); err != nil {
		return Inspection{}, err
	}
	planHash, err := hashJSON(plan)
	if err != nil {
		return Inspection{}, err
	}
	if plan.Kind != "waldo-model-plan" || plan.Schema != PlanSchema || planHash != record.PlanSHA256 || record.ID != planHash || plan.Name != record.Name || plan.ArchitectureSHA256 != record.ArchitectureSHA256 || !reflect.DeepEqual(plan.Architecture, record.Architecture) || !reflect.DeepEqual(plan.Forecast, record.Forecast) {
		return Inspection{}, fmt.Errorf("%s has an invalid immutable model plan", directory)
	}
	var bom ModelBOM
	if err := readJSON(filepath.Join(directory, "MODEL-BOM.json"), &bom); err != nil {
		return Inspection{}, err
	}
	if bom.Kind != "openwaldo-bom" || bom.Schema != ModelBOMSchema || bom.Subject != "model" || bom.ModelID != record.ID || bom.Name != record.Name || bom.PlanSHA256 != record.PlanSHA256 || bom.ArchitectureSHA256 != record.ArchitectureSHA256 || bom.Generated != record.Updated || !reflect.DeepEqual(bom.Runs, record.Runs) {
		return Inspection{}, fmt.Errorf("%s has an invalid model OpenWALDO BOM", directory)
	}
	inspection := Inspection{Path: directory, Plan: plan, Model: record, BOM: bom}
	for _, pin := range record.Runs {
		position := len(inspection.Runs)
		if pin.Ordinal != position+1 {
			return Inspection{}, fmt.Errorf("model run %s has ordinal %d at position %d", pin.ID, pin.Ordinal, position+1)
		}
		var run RunRecord
		runDirectory := filepath.Join(directory, "runs", runDirectoryName(pin))
		if err := readJSON(filepath.Join(runDirectory, "RUN.json"), &run); err != nil {
			return Inspection{}, err
		}
		var runBOM RunBOM
		if err := readJSON(filepath.Join(runDirectory, "RUN-BOM.json"), &runBOM); err != nil {
			return Inspection{}, err
		}
		runBOMHash, err := hashJSON(runBOM)
		if err != nil {
			return Inspection{}, err
		}
		if run.Kind != "waldo-training-run" || run.Schema != RunSchema || run.ID != pin.ID || run.State != pin.State || run.BOMSHA256 != pin.BOMSHA256 || runBOMHash != pin.BOMSHA256 || runBOM.ID != pin.ID || runBOM.ModelID != record.ID || runBOM.Stage != pin.Stage || runBOM.Ordinal != pin.Ordinal {
			return Inspection{}, fmt.Errorf("run %s does not match its model pin", pin.ID)
		}
		if runBOM.ArchitectureSHA256 != plan.ArchitectureSHA256 || runBOM.ModelID != record.ID || runBOM.Stage == "" || runBOM.StageType == "" || runBOM.Objective == "" {
			return Inspection{}, fmt.Errorf("run %s does not match its immutable model architecture", pin.ID)
		}
		corpusHash, err := hashJSON(runBOM.CorpusBOM)
		if err != nil || corpusHash != runBOM.CorpusBOMSHA256 || runBOM.ArchitectureSHA256 != record.ArchitectureSHA256 {
			return Inspection{}, fmt.Errorf("run %s has inconsistent corpus or architecture identity", pin.ID)
		}
		if err := runBOM.CorpusBOM.Validate(); err != nil {
			return Inspection{}, fmt.Errorf("run %s corpus OpenWALDO BOM: %w", pin.ID, err)
		}
		if err := validateRunState(run, pin); err != nil {
			return Inspection{}, err
		}
		inspection.Runs = append(inspection.Runs, run)
		inspection.RunBOMs = append(inspection.RunBOMs, runBOM)
	}
	return inspection, nil
}

func validateRunState(run RunRecord, pin RunPin) error {
	switch run.State {
	case RunPlanned:
		if run.Started != "" || run.Finished != "" || run.Observation != nil || run.Error != "" {
			return fmt.Errorf("planned run %s contains observations or terminal state", run.ID)
		}
	case RunRunning:
		if run.Started == "" || run.Finished != "" || run.Observation != nil || run.Error != "" {
			return fmt.Errorf("running run %s has inconsistent state", run.ID)
		}
	case RunComplete:
		if run.Started == "" || run.Finished == "" || run.Observation == nil || run.Error != "" || len(run.Observation.Artifacts) == 0 {
			return fmt.Errorf("complete run %s has incomplete observations", run.ID)
		}
		observationHash, err := hashJSON(run.Observation)
		if err != nil {
			return err
		}
		if observationHash != pin.ObservationSHA256 || !reflect.DeepEqual(run.Observation.Artifacts, pin.Artifacts) {
			return fmt.Errorf("complete run %s observations do not match its model pin", run.ID)
		}
	case RunFailed, RunInterrupted:
		if run.Started == "" || run.Finished == "" || run.Observation != nil || run.Error == "" {
			return fmt.Errorf("terminal run %s has inconsistent failure state", run.ID)
		}
	default:
		return fmt.Errorf("run %s has unsupported state %q", run.ID, run.State)
	}
	return nil
}

func modelDirectory(root, value string) (string, error) {
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if value == "~" {
			value = home
		} else {
			value = filepath.Join(home, strings.TrimPrefix(value, "~/"))
		}
	}
	if filepath.IsAbs(value) || strings.HasPrefix(value, ".") || strings.ContainsRune(value, filepath.Separator) {
		return filepath.Abs(value)
	}
	if !validName.MatchString(value) {
		return "", fmt.Errorf("invalid model name or path %q", value)
	}
	return filepath.Join(root, value), nil
}

func runDirectoryName(pin RunPin) string {
	return fmt.Sprintf("%04d-%s-%s", pin.Ordinal, pin.Stage, pin.ID)
}

func modelBOM(record ModelRecord) ModelBOM {
	return ModelBOM{
		Kind: "openwaldo-bom", Schema: ModelBOMSchema, Subject: "model",
		ModelID: record.ID, Name: record.Name, PlanSHA256: record.PlanSHA256,
		ArchitectureSHA256: record.ArchitectureSHA256, Runs: append([]RunPin(nil), record.Runs...),
		Generated: record.Updated,
	}
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".waldo-state-*")
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

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func hashJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func sortPins(pins []RunPin) {
	sort.Slice(pins, func(i, j int) bool { return pins[i].Ordinal < pins[j].Ordinal })
}
