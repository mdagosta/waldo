# ADR 0024: Execute Linux training through the shared PyTorch worker contract

Status: accepted
- Date: 2026-08-05

## Context

WALDO's first real adapter used MLX, but the portable compose, deterministic
record stream, run state, and BOM contracts were designed to be
framework-neutral. Linux needs a real local backend without duplicating index,
Parquet, training-profile, or provenance logic in Python.

## Decision

Add a single-process PyTorch adapter for Linux. It discovers and verifies one
usable CPU, NVIDIA CUDA, or AMD ROCm device, then records the exact Python,
PyTorch, host, device, and accelerator facts before execution.

The embedded worker consumes the same schema-1 NDJSON stream as MLX and emits
the same event and observation protocol. It implements the same architecture,
optimizer, schedule, token packing, checkpoint cadence, and internal
Safetensors tensor names. This permits a later WALDO run to initialize from a
verified prior MLX or PyTorch terminal artifact without a conversion step.

TorchTitan remains a separate distributed adapter. Its installation retains
priority in Linux automatic selection; ADR 0025 defines that adapter without
changing this single-process PyTorch contract.

## Consequences

- `model.backend=auto` can train on Linux when TorchTitan is absent and a
  usable PyTorch installation is present.
- CPU execution is supported for correctness and small models; CUDA and ROCm
  use PyTorch's `cuda` device API and record the actual manufacturer.
- PyTorch does not parse Parquet, traverse an index, or own lifecycle files.
- PyTorch generation, distributed execution, optimizer-state resume, and
  held-out evaluation remain independent features.

ADR 0029 subsequently adds optimizer-state resume, and ADR 0030 adds held-out
evaluation without changing the PyTorch adapter boundary.
