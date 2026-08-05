# ADR 0021: Keep inference ephemeral and artifact-bound

- Status: accepted
- Date: 2026-08-05

## Context

A model may contain simulated history, failed runs, and multiple generations
of real weights. Machine training preferences do not determine which framework
can read an existing artifact. Interactive output can also contain arbitrary
byte tokens, and recording every local prompt as lifecycle state would
conflate immutable training provenance with ephemeral use.

## Decision

Inference selects only the model BOM's `current_run_id`, requires a complete
non-simulated run, and verifies weight, configuration, and tokenizer bytes
against their BOM sizes and SHA-256 hashes before launching a worker. The
recorded artifact backend—not `model.backend`—selects the inference adapter.
Missing runtimes fail closed with installation diagnostics.

Training and inference use one embedded MLX model definition. The inference
worker remains alive for the command lifetime, loads weights once, and uses an
incremental key/value cache. Its schema-1 line protocol streams base64 byte
tokens and a typed completion record. WALDO escapes terminal controls and
invalid UTF-8 in human output. One-shot JSON is emitted only after completion.

Interactive history is bounded by the architecture context and is not written
to the model directory. A model without an explicit chat template is described
as raw causal continuation; WALDO does not invent an instruction format.

## Consequences

- Simulated artifacts remain auditable without becoming inference input.
- Relocated exports and managed models use the same verified inference path.
- Changing a preferred training backend cannot reinterpret existing weights.
- Prompts and responses remain private process state unless a future explicit
  export or evaluation command records them.
- Future framework adapters implement the same session contract without
  changing model lifecycle persistence.
