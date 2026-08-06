# ADR 0019: Fail closed into a real MLX backend on Apple Silicon

- Status: accepted
- Date: 2026-08-05

## Context

The fake backend proved lifecycle persistence but was still the production
default. A normal `model train` command could therefore report success while
producing no trained weights. The first real backend must preserve the portable
compose and worker boundaries, produce verifiable artifacts, and avoid making
Python environment selection implicit or unauditable.

## Decision

Automatic training on Apple Silicon resolves only to MLX. WALDO searches viable
Python executables, imports the installed MLX distribution, executes a small
operation on Metal, and records the selected executable, Python and MLX
versions, accelerator name, and memory in the run OpenWALDO BOM. If every probe
fails, resolution fails before a run is created. The fake adapter is available
only through the explicit machine-local `model.backend=fake` setting.

The MLX Python worker is embedded into the WALDO binary and communicates solely
through the schema-1 worker protocol. Its first supported architecture is the
portable decoder transformer with the immutable built-in byte tokenizer and
259-token vocabulary. It implements grouped-query rotary attention, RMS
normalization, a gated feed-forward network, continuous-EOS packing, AdamW,
warmup plus cosine decay, actual loss and gradient updates, checkpoint and
terminal Safetensors, and typed progress and training-loss observations.
Direct additional training verifies and initializes from the latest real
terminal weights and pins that source run and artifact hash in the new run BOM.
It does not treat simulated artifacts as weights.

Unsupported tokenizers and architectures fail during resolution. This slice
does not describe training-loss samples as held-out validation, does not claim
checkpoint resume, and does not implement chat generation.

ADR 0029 later adds complete resume bundles, and ADR 0030 later replaces the
training-loss observation with pinned held-out evaluation. ADR 0021 adds chat
generation. Those additions preserve this adapter boundary.

## Consequences

- A successful default training run on supported Apple hardware means real
  weights were optimized; simulation cannot be mistaken for success.
- MLX remains a peer adapter behind the same contract future PyTorch and
  TensorFlow adapters will implement.
- Python and MLX remain explicit runtime dependencies, while WALDO owns and
  versions the worker implementation.
- The 10m, 35m, and 90m built-in byte-tokenizer presets are executable now.
  Larger tiktoken presets remain unavailable until their pinned tokenizer path
  is implemented.
