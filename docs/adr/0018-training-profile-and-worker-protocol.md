# ADR 0018: Resolve training profiles and stream a versioned worker protocol

- Status: accepted
- Date: 2026-08-05

## Context

A short compose must not leave optimizer defaults, record order, packing,
checkpoint cadence, or evaluation cadence implicit inside whichever framework
happens to execute it. Conversely, requiring every low-level value would make
the common training path difficult to use. Framework adapters must also avoid
gaining their own Parquet readers and subtly different data semantics.

## Decision

Portable stage parameters select `causal-pretrain-shuffled` by default. Before a run
BOM is written, WALDO resolves every omitted value and persists the complete
schema-1 profile: AdamW constants, cosine schedule and warmup, global batch and
sequence length, planned token capacity, deterministic bounded shuffle,
continuous EOS packing, and checkpoint/evaluation cadence. Pointer-valued
portable overrides distinguish an omitted default from an explicit zero.
Epoch count is also persisted and defaults to one. Direct training derives its
step count from exact tokenizer targets across the requested epochs; it does
not treat index reference-token estimates as an execution budget.

WALDO's training domain owns canonical Parquet decoding and produces records in
a deterministic order. Shards are sorted and seeded, then records pass through
a bounded-memory shuffle whose SplitMix64 algorithm is part of the contract.
Multiple epochs repeat the canonical inputs with a deterministic epoch-derived
shuffle seed while preserving continuous packing across epoch boundaries.
Both its record-count and retained-byte limits are persisted. A framework
worker never reads Parquet or an index.

An embedded framework worker receives schema-1 NDJSON: one begin frame, record
frames, and one end frame. It returns typed event, completion, or error frames.
Events can carry progress, loss, throughput, ETA, checkpoints, and evaluations.
The terminal observation records actual steps and tokens, final loss,
checkpoint/evaluation results, and content-addressed artifacts. WALDO verifies
all artifact bytes before accepting completion.

## Consequences

- A compact compose remains reproducible because all defaults become durable
  run-BOM facts.
- MLX, PyTorch, TensorFlow, and TorchTitan consume the same record and event
  protocol.
- Corpus size does not imply equivalent memory use; shuffle memory is explicit
  and bounded.
- Changing any default, ordering algorithm, packing rule, or frame shape
  requires a new profile or protocol schema.
- The first MLX worker can be embedded in the Go binary while Python and MLX
  remain explicit runtime dependencies.
