# ADR 0048: Make corpus weights and dropout durable training facts

Status: accepted

## Decision

Schema-1 model composes may declare `architecture.dropout` in `0..<1`. It is
part of architecture identity, applies to attention and feed-forward residual
branches during training, and is disabled for evaluation and inference.

`causal-pretrain-weighted` requires positive integer weights for every selected
corpus. WALDO chooses the corpus with the lowest emitted-token-to-weight ratio
while preserving deterministic shuffle, held-out selection, consumption
accounting, and checkpoint compatibility.
