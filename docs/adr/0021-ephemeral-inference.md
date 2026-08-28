# ADR 0021: Keep inference ephemeral and artifact-bound

Status: accepted

## Decision

Inference selects only the model BOM's current complete, non-simulated weights.
It verifies configuration, tokenizer, and weight sizes and hashes before
starting the adapter recorded by the artifact.

The schema-1 inference protocol sends prompt bytes for the built-in byte
tokenizer and token IDs for supported subword tokenizers. Workers return token
bytes or IDs and a typed completion record. The command escapes unsafe terminal
output and emits structured output only after completion.

Interactive history is bounded by the architecture context and is not written
to model lifecycle state. Models without an interaction template remain raw
causal continuation models.
