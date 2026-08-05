# ADR 0025: Use TorchTitan for single-node distributed training

- Status: accepted
- Date: 2026-08-05

## Context

Linux automatic backend selection deliberately prefers TorchTitan, but an
adapter error on a machine with TorchTitan installed prevents the usable
PyTorch fallback from running. Calling the single-process worker “TorchTitan”
would also misrepresent the execution environment.

WALDO must retain authority over its canonical data order, portable training
profile, run state, observations, and BOMs. TorchTitan's documented extension
points are evolving, so binding WALDO's durable formats to its complete Trainer
configuration would make TorchTitan's internal configuration a second compose.

## Decision

Implement a distinct single-node TorchTitan adapter. It requires Linux, a
compatible TorchTitan/PyTorch installation, and at least one visible CUDA or
ROCm GPU. The resolver executes a real operation on every device, verifies the
TorchTitan device-mesh and PyTorch FSDP2/state-dict APIs, and records one
accelerator entry per worker rank.

The adapter launches the embedded WALDO PyTorch worker with
`torch.distributed.run`, one rank per visible GPU. Rank zero reads WALDO's
schema-1 NDJSON stream and broadcasts each frame. TorchTitan constructs the
data-parallel/FSDP mesh, while PyTorch FSDP2 shards the model. All ranks gather
portable full state dictionaries; rank zero atomically writes checkpoints and
terminal Safetensors and emits the shared observation protocol.

## Consequences

- Linux `auto` can genuinely execute its preferred installed TorchTitan.
- TorchTitan and PyTorch share model tensor names and training semantics but
  retain distinct backend identities and execution facts.
- TorchTitan consumes no index or Parquet APIs and creates no alternate compose
  or BOM format.
- The first scope is one node and all visible GPUs. Multi-node rendezvous,
  scheduler integration, tensor/pipeline parallelism, and distributed
  optimizer-state resume remain later orchestration features.
