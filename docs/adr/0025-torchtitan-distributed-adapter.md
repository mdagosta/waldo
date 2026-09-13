# ADR 0025: Use TorchTitan for distributed training

Status: accepted

## Decision

TorchTitan is the distributed Linux adapter. It requires a compatible
TorchTitan and PyTorch installation and visible CUDA or ROCm devices. WALDO
records every worker rank in the execution topology.

Single-node execution launches one local rank per visible GPU. Multi-host
execution uses a hostfile containing only host names. Rank 0 stages its exact
binary and launches secondary workers through non-interactive SSH. It verifies
a homogeneous runtime, GPU count, and accelerator topology before materializing
data. Manual node-rank and rendezvous flags remain a compatibility interface.

Rank zero alone reads and tokenizes WALDO's deterministic worker stream and
broadcasts compact token frames through NCCL. Each rank deterministically
selects a different slice of the declared global batch; losses, token counts,
and corpus consumption are aggregated across ranks. Secondary hosts do not
need the index, corpus objects, lookaside credentials, shared model storage,
or NFS. WALDO selects complete-model data parallelism, host-local hybrid
sharding, or full FSDP2 sharding from the model-state estimate, GPU memory, and
host topology. NCCL uses available local GPU links. Rank zero commits
checkpoints, terminal Safetensors, and observations.

Multi-node interrupted runs resume from the newest verified checkpoint when
the exact training command is repeated. The launcher stages verified checkpoint
artifacts at an identical path on every node through its remote-execution
transport; no shared filesystem is required. TorchTitan restores model,
optimizer, per-rank RNG, and corpus-consumption state only after validating the
saved topology and parallelism. Temporary staging lives beneath the configured
lookaside scratch root and is removed at launcher-session completion.
