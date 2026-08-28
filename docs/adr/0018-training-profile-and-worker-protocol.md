# ADR 0018: Resolve training profiles and use one worker protocol

Status: accepted

## Decision

WALDO resolves every omitted portable training value before writing a run BOM.
The resolved schema-1 profile pins optimizer, schedule, batch, sequence length,
budget, record order, packing, shuffle bounds, checkpoint cadence, evaluation,
and corpus weighting.

The training domain owns Parquet decoding, filtering, held-out selection,
tokenization, deterministic ordering, and continuous packing. Framework
workers never read an index or Parquet file.

MLX, PyTorch, and TorchTitan use the schema-1 NDJSON worker protocol. WALDO
sends begin, record or token, and end frames. Workers return typed progress,
checkpoint, evaluation, completion, or error frames. WALDO verifies every
reported artifact before accepting completion.
