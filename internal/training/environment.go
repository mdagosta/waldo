package training

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	BackendAuto       = "auto"
	BackendMLX        = "mlx"
	BackendTorchTitan = "torchtitan"
	BackendPyTorch    = "pytorch"
	BackendFake       = "fake"
)

var errPythonPackageNotFound = errors.New("Python package not found")

type pythonPackage struct {
	Python  string
	Version string
}

// EnvironmentResolver keeps host policy separate from framework adapters.
// A new adapter only needs to be registered in Resolvers; automatic host and
// installation selection remains here.
type EnvironmentResolver struct {
	Preference string
	OS         string
	Arch       string
	Candidates []string
	Resolvers  map[string]Resolver
	Probe      func(context.Context, []string, string, string) (pythonPackage, error)
}

func NewEnvironmentResolver(preference string) Resolver {
	return EnvironmentResolver{
		Preference: preference,
		Resolvers: map[string]Resolver{
			BackendMLX:        NewMLXResolver(),
			BackendTorchTitan: NewTorchTitanResolver(),
			BackendPyTorch:    NewPyTorchResolver(),
			BackendFake:       FakeResolver(),
		},
	}
}

func (resolver EnvironmentResolver) Resolve(ctx context.Context, request ResolveRequest) (Selection, error) {
	preference := strings.ToLower(strings.TrimSpace(resolver.Preference))
	if preference == "" {
		preference = BackendAuto
	}
	hostOS, hostArch := resolver.OS, resolver.Arch
	if hostOS == "" {
		hostOS = runtime.GOOS
	}
	if hostArch == "" {
		hostArch = runtime.GOARCH
	}

	selected := preference
	var detected *pythonPackage
	if preference == BackendAuto {
		var err error
		selected, detected, err = resolver.automaticBackend(ctx, hostOS, hostArch)
		if err != nil {
			return Selection{}, err
		}
	}
	if selected == BackendMLX && (hostOS != "darwin" || hostArch != "arm64") {
		return Selection{}, fmt.Errorf("model.backend=%s selected MLX, but MLX training requires macOS on Apple Silicon; use model.backend=auto on this %s/%s host", preference, hostOS, hostArch)
	}
	if selected == BackendTorchTitan || selected == BackendPyTorch {
		if detected == nil {
			distribution, module := selected, selected
			if selected == BackendPyTorch {
				distribution, module = "torch", "torch"
			}
			installation, err := resolver.probe()(ctx, resolver.candidates(), distribution, module)
			if err != nil {
				if errors.Is(err, errPythonPackageNotFound) {
					return Selection{}, backendInstallError(preference, selected, hostOS, hostArch)
				}
				return Selection{}, fmt.Errorf("probe selected %s backend: %w", selected, err)
			}
			detected = &installation
		}
		adapter := resolver.resolver(selected)
		if adapter == nil {
			return Selection{}, fmt.Errorf("model.backend=%s selected %s %s in %s, but this WALDO build does not yet include the %s execution adapter", preference, backendDisplayName(selected), detected.Version, detected.Python, backendDisplayName(selected))
		}
		return adapter.Resolve(ctx, request)
	}
	adapter := resolver.resolver(selected)
	if adapter == nil {
		return Selection{}, fmt.Errorf("model.backend=%s selected unsupported backend %q", preference, selected)
	}
	selection, err := adapter.Resolve(ctx, request)
	if err != nil && selected == BackendMLX && strings.Contains(err.Error(), "no usable MLX runtime") {
		return Selection{}, fmt.Errorf("model.backend=%s selected MLX, but it is not installed or usable; install it with `python3 -m pip install mlx` (requires macOS 14+, Apple Silicon, and native Python 3.10+): %w", preference, err)
	}
	return selection, err
}

func (resolver EnvironmentResolver) automaticBackend(ctx context.Context, hostOS, hostArch string) (string, *pythonPackage, error) {
	switch hostOS {
	case "darwin":
		// MLX is the sole automatic macOS choice. An Intel Mac receives the
		// platform-specific error in Resolve instead of an unrelated fallback.
		return BackendMLX, nil, nil
	case "linux":
		for _, candidate := range []struct {
			backend      string
			distribution string
			module       string
		}{
			{BackendTorchTitan, "torchtitan", "torchtitan"},
			{BackendPyTorch, "torch", "torch"},
		} {
			installation, err := resolver.probe()(ctx, resolver.candidates(), candidate.distribution, candidate.module)
			if err == nil {
				return candidate.backend, &installation, nil
			}
			if !errors.Is(err, errPythonPackageNotFound) {
				return "", nil, fmt.Errorf("probe %s: %w", backendDisplayName(candidate.backend), err)
			}
		}
		return "", nil, fmt.Errorf("model.backend=auto found neither TorchTitan nor PyTorch on %s/%s; install TorchTitan from https://github.com/pytorch/torchtitan#installation or choose the correct PyTorch package at https://pytorch.org/get-started/locally/", hostOS, hostArch)
	default:
		return "", nil, fmt.Errorf("model.backend=auto has no training backend policy for %s/%s; configure a supported backend explicitly", hostOS, hostArch)
	}
}

func (resolver EnvironmentResolver) resolver(name string) Resolver {
	if resolver.Resolvers != nil {
		return resolver.Resolvers[name]
	}
	if name == BackendMLX {
		return NewMLXResolver()
	}
	if name == BackendPyTorch {
		return NewPyTorchResolver()
	}
	if name == BackendTorchTitan {
		return NewTorchTitanResolver()
	}
	if name == BackendFake {
		return FakeResolver()
	}
	return nil
}

func (resolver EnvironmentResolver) candidates() []string {
	if len(resolver.Candidates) > 0 {
		return resolver.Candidates
	}
	return mlxPythonCandidates()
}

func (resolver EnvironmentResolver) probe() func(context.Context, []string, string, string) (pythonPackage, error) {
	if resolver.Probe != nil {
		return resolver.Probe
	}
	return probePythonPackage
}

func backendDisplayName(name string) string {
	switch name {
	case BackendMLX:
		return "MLX"
	case BackendTorchTitan:
		return "TorchTitan"
	case BackendPyTorch:
		return "PyTorch"
	default:
		return name
	}
}

func backendInstallError(preference, selected, hostOS, hostArch string) error {
	switch selected {
	case BackendTorchTitan:
		return fmt.Errorf("model.backend=%s selected TorchTitan, but the `torchtitan` Python package is not installed on %s/%s; follow https://github.com/pytorch/torchtitan#installation", preference, hostOS, hostArch)
	case BackendPyTorch:
		return fmt.Errorf("model.backend=%s selected PyTorch, but the `torch` Python package is not installed on %s/%s; choose the CUDA, ROCm, or CPU install for this host at https://pytorch.org/get-started/locally/", preference, hostOS, hostArch)
	default:
		return fmt.Errorf("model.backend=%s selected %s, but it is not installed on %s/%s", preference, selected, hostOS, hostArch)
	}
}

const pythonPackageProbeProgram = `
import importlib.metadata
import importlib.util
import json
import sys
distribution, module = sys.argv[1], sys.argv[2]
spec = importlib.util.find_spec(module)
if spec is None:
    print(json.dumps({"installed": False}))
else:
    try:
        version = importlib.metadata.version(distribution)
    except importlib.metadata.PackageNotFoundError:
        version = "unknown"
    print(json.dumps({"installed": True, "version": version}))
`

func probePythonPackage(ctx context.Context, candidates []string, distribution, module string) (pythonPackage, error) {
	type result struct {
		Installed bool   `json:"installed"`
		Version   string `json:"version"`
	}
	var failures []string
	for _, python := range candidates {
		probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		command := exec.CommandContext(probeCtx, python, "-c", pythonPackageProbeProgram, distribution, module)
		var stderr cappedBuffer
		command.Stderr = &stderr
		output, err := command.Output()
		cancel()
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s%s", python, workerStderr(stderr.String())))
			continue
		}
		var facts result
		if err := json.Unmarshal(bytes.TrimSpace(output), &facts); err != nil {
			failures = append(failures, fmt.Sprintf("%s: invalid probe output", python))
			continue
		}
		if facts.Installed {
			return pythonPackage{Python: python, Version: facts.Version}, nil
		}
	}
	if len(failures) > 0 && len(failures) == len(candidates) {
		return pythonPackage{}, fmt.Errorf("no Python runtime could be probed: %s", strings.Join(failures, "; "))
	}
	return pythonPackage{}, errPythonPackageNotFound
}
