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
	ComposeKind      = "waldo-ingest-compose"
	ComposeSchema    = 1
	composeMaximum   = 1 << 20
	composeJournal   = "COMPOSE.json"
	composeWorkspace = "composes"
)

var composeStepName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Compose describes source preparation followed by the normal WALDO ingest
// pipeline. Commands are source-specific external producers; their only output
// is a WALDO-owned temporary directory.
type Compose struct {
	Kind        string        `json:"kind" yaml:"kind"`
	Schema      int           `json:"schema" yaml:"schema"`
	Title       string        `json:"title" yaml:"title"`
	Description string        `json:"description,omitempty" yaml:"description,omitempty"`
	License     string        `json:"license" yaml:"license"`
	Source      ComposeSource `json:"source" yaml:"source"`
	TextColumn  string        `json:"text_column,omitempty" yaml:"text_column,omitempty"`
	Steps       []ComposeStep `json:"steps" yaml:"steps"`
}

type ComposeSource struct {
	Name     string `json:"name,omitempty" yaml:"name,omitempty"`
	URL      string `json:"url" yaml:"url"`
	Category string `json:"category" yaml:"category"`
}

type ComposeStep struct {
	Name string   `json:"name" yaml:"name"`
	Exec string   `json:"exec" yaml:"exec"`
	Args []string `json:"args,omitempty" yaml:"args,omitempty"`
}

type LoadedCompose struct {
	Compose     Compose              `json:"compose"`
	Path        string               `json:"path"`
	SHA256      string               `json:"sha256"`
	Evidence    index.Composition    `json:"evidence"`
	Executables []ResolvedExecutable `json:"executables"`
}

type ResolvedExecutable struct {
	Name   string   `json:"name"`
	Exec   string   `json:"exec"`
	Path   string   `json:"path"`
	SHA256 string   `json:"sha256"`
	Args   []string `json:"args,omitempty"`
}

// LoadCompose recognizes only a small, strictly identified YAML or JSON
// document. Ordinary source files remain inputs to content probing.
func LoadCompose(path string) (LoadedCompose, bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return LoadedCompose{}, false, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return LoadedCompose{}, false, nil
		}
		return LoadedCompose{}, false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > composeMaximum {
		return LoadedCompose{}, false, nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return LoadedCompose{}, false, err
	}
	var header struct {
		Kind string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		if bytes.Contains(data, []byte(ComposeKind)) {
			return LoadedCompose{}, true, fmt.Errorf("%s: malformed ingest compose: %w", abs, err)
		}
		return LoadedCompose{}, false, nil
	}
	if header.Kind != ComposeKind {
		return LoadedCompose{}, false, nil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var compose Compose
	if err := decoder.Decode(&compose); err != nil {
		return LoadedCompose{}, true, fmt.Errorf("%s: %w", abs, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple YAML documents are not allowed")
		}
		return LoadedCompose{}, true, fmt.Errorf("%s: %w", abs, err)
	}
	if err := compose.Validate(); err != nil {
		return LoadedCompose{}, true, fmt.Errorf("%s: %w", abs, err)
	}
	digest := sha256.Sum256(data)
	loaded := LoadedCompose{Compose: compose, Path: abs, SHA256: hex.EncodeToString(digest[:])}
	loaded.Evidence = index.Composition{Path: filepath.Base(abs), SHA256: loaded.SHA256}
	for _, step := range compose.Steps {
		resolved, err := resolveComposeExecutable(abs, step)
		if err != nil {
			return LoadedCompose{}, true, err
		}
		loaded.Executables = append(loaded.Executables, resolved)
		loaded.Evidence.Steps = append(loaded.Evidence.Steps, index.CompositionStep{
			Name: step.Name, Script: filepath.ToSlash(relativeEvidencePath(filepath.Dir(abs), resolved.Path)), SHA256: resolved.SHA256,
		})
	}
	populateGitEvidence(&loaded)
	return loaded, true, nil
}

func (compose Compose) Validate() error {
	if compose.Kind != ComposeKind || compose.Schema != ComposeSchema {
		return fmt.Errorf("unsupported ingest compose identity %q schema %d", compose.Kind, compose.Schema)
	}
	if strings.TrimSpace(compose.Title) == "" || strings.TrimSpace(compose.License) == "" {
		return fmt.Errorf("title and license are required")
	}
	if strings.TrimSpace(compose.Source.URL) == "" || strings.TrimSpace(compose.Source.Category) == "" {
		return fmt.Errorf("source url and category are required")
	}
	if _, ok := index.CanonicalSourceCategory(compose.Source.Category); !ok {
		return fmt.Errorf("unsupported source category %q", compose.Source.Category)
	}
	category, _ := index.CanonicalSourceCategory(compose.Source.Category)
	switch category {
	case index.SourcePublicDataset, index.SourcePrivateThirdParty, index.SourceOther:
	default:
		return fmt.Errorf("source category %q requires acquisition evidence fields that ingest compose does not collect yet", category)
	}
	if len(compose.Steps) == 0 {
		return fmt.Errorf("at least one fetcher step is required")
	}
	seen := map[string]bool{}
	for position, step := range compose.Steps {
		if !composeStepName.MatchString(step.Name) || seen[step.Name] {
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

func resolveComposeExecutable(composePath string, step ComposeStep) (ResolvedExecutable, error) {
	path := filepath.FromSlash(step.Exec)
	if strings.ContainsAny(step.Exec, `/\\`) {
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(composePath), path)
		}
		path = filepath.Clean(path)
	} else {
		resolved, err := exec.LookPath(step.Exec)
		if err != nil {
			return ResolvedExecutable{}, fmt.Errorf("compose step %q exec %q was not found in PATH: %w", step.Name, step.Exec, err)
		}
		path, err = filepath.Abs(resolved)
		if err != nil {
			return ResolvedExecutable{}, fmt.Errorf("resolve compose step %q exec %q: %w", step.Name, step.Exec, err)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return ResolvedExecutable{}, fmt.Errorf("compose step %q executable %s: %w", step.Name, path, err)
	}
	if !info.Mode().IsRegular() {
		return ResolvedExecutable{}, fmt.Errorf("compose step %q executable %s must resolve to a regular file", step.Name, path)
	}
	if info.Mode()&0o111 == 0 {
		return ResolvedExecutable{}, fmt.Errorf("compose step %q executable %s is not executable", step.Name, path)
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

func populateGitEvidence(loaded *LoadedCompose) {
	root, err := gitOutput(filepath.Dir(loaded.Path), "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return
	}
	loaded.Evidence.Path = filepath.ToSlash(relativeEvidencePath(root, loaded.Path))
	for index := range loaded.Evidence.Steps {
		loaded.Evidence.Steps[index].Script = evidenceExecutablePath(root, loaded.Executables[index].Path)
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

type PreparedCompose struct {
	Loaded    LoadedCompose `json:"loaded"`
	Workspace string        `json:"workspace"`
	Inputs    string        `json:"inputs"`
	Probe     Probe         `json:"probe"`
}

type composeState struct {
	Kind     string `json:"kind"`
	Schema   int    `json:"schema"`
	Identity string `json:"identity"`
	Status   string `json:"status"`
	Probe    *Probe `json:"probe,omitempty"`
}

func ComposeIdentity(loaded LoadedCompose, destination string) string {
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

// PrepareCompose executes each explicitly declared command in order. Commands
// share one WALDO-owned working directory and contractually stop after
// populating it; the resulting regular files are independently probed.
func PrepareCompose(ctx context.Context, loaded LoadedCompose, destination, stagingBase string, runner CommandRunner, stdout, stderr io.Writer) (PreparedCompose, error) {
	if runner == nil {
		return PreparedCompose{}, fmt.Errorf("compose command runner is required")
	}
	base, err := filepath.Abs(stagingBase)
	if err != nil {
		return PreparedCompose{}, err
	}
	identity := ComposeIdentity(loaded, destination)
	workspace := filepath.Join(base, composeWorkspace, identity)
	inputs := filepath.Join(workspace, "inputs")
	statePath := filepath.Join(workspace, composeJournal)
	state, exists, err := loadComposeState(statePath)
	if err != nil {
		return PreparedCompose{}, err
	}
	if exists && state.Identity != identity {
		return PreparedCompose{}, fmt.Errorf("compose workspace belongs to %s, not %s", state.Identity, identity)
	}
	if exists && state.Status == "prepared" {
		if state.Probe == nil {
			return PreparedCompose{}, fmt.Errorf("prepared compose workspace has no input probe")
		}
		observed, err := ProbePaths(ctx, []string{inputs})
		if err != nil {
			return PreparedCompose{}, err
		}
		if !sameProbe(*state.Probe, observed) {
			return PreparedCompose{}, fmt.Errorf("prepared compose inputs changed in %s", inputs)
		}
		return PreparedCompose{Loaded: loaded, Workspace: workspace, Inputs: inputs, Probe: observed}, nil
	}
	if exists && state.Status != "preparing" {
		return PreparedCompose{}, fmt.Errorf("unsupported compose workspace status %q", state.Status)
	}
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return PreparedCompose{}, err
	}
	if err := os.RemoveAll(inputs); err != nil {
		return PreparedCompose{}, err
	}
	if err := os.MkdirAll(inputs, 0o700); err != nil {
		return PreparedCompose{}, err
	}
	state = composeState{Kind: "waldo-ingest-compose-state", Schema: 1, Identity: identity, Status: "preparing"}
	if err := writeComposeState(statePath, state); err != nil {
		return PreparedCompose{}, err
	}
	environment := composeEnvironment(inputs, loaded.Path)
	for position, executable := range loaded.Executables {
		if err := verifyComposeExecutable(executable); err != nil {
			return PreparedCompose{}, err
		}
		emitProgress(ctx, ProgressEvent{Phase: "fetch", Status: "started", Input: executable.Name, Sequence: position + 1})
		if err := runner.Run(ctx, executable.Path, executable.Args, inputs, environment, stdout, stderr); err != nil {
			return PreparedCompose{}, fmt.Errorf("compose step %q failed: %w", executable.Name, err)
		}
		if err := verifyComposeExecutable(executable); err != nil {
			return PreparedCompose{}, err
		}
		emitProgress(ctx, ProgressEvent{Phase: "fetch", Status: "completed", Input: executable.Name, Sequence: position + 1})
	}
	composeData, err := os.ReadFile(loaded.Path)
	if err != nil {
		return PreparedCompose{}, fmt.Errorf("recheck ingest compose: %w", err)
	}
	composeDigest := sha256.Sum256(composeData)
	if hex.EncodeToString(composeDigest[:]) != loaded.SHA256 {
		return PreparedCompose{}, fmt.Errorf("ingest compose %s changed while its steps ran", loaded.Path)
	}
	probe, err := ProbePaths(ctx, []string{inputs})
	if err != nil {
		return PreparedCompose{}, fmt.Errorf("probe compose output: %w", err)
	}
	state.Status, state.Probe = "prepared", &probe
	if err := writeComposeState(statePath, state); err != nil {
		return PreparedCompose{}, err
	}
	return PreparedCompose{Loaded: loaded, Workspace: workspace, Inputs: inputs, Probe: probe}, nil
}

func verifyComposeExecutable(executable ResolvedExecutable) error {
	info, err := os.Stat(executable.Path)
	if err != nil {
		return fmt.Errorf("recheck compose step %q executable: %w", executable.Name, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("compose step %q executable changed type or is no longer executable", executable.Name)
	}
	data, err := os.ReadFile(executable.Path)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != executable.SHA256 {
		return fmt.Errorf("compose step %q executable changed after compose validation", executable.Name)
	}
	return nil
}

func composeEnvironment(inputs, composePath string) []string {
	environment := slices.DeleteFunc(append([]string(nil), os.Environ()...), func(value string) bool {
		return strings.HasPrefix(value, "WALDO_FETCH_DIR=") || strings.HasPrefix(value, "WALDO_COMPOSE_FILE=")
	})
	return append(environment, "WALDO_FETCH_DIR="+inputs, "WALDO_COMPOSE_FILE="+composePath)
}

func sameProbe(expected, observed Probe) bool {
	left, _ := json.Marshal(expected)
	right, _ := json.Marshal(observed)
	return bytes.Equal(left, right)
}

func loadComposeState(path string) (composeState, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return composeState{}, false, nil
	}
	if err != nil {
		return composeState{}, false, err
	}
	var state composeState
	if err := json.Unmarshal(data, &state); err != nil {
		return composeState{}, false, fmt.Errorf("%s: %w", path, err)
	}
	if state.Kind != "waldo-ingest-compose-state" || state.Schema != 1 || state.Identity == "" {
		return composeState{}, false, fmt.Errorf("%s: unsupported compose state", path)
	}
	return state, true, nil
}

func writeComposeState(path string, state composeState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".waldo-compose-state-*")
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

func PurgePreparedCompose(prepared PreparedCompose) error {
	if prepared.Workspace == "" || prepared.Workspace != filepath.Clean(prepared.Workspace) || filepath.Base(filepath.Dir(prepared.Workspace)) != composeWorkspace || !validSHA256(filepath.Base(prepared.Workspace)) {
		return fmt.Errorf("refuse to purge invalid compose workspace %q", prepared.Workspace)
	}
	parent := filepath.Dir(prepared.Workspace)
	if err := os.RemoveAll(prepared.Workspace); err != nil {
		return fmt.Errorf("purge compose workspace %s: %w", prepared.Workspace, err)
	}
	if err := syncDirectory(parent); err != nil {
		return err
	}
	return nil
}
