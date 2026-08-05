package training

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/openwaldo/waldo-new/internal/mlxruntime"
)

const MLXRevision = "builtin-mlx-worker-schema-1"

//go:embed workers/mlx.py
var mlxWorker []byte

type MLX struct {
	Python  string
	Version string
}

func (backend MLX) Descriptor() Descriptor {
	return Descriptor{
		Identity:  Identity{Name: "mlx", Revision: MLXRevision},
		Framework: "mlx",
		Capabilities: Capabilities{
			Objectives: []string{"causal-language-modeling"}, Safetensors: true,
		},
	}
}

func (backend MLX) Run(ctx context.Context, request Request) (Observation, error) {
	if backend.Python == "" {
		return Observation{}, fmt.Errorf("MLX Python runtime is required")
	}
	if request.Records == nil {
		return Observation{}, fmt.Errorf("MLX backend received no canonical record stream")
	}
	if err := os.MkdirAll(request.ArtifactDirectory, 0o755); err != nil {
		return Observation{}, fmt.Errorf("create MLX artifact directory: %w", err)
	}
	command := exec.CommandContext(ctx, backend.Python, "-c", mlxruntime.WithModel(mlxWorker), request.ArtifactDirectory, request.ArtifactPrefix)
	stdin, err := command.StdinPipe()
	if err != nil {
		return Observation{}, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return Observation{}, err
	}
	var stderr cappedBuffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return Observation{}, fmt.Errorf("start MLX worker with %s: %w", backend.Python, err)
	}

	type workerResult struct {
		observation Observation
		err         error
	}
	result := make(chan workerResult, 1)
	go func() {
		var observation Observation
		completed := false
		err := ReadWorkerOutput(stdout, func(frame WorkerOutputFrame) error {
			switch frame.Kind {
			case "event":
				if request.Report != nil {
					request.Report(*frame.Event)
				}
			case "complete":
				if completed {
					return fmt.Errorf("MLX worker returned more than one completion")
				}
				completed = true
				observation = *frame.Observation
			case "error":
				return errors.New(frame.Error)
			}
			return nil
		})
		if err == nil && !completed {
			err = fmt.Errorf("MLX worker exited without a completion observation")
		}
		if err != nil && command.Process != nil {
			_ = command.Process.Kill()
		}
		result <- workerResult{observation: observation, err: err}
	}()

	begin := WorkerBegin{
		RunID: request.RunID, Stage: request.Stage, Objective: request.Objective,
		ArchitectureSHA256: request.ArchitectureSHA256, Architecture: request.Architecture,
		Parameters: request.Parameters,
	}
	if request.Initialization != nil {
		begin.Initialization = &WorkerInitialization{
			SourceRunID: request.Initialization.SourceRunID,
			Artifact:    request.Initialization.Artifact,
			Path:        request.Initialization.Path,
		}
	}
	writeErr := WriteWorkerInput(ctx, stdin, begin, request.Records)
	closeErr := stdin.Close()
	waitErr := command.Wait()
	worker := <-result
	if worker.err != nil {
		return Observation{}, fmt.Errorf("MLX worker: %w%s", worker.err, workerStderr(stderr.String()))
	}
	if writeErr != nil && !errors.Is(writeErr, io.ErrClosedPipe) {
		return Observation{}, fmt.Errorf("stream records to MLX worker: %w%s", writeErr, workerStderr(stderr.String()))
	}
	if closeErr != nil && waitErr == nil {
		return Observation{}, fmt.Errorf("close MLX worker input: %w", closeErr)
	}
	if waitErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Observation{}, ctxErr
		}
		return Observation{}, fmt.Errorf("MLX worker exited: %w%s", waitErr, workerStderr(stderr.String()))
	}
	return worker.observation, nil
}

type mlxProbe struct {
	PythonVersion string `json:"python_version"`
	MLXVersion    string `json:"mlx_version"`
	Accelerator   string `json:"accelerator"`
	MemoryBytes   uint64 `json:"memory_bytes"`
}

type MLXResolver struct {
	Candidates []string
	Probe      func(context.Context, string) (mlxProbe, error)
	OS         string
	Arch       string
}

func NewMLXResolver() Resolver { return MLXResolver{} }

func (resolver MLXResolver) Resolve(ctx context.Context, request ResolveRequest) (Selection, error) {
	hostOS, hostArch := resolver.OS, resolver.Arch
	if hostOS == "" {
		hostOS = runtime.GOOS
	}
	if hostArch == "" {
		hostArch = runtime.GOARCH
	}
	if hostOS != "darwin" || hostArch != "arm64" {
		return Selection{}, fmt.Errorf("no real training backend is available for %s/%s; MLX requires Apple Silicon", hostOS, hostArch)
	}
	var architecture struct {
		Family         string `json:"family"`
		VocabularySize uint64 `json:"vocabulary_size"`
		Tokenizer      struct {
			Name     string `json:"name"`
			Revision string `json:"revision"`
		} `json:"tokenizer"`
	}
	if err := json.Unmarshal(request.Architecture, &architecture); err != nil {
		return Selection{}, fmt.Errorf("decode architecture for MLX: %w", err)
	}
	if architecture.Family != "decoder-transformer" {
		return Selection{}, fmt.Errorf("MLX backend does not support architecture family %q", architecture.Family)
	}
	if architecture.Tokenizer.Name != "byte" || architecture.Tokenizer.Revision != "builtin-byte-schema-1" || architecture.VocabularySize != 259 {
		return Selection{}, fmt.Errorf("MLX backend currently requires tokenizer byte@builtin-byte-schema-1 with vocabulary_size 259; model pins %s@%s with vocabulary_size %d", architecture.Tokenizer.Name, architecture.Tokenizer.Revision, architecture.VocabularySize)
	}
	candidates := resolver.Candidates
	if len(candidates) == 0 {
		candidates = mlxPythonCandidates()
	}
	probe := resolver.Probe
	if probe == nil {
		probe = probeMLX
	}
	var failures []string
	for _, candidate := range candidates {
		facts, err := probe(ctx, candidate)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate, err))
			continue
		}
		backend := MLX{Python: candidate, Version: facts.MLXVersion}
		descriptor := backend.Descriptor()
		return Selection{Backend: backend, Execution: Execution{
			Backend: descriptor.Identity, Framework: descriptor.Framework,
			Runtime:      fmt.Sprintf("%s; Python %s; MLX %s", candidate, facts.PythonVersion, facts.MLXVersion),
			Host:         Host{OS: hostOS, Architecture: hostArch},
			Accelerators: []Accelerator{{Manufacturer: "Apple", Model: facts.Accelerator, MemoryBytes: facts.MemoryBytes}},
			Nodes:        1, WorldSize: 1,
		}}, nil
	}
	detail := strings.Join(failures, "; ")
	if detail != "" {
		detail = ": " + detail
	}
	return Selection{}, fmt.Errorf("no usable MLX runtime found; install MLX into a Python 3 environment on PATH%s", detail)
}

func FakeResolver() Resolver {
	return ResolverFunc(func(_ context.Context, _ ResolveRequest) (Selection, error) {
		backend := Fake{}
		descriptor := backend.Descriptor()
		return Selection{Backend: backend, Execution: Execution{
			Backend: descriptor.Identity, Framework: descriptor.Framework, Runtime: "explicit-test-simulation",
			Host: Host{OS: runtime.GOOS, Architecture: runtime.GOARCH}, Nodes: 1, WorldSize: 1,
		}}, nil
	})
}

func mlxPythonCandidates() []string {
	var candidates []string
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			candidates = append(candidates, path)
		}
	}
	candidates = append(candidates, "/opt/homebrew/bin/python3", "/usr/local/bin/python3")
	seen := map[string]bool{}
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		if info, err := os.Stat(candidate); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		seen[candidate] = true
		result = append(result, candidate)
	}
	return result
}

const mlxProbeProgram = `
import importlib.metadata
import json
import platform
import subprocess
import sys
import mlx.core as mx
mx.eval(mx.array([1], dtype=mx.int32))
def sysctl(name, default):
    try:
        return subprocess.check_output(["/usr/sbin/sysctl", "-n", name], text=True).strip()
    except Exception:
        return default
print(json.dumps({
    "python_version": platform.python_version(),
    "mlx_version": importlib.metadata.version("mlx"),
    "accelerator": sysctl("machdep.cpu.brand_string", "Apple Silicon GPU"),
    "memory_bytes": int(sysctl("hw.memsize", "0")),
}))
`

func probeMLX(ctx context.Context, python string) (mlxProbe, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, python, "-c", mlxProbeProgram)
	var stderr cappedBuffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		if probeCtx.Err() != nil {
			return mlxProbe{}, probeCtx.Err()
		}
		return mlxProbe{}, fmt.Errorf("probe failed%s", workerStderr(stderr.String()))
	}
	var facts mlxProbe
	if err := json.Unmarshal(bytes.TrimSpace(output), &facts); err != nil {
		return mlxProbe{}, fmt.Errorf("invalid probe output: %w", err)
	}
	if facts.PythonVersion == "" || facts.MLXVersion == "" || facts.Accelerator == "" {
		return mlxProbe{}, fmt.Errorf("incomplete probe output")
	}
	return facts, nil
}

type cappedBuffer struct {
	mutex sync.Mutex
	data  []byte
}

func (buffer *cappedBuffer) Write(data []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	const limit = 64 * 1024
	remaining := limit - len(buffer.data)
	if remaining > 0 {
		buffer.data = append(buffer.data, data[:min(len(data), remaining)]...)
	}
	return len(data), nil
}

func (buffer *cappedBuffer) String() string {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return string(buffer.data)
}

func workerStderr(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return "; stderr: " + value
}
