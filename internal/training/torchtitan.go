// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package training

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const TorchTitanRevision = "builtin-torchtitan-worker-schema-1-r3"

type TorchTitan struct {
	Python    string
	Version   string
	WorldSize int
}

func (backend TorchTitan) Descriptor() Descriptor {
	return Descriptor{
		Identity:  Identity{Name: BackendTorchTitan, Revision: TorchTitanRevision},
		Framework: BackendTorchTitan,
		Capabilities: Capabilities{
			Objectives: []string{"causal-language-modeling"}, CheckpointResume: true, Distributed: true, Safetensors: true,
		},
	}
}

func (backend TorchTitan) Run(ctx context.Context, request Request) (Observation, error) {
	if backend.Python == "" {
		return Observation{}, fmt.Errorf("TorchTitan Python runtime is required")
	}
	if backend.WorldSize < 1 {
		return Observation{}, fmt.Errorf("TorchTitan world size must be positive")
	}
	if err := os.MkdirAll(request.ArtifactDirectory, 0o755); err != nil {
		return Observation{}, fmt.Errorf("create TorchTitan artifact directory: %w", err)
	}
	worker, err := os.CreateTemp(request.ArtifactDirectory, ".waldo-torchtitan-worker-*.py")
	if err != nil {
		return Observation{}, fmt.Errorf("stage embedded TorchTitan worker: %w", err)
	}
	workerPath := worker.Name()
	defer os.Remove(workerPath)
	if _, err := worker.Write(pyTorchWorker); err != nil {
		_ = worker.Close()
		return Observation{}, err
	}
	if err := worker.Sync(); err != nil {
		_ = worker.Close()
		return Observation{}, err
	}
	if err := worker.Close(); err != nil {
		return Observation{}, err
	}
	command := exec.CommandContext(ctx, backend.Python,
		"-m", "torch.distributed.run", "--standalone",
		fmt.Sprintf("--nproc-per-node=%d", backend.WorldSize),
		workerPath, request.ArtifactDirectory, request.ArtifactPrefix, "torchtitan",
	)
	return runWorkerCommand(ctx, "TorchTitan", command, request)
}

type torchTitanDevice struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	MemoryBytes  uint64 `json:"memory_bytes"`
}

type torchTitanProbe struct {
	PythonVersion     string             `json:"python_version"`
	TorchVersion      string             `json:"torch_version"`
	TorchTitanVersion string             `json:"torchtitan_version"`
	Devices           []torchTitanDevice `json:"devices"`
}

type TorchTitanResolver struct {
	Candidates []string
	Probe      func(context.Context, string) (torchTitanProbe, error)
	OS         string
	Arch       string
}

func NewTorchTitanResolver() Resolver { return TorchTitanResolver{} }

func (resolver TorchTitanResolver) Resolve(ctx context.Context, request ResolveRequest) (Selection, error) {
	hostOS, hostArch := resolver.OS, resolver.Arch
	if hostOS == "" {
		hostOS = runtime.GOOS
	}
	if hostArch == "" {
		hostArch = runtime.GOARCH
	}
	if hostOS != "linux" {
		return Selection{}, fmt.Errorf("TorchTitan training requires Linux; this host is %s/%s", hostOS, hostArch)
	}
	if err := validateTorchArchitecture(request.Architecture, "TorchTitan"); err != nil {
		return Selection{}, err
	}
	candidates := resolver.Candidates
	if len(candidates) == 0 {
		candidates = mlxPythonCandidates()
	}
	probe := resolver.Probe
	if probe == nil {
		probe = probeTorchTitan
	}
	var failures []string
	for _, candidate := range candidates {
		facts, err := probe(ctx, candidate)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate, err))
			continue
		}
		backend := TorchTitan{Python: candidate, Version: facts.TorchTitanVersion, WorldSize: len(facts.Devices)}
		descriptor := backend.Descriptor()
		execution := Execution{
			Backend: descriptor.Identity, Framework: descriptor.Framework,
			Runtime: fmt.Sprintf("%s; Python %s; TorchTitan %s; PyTorch %s", candidate, facts.PythonVersion, facts.TorchTitanVersion, facts.TorchVersion),
			Host:    Host{OS: hostOS, Architecture: hostArch}, Nodes: 1, WorldSize: len(facts.Devices),
		}
		for _, device := range facts.Devices {
			execution.Accelerators = append(execution.Accelerators, Accelerator{Manufacturer: device.Manufacturer, Model: device.Model, MemoryBytes: device.MemoryBytes})
		}
		return Selection{Backend: backend, Execution: execution}, nil
	}
	detail := strings.Join(failures, "; ")
	if detail != "" {
		detail = ": " + detail
	}
	return Selection{}, fmt.Errorf("no usable TorchTitan runtime found; install a matching TorchTitan and PyTorch build from https://github.com/pytorch/torchtitan#installation%s", detail)
}

func validateTorchArchitecture(raw json.RawMessage, label string) error {
	var architecture struct {
		Family         string `json:"family"`
		VocabularySize uint64 `json:"vocabulary_size"`
		Tokenizer      struct {
			Name     string `json:"name"`
			Revision string `json:"revision"`
		} `json:"tokenizer"`
	}
	if err := json.Unmarshal(raw, &architecture); err != nil {
		return fmt.Errorf("decode architecture for %s: %w", label, err)
	}
	if architecture.Family != "decoder-transformer" {
		return fmt.Errorf("%s backend does not support architecture family %q", label, architecture.Family)
	}
	if architecture.Tokenizer.Name != "byte" || architecture.Tokenizer.Revision != "builtin-byte-schema-1" || architecture.VocabularySize != 259 {
		return fmt.Errorf("%s backend currently requires tokenizer byte@builtin-byte-schema-1 with vocabulary_size 259; model pins %s@%s with vocabulary_size %d", label, architecture.Tokenizer.Name, architecture.Tokenizer.Revision, architecture.VocabularySize)
	}
	return nil
}

const torchTitanProbeProgram = `
import importlib.metadata
import json
import platform
import torch
import torchtitan
from torch.distributed._composable.fsdp import fully_shard
from torch.distributed.checkpoint.state_dict import get_model_state_dict, StateDictOptions
from torchtitan.distributed import ParallelDims

if not torch.cuda.is_available() or torch.cuda.device_count() < 1:
    raise RuntimeError("TorchTitan requires at least one visible CUDA or ROCm GPU")
manufacturer = "AMD" if torch.version.hip else "NVIDIA"
devices = []
for index in range(torch.cuda.device_count()):
    properties = torch.cuda.get_device_properties(index)
    value = torch.tensor([1.0], device=f"cuda:{index}")
    torch.sum(value).item()
    devices.append({"manufacturer": manufacturer, "model": properties.name, "memory_bytes": properties.total_memory})
print(json.dumps({
    "python_version": platform.python_version(),
    "torch_version": torch.__version__,
    "torchtitan_version": importlib.metadata.version("torchtitan"),
    "devices": devices,
}))
`

func probeTorchTitan(ctx context.Context, python string) (torchTitanProbe, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, python, "-c", torchTitanProbeProgram)
	var stderr cappedBuffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		if probeCtx.Err() != nil {
			return torchTitanProbe{}, probeCtx.Err()
		}
		return torchTitanProbe{}, fmt.Errorf("probe failed%s", workerStderr(stderr.String()))
	}
	var facts torchTitanProbe
	if err := json.Unmarshal(bytes.TrimSpace(output), &facts); err != nil {
		return torchTitanProbe{}, fmt.Errorf("invalid probe output: %w", err)
	}
	if facts.PythonVersion == "" || facts.TorchVersion == "" || facts.TorchTitanVersion == "" || len(facts.Devices) == 0 {
		return torchTitanProbe{}, fmt.Errorf("incomplete probe output")
	}
	for _, device := range facts.Devices {
		if device.Manufacturer == "" || device.Model == "" || device.MemoryBytes == 0 {
			return torchTitanProbe{}, fmt.Errorf("incomplete accelerator probe output")
		}
	}
	return facts, nil
}
