package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/openwaldo/waldo-new/internal/index"
	"gopkg.in/yaml.v3"
)

const (
	RecipeKind      = "waldo-ingest-recipe"
	RecipeSchema    = 1
	recipeMaximum   = 1 << 20
	recipeJournal   = "RECIPE.json"
	recipeWorkspace = "recipes"
)

var recipeStepName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// IngestRecipe describes source preparation followed by the normal WALDO ingest
// pipeline. Commands are source-specific external producers; their only output
// is a WALDO-owned temporary directory.
type IngestRecipe struct {
	Kind        string       `json:"kind" yaml:"kind"`
	Schema      int          `json:"schema" yaml:"schema"`
	Title       string       `json:"title" yaml:"title"`
	Description string       `json:"description,omitempty" yaml:"description,omitempty"`
	License     string       `json:"license" yaml:"license"`
	Source      RecipeSource `json:"source" yaml:"source"`
	TextColumn  string       `json:"text_column,omitempty" yaml:"text_column,omitempty"`
	Steps       []RecipeStep `json:"steps" yaml:"steps"`
}

type RecipeSource struct {
	Name     string `json:"name,omitempty" yaml:"name,omitempty"`
	URL      string `json:"url" yaml:"url"`
	Category string `json:"category" yaml:"category"`
}

type RecipeStep struct {
	Name string   `json:"name" yaml:"name"`
	Exec string   `json:"exec" yaml:"exec"`
	Args []string `json:"args,omitempty" yaml:"args,omitempty"`
}

type LoadedRecipe struct {
	Recipe      IngestRecipe               `json:"recipe"`
	Path        string                     `json:"path"`
	SHA256      string                     `json:"sha256"`
	Evidence    index.IngestRecipeEvidence `json:"evidence"`
	Executables []ResolvedExecutable       `json:"executables"`
}

type ResolvedExecutable struct {
	Name   string   `json:"name"`
	Exec   string   `json:"exec"`
	Path   string   `json:"path"`
	SHA256 string   `json:"sha256"`
	Args   []string `json:"args,omitempty"`
}

// LoadRecipe recognizes only a small, strictly identified YAML or JSON
// document. Ordinary source files remain inputs to content probing.
func LoadRecipe(path string) (LoadedRecipe, bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return LoadedRecipe{}, false, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return LoadedRecipe{}, false, nil
		}
		return LoadedRecipe{}, false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > recipeMaximum {
		return LoadedRecipe{}, false, nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return LoadedRecipe{}, false, err
	}
	var header struct {
		Kind string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		if bytes.Contains(data, []byte(RecipeKind)) {
			return LoadedRecipe{}, true, fmt.Errorf("%s: malformed ingest recipe: %w", abs, err)
		}
		return LoadedRecipe{}, false, nil
	}
	if header.Kind != RecipeKind {
		if header.Kind == "waldo-ingest-compose" {
			return LoadedRecipe{}, true, fmt.Errorf("%s: ingest identity %q is retired; use %q", abs, header.Kind, RecipeKind)
		}
		return LoadedRecipe{}, false, nil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var recipe IngestRecipe
	if err := decoder.Decode(&recipe); err != nil {
		return LoadedRecipe{}, true, fmt.Errorf("%s: %w", abs, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple YAML documents are not allowed")
		}
		return LoadedRecipe{}, true, fmt.Errorf("%s: %w", abs, err)
	}
	if err := recipe.Validate(); err != nil {
		return LoadedRecipe{}, true, fmt.Errorf("%s: %w", abs, err)
	}
	digest := sha256.Sum256(data)
	loaded := LoadedRecipe{Recipe: recipe, Path: abs, SHA256: hex.EncodeToString(digest[:])}
	loaded.Evidence = index.IngestRecipeEvidence{Path: filepath.Base(abs), SHA256: loaded.SHA256}
	for _, step := range recipe.Steps {
		resolved, err := resolveRecipeExecutable(abs, step)
		if err != nil {
			return LoadedRecipe{}, true, err
		}
		loaded.Executables = append(loaded.Executables, resolved)
		loaded.Evidence.Steps = append(loaded.Evidence.Steps, index.RecipeStepEvidence{
			Name: step.Name, Executable: filepath.ToSlash(relativeEvidencePath(filepath.Dir(abs), resolved.Path)), SHA256: resolved.SHA256,
		})
	}
	populateGitEvidence(&loaded)
	return loaded, true, nil
}

func (recipe IngestRecipe) Validate() error {
	if recipe.Kind != RecipeKind || recipe.Schema != RecipeSchema {
		return fmt.Errorf("unsupported ingest recipe identity %q schema %d", recipe.Kind, recipe.Schema)
	}
	if strings.TrimSpace(recipe.Title) == "" || strings.TrimSpace(recipe.License) == "" {
		return fmt.Errorf("title and license are required")
	}
	if strings.TrimSpace(recipe.Source.URL) == "" || strings.TrimSpace(recipe.Source.Category) == "" {
		return fmt.Errorf("source url and category are required")
	}
	if _, ok := index.CanonicalSourceCategory(recipe.Source.Category); !ok {
		return fmt.Errorf("unsupported source category %q", recipe.Source.Category)
	}
	category, _ := index.CanonicalSourceCategory(recipe.Source.Category)
	switch category {
	case index.SourcePublicDataset, index.SourcePrivateThirdParty, index.SourceOther:
	default:
		return fmt.Errorf("source category %q requires acquisition evidence fields that ingest recipe does not collect yet", category)
	}
	if len(recipe.Steps) == 0 {
		return fmt.Errorf("at least one fetcher step is required")
	}
	seen := map[string]bool{}
	for position, step := range recipe.Steps {
		if !recipeStepName.MatchString(step.Name) || seen[step.Name] {
			return fmt.Errorf("step %d has invalid or duplicate name %q", position+1, step.Name)
		}
		seen[step.Name] = true
		if strings.TrimSpace(step.Exec) == "" {
			return fmt.Errorf("step %q exec is required", step.Name)
		}
		if strings.ContainsRune(step.Exec, '\x00') {
			return fmt.Errorf("step %q exec contains NUL", step.Name)
		}
		for _, argument := range step.Args {
			if strings.ContainsRune(argument, '\x00') {
				return fmt.Errorf("step %q has an argument containing NUL", step.Name)
			}
		}
	}
	return nil
}

func resolveRecipeExecutable(recipePath string, step RecipeStep) (ResolvedExecutable, error) {
	path := filepath.FromSlash(step.Exec)
	if strings.ContainsAny(step.Exec, `/\\`) {
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(recipePath), path)
		}
		path = filepath.Clean(path)
	} else {
		resolved, err := exec.LookPath(step.Exec)
		if err != nil {
			return ResolvedExecutable{}, fmt.Errorf("recipe step %q exec %q was not found in PATH: %w", step.Name, step.Exec, err)
		}
		path, err = filepath.Abs(resolved)
		if err != nil {
			return ResolvedExecutable{}, fmt.Errorf("resolve recipe step %q exec %q: %w", step.Name, step.Exec, err)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return ResolvedExecutable{}, fmt.Errorf("recipe step %q executable %s: %w", step.Name, path, err)
	}
	if !info.Mode().IsRegular() {
		return ResolvedExecutable{}, fmt.Errorf("recipe step %q executable %s must resolve to a regular file", step.Name, path)
	}
	if info.Mode()&0o111 == 0 {
		return ResolvedExecutable{}, fmt.Errorf("recipe step %q executable %s is not executable", step.Name, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ResolvedExecutable{}, err
	}
	digest := sha256.Sum256(data)
	return ResolvedExecutable{Name: step.Name, Exec: step.Exec, Path: path, SHA256: hex.EncodeToString(digest[:]), Args: append([]string(nil), step.Args...)}, nil
}

func relativeEvidencePath(base, path string) string {
	relative, err := filepath.Rel(base, path)
	if err != nil {
		return filepath.Base(path)
	}
	return relative
}

func populateGitEvidence(loaded *LoadedRecipe) {
	root, err := gitOutput(filepath.Dir(loaded.Path), "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return
	}
	loaded.Evidence.Path = filepath.ToSlash(relativeEvidencePath(root, loaded.Path))
	for index := range loaded.Evidence.Steps {
		loaded.Evidence.Steps[index].Executable = evidenceExecutablePath(root, loaded.Executables[index].Path)
	}
	loaded.Evidence.Commit, _ = gitOutput(root, "rev-parse", "HEAD")
	loaded.Evidence.Repository, _ = gitOutput(root, "config", "--get", "remote.origin.url")
	status, _ := gitOutput(root, "status", "--porcelain", "--untracked-files=normal")
	loaded.Evidence.Dirty = status != ""
}

func evidenceExecutablePath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(relative)
	}
	return filepath.ToSlash(path)
}

func gitOutput(directory string, arguments ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

type CommandRunner interface {
	Run(context.Context, string, []string, string, []string, io.Writer, io.Writer) error
}

type ExecCommandRunner struct{}

func (ExecCommandRunner) Run(ctx context.Context, path string, arguments []string, directory string, environment []string, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, path, arguments...)
	command.Dir = directory
	command.Env = environment
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

type PreparedRecipe struct {
	Loaded    LoadedRecipe `json:"loaded"`
	Workspace string       `json:"workspace"`
	Inputs    string       `json:"inputs"`
	Probe     Probe        `json:"probe"`
}

type recipeState struct {
	Kind     string `json:"kind"`
	Schema   int    `json:"schema"`
	Identity string `json:"identity"`
	Status   string `json:"status"`
	Probe    *Probe `json:"probe,omitempty"`
}

func RecipeIdentity(loaded LoadedRecipe, destination string) string {
	hash := sha256.New()
	hash.Write([]byte(loaded.SHA256))
	hash.Write([]byte{0})
	hash.Write([]byte(destination))
	for _, executable := range loaded.Executables {
		hash.Write([]byte{0})
		hash.Write([]byte(executable.SHA256))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// PrepareRecipe executes each explicitly declared command in order. Commands
// share one WALDO-owned working directory and contractually stop after
// populating it; the resulting regular files are independently probed.
func PrepareRecipe(ctx context.Context, loaded LoadedRecipe, destination, stagingBase string, runner CommandRunner, stdout, stderr io.Writer) (PreparedRecipe, error) {
	if runner == nil {
		return PreparedRecipe{}, fmt.Errorf("recipe command runner is required")
	}
	base, err := filepath.Abs(stagingBase)
	if err != nil {
		return PreparedRecipe{}, err
	}
	identity := RecipeIdentity(loaded, destination)
	workspace := filepath.Join(base, recipeWorkspace, identity)
	inputs := filepath.Join(workspace, "inputs")
	statePath := filepath.Join(workspace, recipeJournal)
	state, exists, err := loadRecipeState(statePath)
	if err != nil {
		return PreparedRecipe{}, err
	}
	if exists && state.Identity != identity {
		return PreparedRecipe{}, fmt.Errorf("recipe workspace belongs to %s, not %s", state.Identity, identity)
	}
	if exists && state.Status == "prepared" {
		if state.Probe == nil {
			return PreparedRecipe{}, fmt.Errorf("prepared recipe workspace has no input probe")
		}
		observed, err := ProbePaths(ctx, []string{inputs})
		if err != nil {
			return PreparedRecipe{}, err
		}
		if !sameProbe(*state.Probe, observed) {
			return PreparedRecipe{}, fmt.Errorf("prepared recipe inputs changed in %s", inputs)
		}
		return PreparedRecipe{Loaded: loaded, Workspace: workspace, Inputs: inputs, Probe: observed}, nil
	}
	if exists && state.Status != "preparing" {
		return PreparedRecipe{}, fmt.Errorf("unsupported recipe workspace status %q", state.Status)
	}
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return PreparedRecipe{}, err
	}
	if err := os.RemoveAll(inputs); err != nil {
		return PreparedRecipe{}, err
	}
	if err := os.MkdirAll(inputs, 0o700); err != nil {
		return PreparedRecipe{}, err
	}
	state = recipeState{Kind: "waldo-ingest-recipe-state", Schema: 1, Identity: identity, Status: "preparing"}
	if err := writeRecipeState(statePath, state); err != nil {
		return PreparedRecipe{}, err
	}
	environment := recipeEnvironment(inputs, loaded.Path)
	for position, executable := range loaded.Executables {
		if err := verifyRecipeExecutable(executable); err != nil {
			return PreparedRecipe{}, err
		}
		emitProgress(ctx, ProgressEvent{Phase: "fetch", Status: "started", Input: executable.Name, Sequence: position + 1})
		if err := runner.Run(ctx, executable.Path, executable.Args, inputs, environment, stdout, stderr); err != nil {
			return PreparedRecipe{}, fmt.Errorf("recipe step %q failed: %w", executable.Name, err)
		}
		if err := verifyRecipeExecutable(executable); err != nil {
			return PreparedRecipe{}, err
		}
		emitProgress(ctx, ProgressEvent{Phase: "fetch", Status: "completed", Input: executable.Name, Sequence: position + 1})
	}
	recipeData, err := os.ReadFile(loaded.Path)
	if err != nil {
		return PreparedRecipe{}, fmt.Errorf("recheck ingest recipe: %w", err)
	}
	recipeDigest := sha256.Sum256(recipeData)
	if hex.EncodeToString(recipeDigest[:]) != loaded.SHA256 {
		return PreparedRecipe{}, fmt.Errorf("ingest recipe %s changed while its steps ran", loaded.Path)
	}
	probe, err := ProbePaths(ctx, []string{inputs})
	if err != nil {
		return PreparedRecipe{}, fmt.Errorf("probe recipe output: %w", err)
	}
	state.Status, state.Probe = "prepared", &probe
	if err := writeRecipeState(statePath, state); err != nil {
		return PreparedRecipe{}, err
	}
	return PreparedRecipe{Loaded: loaded, Workspace: workspace, Inputs: inputs, Probe: probe}, nil
}

func verifyRecipeExecutable(executable ResolvedExecutable) error {
	info, err := os.Stat(executable.Path)
	if err != nil {
		return fmt.Errorf("recheck recipe step %q executable: %w", executable.Name, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("recipe step %q executable changed type or is no longer executable", executable.Name)
	}
	data, err := os.ReadFile(executable.Path)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != executable.SHA256 {
		return fmt.Errorf("recipe step %q executable changed after recipe validation", executable.Name)
	}
	return nil
}

func recipeEnvironment(inputs, recipePath string) []string {
	environment := slices.DeleteFunc(append([]string(nil), os.Environ()...), func(value string) bool {
		return strings.HasPrefix(value, "WALDO_FETCH_DIR=") || strings.HasPrefix(value, "WALDO_INGEST_RECIPE=")
	})
	return append(environment, "WALDO_FETCH_DIR="+inputs, "WALDO_INGEST_RECIPE="+recipePath)
}

func sameProbe(expected, observed Probe) bool {
	left, _ := json.Marshal(expected)
	right, _ := json.Marshal(observed)
	return bytes.Equal(left, right)
}

func loadRecipeState(path string) (recipeState, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return recipeState{}, false, nil
	}
	if err != nil {
		return recipeState{}, false, err
	}
	var state recipeState
	if err := json.Unmarshal(data, &state); err != nil {
		return recipeState{}, false, fmt.Errorf("%s: %w", path, err)
	}
	if state.Kind != "waldo-ingest-recipe-state" || state.Schema != 1 || state.Identity == "" {
		return recipeState{}, false, fmt.Errorf("%s: unsupported recipe state", path)
	}
	return state, true, nil
}

func writeRecipeState(path string, state recipeState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".waldo-recipe-state-*")
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
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
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
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	committed = true
	return nil
}

func PurgePreparedRecipe(prepared PreparedRecipe) error {
	if prepared.Workspace == "" || prepared.Workspace != filepath.Clean(prepared.Workspace) || filepath.Base(filepath.Dir(prepared.Workspace)) != recipeWorkspace || !validSHA256(filepath.Base(prepared.Workspace)) {
		return fmt.Errorf("refuse to purge invalid recipe workspace %q", prepared.Workspace)
	}
	parent := filepath.Dir(prepared.Workspace)
	if err := os.RemoveAll(prepared.Workspace); err != nil {
		return fmt.Errorf("purge recipe workspace %s: %w", prepared.Workspace, err)
	}
	if err := syncDirectory(parent); err != nil {
		return err
	}
	return nil
}
