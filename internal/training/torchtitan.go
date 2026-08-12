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
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const TorchTitanRevision = "builtin-torchtitan-worker-schema-1-r7"

type TorchTitan struct {
	Python     string
	Version    string
	LocalProcs int    // procs per node (local visible GPUs); --nproc-per-node
	Nodes      int    // --nnodes; 0 or 1 keeps the single-node --standalone launch
	NodeRank   int    // --node-rank
	Rendezvous string // host:port; split into --master-addr/--master-port for static rendezvous
	Interface  string // NCCL_SOCKET_IFNAME
	HCA        string // NCCL_IB_HCA
	Secondary  bool   // a non-primary node: join-only, no record stream or observation
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
	if backend.LocalProcs < 1 {
		return Observation{}, fmt.Errorf("TorchTitan requires at least one local process")
	}
	if backend.Nodes > 1 {
		if _, _, err := net.SplitHostPort(backend.Rendezvous); err != nil {
			return Observation{}, fmt.Errorf("multi-node TorchTitan rendezvous %q must be host:port: %w", backend.Rendezvous, err)
		}
		if backend.NodeRank < 0 || backend.NodeRank >= backend.Nodes {
			return Observation{}, fmt.Errorf("TorchTitan node rank %d is out of range for %d nodes", backend.NodeRank, backend.Nodes)
		}
		// The primary owns the record stream broadcast from global rank 0, so a
		// non-secondary backend must be node rank 0; otherwise rank 0 would land
		// on a join-only node and the first broadcast would deadlock.
		if !backend.Secondary && backend.NodeRank != 0 {
			return Observation{}, fmt.Errorf("primary TorchTitan node must be rank 0, not %d", backend.NodeRank)
		}
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
	command := exec.CommandContext(ctx, backend.Python, backend.launchArguments(workerPath, request)...)
	command.Env = backend.environment()
	if backend.Secondary {
		return runWorkerJoin(ctx, "TorchTitan", command)
	}
	return runWorkerCommand(ctx, "TorchTitan", command, request)
}

// launchArguments builds the torch.distributed.run invocation. A single node
// keeps the original --standalone rendezvous; multiple nodes use a static
// rendezvous pinned to master-addr/master-port and disable elastic restarts so
// WALDO owns retry and resume.
//
// Static (not c10d) is deliberate: the c10d backend elects the worker-group
// master and advertises it via socket.getfqdn(), which on a containerized
// cluster resolves to the underlying host name and a random port that the
// other nodes' containers cannot reach, so every collective hangs. A static
// rendezvous binds the caller-supplied endpoint on rank 0 unconditionally and
// every secondary connects to exactly that address.
func (backend TorchTitan) launchArguments(workerPath string, request Request) []string {
	arguments := []string{"-m", "torch.distributed.run"}
	if backend.Nodes > 1 {
		// The endpoint was validated as host:port by Run before launch.
		host, port, _ := net.SplitHostPort(backend.Rendezvous)
		arguments = append(arguments,
			fmt.Sprintf("--nnodes=%d", backend.Nodes),
			fmt.Sprintf("--node-rank=%d", backend.NodeRank),
			fmt.Sprintf("--master-addr=%s", host),
			fmt.Sprintf("--master-port=%s", port),
			"--max-restarts=0",
		)
	} else {
		arguments = append(arguments, "--standalone")
	}
	return append(arguments,
		fmt.Sprintf("--nproc-per-node=%d", backend.LocalProcs),
		workerPath, request.ArtifactDirectory, request.ArtifactPrefix, "torchtitan",
	)
}

// environment builds the worker environment. PYTHONUNBUFFERED keeps
// torch.distributed.run from block-buffering the worker's progress frames, so
// WALDO's reader sees step progress live rather than only at process exit. It
// also adds the NCCL settings a multi-node run needs to reach the
// ConnectX/RoCE fabric when an interface or HCA is configured.
func (backend TorchTitan) environment() []string {
	environment := append(os.Environ(), "PYTHONUNBUFFERED=1")
	if backend.Interface != "" {
		environment = append(environment, "NCCL_SOCKET_IFNAME="+backend.Interface)
	}
	if backend.HCA != "" {
		environment = append(environment, "NCCL_IB_HCA="+backend.HCA, "NCCL_IB_DISABLE=0")
	}
	return environment
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
	Cluster    Cluster
}

// NewTorchTitanResolverForCluster resolves the TorchTitan backend for a
// specific multi-node topology. The zero Cluster is a single node.
func NewTorchTitanResolverForCluster(cluster Cluster) Resolver {
	return TorchTitanResolver{Cluster: cluster}
}

// backendForCluster builds the TorchTitan backend for one probed Python runtime
// and topology. It is shared by the resolver (primary) and the secondary-node
// launcher so the field set stays in lockstep. A Cluster with fewer than one
// node is treated as a single node.
func backendForCluster(python string, facts torchTitanProbe, cluster Cluster, secondary bool) TorchTitan {
	nodes := cluster.Nodes
	if nodes < 1 {
		nodes = 1
	}
	return TorchTitan{
		Python: python, Version: facts.TorchTitanVersion, LocalProcs: len(facts.Devices),
		Nodes: nodes, NodeRank: cluster.NodeRank, Rendezvous: cluster.Rendezvous,
		Interface: cluster.Interface, HCA: cluster.HCA, Secondary: secondary,
	}
}

// firstUsableTorchTitan probes candidates in order and returns the first that
// succeeds. When none do, it returns an empty python and the accumulated
// per-candidate failures for an actionable error.
func firstUsableTorchTitan(ctx context.Context, candidates []string, probe func(context.Context, string) (torchTitanProbe, error)) (string, torchTitanProbe, []string) {
	var failures []string
	for _, candidate := range candidates {
		facts, err := probe(ctx, candidate)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate, err))
			continue
		}
		return candidate, facts, nil
	}
	return "", torchTitanProbe{}, failures
}

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
		candidates = pythonCandidates()
	}
	probe := resolver.Probe
	if probe == nil {
		probe = probeTorchTitan
	}
	python, facts, failures := firstUsableTorchTitan(ctx, candidates, probe)
	if python == "" {
		detail := strings.Join(failures, "; ")
		if detail != "" {
			detail = ": " + detail
		}
		return Selection{}, fmt.Errorf("no usable TorchTitan runtime found; install a matching TorchTitan and PyTorch build from https://github.com/pytorch/torchtitan#installation%s", detail)
	}
	backend := backendForCluster(python, facts, resolver.Cluster, false)
	nodes, localProcs := backend.Nodes, backend.LocalProcs
	descriptor := backend.Descriptor()
	execution := Execution{
		Backend: descriptor.Identity, Framework: descriptor.Framework,
		Runtime: fmt.Sprintf("%s; Python %s; TorchTitan %s; PyTorch %s", python, facts.PythonVersion, facts.TorchTitanVersion, facts.TorchVersion),
		Host:    Host{OS: hostOS, Architecture: hostArch}, Nodes: nodes, WorldSize: nodes * localProcs,
	}
	// One accelerator entry per rank across every node (ADR 0025). Nodes are
	// assumed homogeneous, which holds for a DGX Spark cluster (1 GPU each).
	for node := 0; node < nodes; node++ {
		for _, device := range facts.Devices {
			execution.Accelerators = append(execution.Accelerators, Accelerator{Manufacturer: device.Manufacturer, Model: device.Model, MemoryBytes: device.MemoryBytes})
		}
	}
	return Selection{Backend: backend, Execution: execution}, nil
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
	if _, _, err := ResolveTokenizer(architecture.Tokenizer.Name, architecture.Tokenizer.Revision, architecture.VocabularySize); err != nil {
		return fmt.Errorf("%s backend: %w", label, err)
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

// RunSecondaryTorchTitan launches a secondary distributed node that joins an
// existing rendezvous and runs its local ranks. Those ranks receive WALDO's
// canonical record stream from global rank 0 over NCCL, so this node neither
// streams records nor authors lifecycle records; the primary node owns the run
// BOM and artifacts. scratchDirectory holds only the ephemeral worker program.
func RunSecondaryTorchTitan(ctx context.Context, cluster Cluster, scratchDirectory string) error {
	if cluster.Nodes < 2 {
		return fmt.Errorf("secondary TorchTitan requires at least two nodes")
	}
	if cluster.NodeRank < 1 || cluster.NodeRank >= cluster.Nodes {
		return fmt.Errorf("secondary TorchTitan node rank %d is out of range for %d nodes", cluster.NodeRank, cluster.Nodes)
	}
	if cluster.Rendezvous == "" || cluster.RendezvousID == "" {
		return fmt.Errorf("secondary TorchTitan requires a rendezvous endpoint and id")
	}
	python, facts, failures := firstUsableTorchTitan(ctx, pythonCandidates(), probeTorchTitan)
	if python == "" {
		detail := strings.Join(failures, "; ")
		if detail != "" {
			detail = ": " + detail
		}
		return fmt.Errorf("no usable TorchTitan runtime found for the secondary node%s", detail)
	}
	backend := backendForCluster(python, facts, cluster, true)
	_, err := backend.Run(ctx, Request{ArtifactDirectory: scratchDirectory, ArtifactPrefix: "artifacts"})
	return err
}
