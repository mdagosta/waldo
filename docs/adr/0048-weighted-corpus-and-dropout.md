# ADR 0048: Make corpus weights and dropout durable training facts

## Decision

Schema-1 model composes may declare residual `architecture.dropout`. The value
is part of immutable architecture identity, is disabled for evaluation and
inference, and must be in `0..<1`.

Training profile `causal-pretrain-v3` requires positive integer
`corpus_weights` keyed by every resolved corpus path. WALDO deterministically
chooses the corpus with the lowest emitted-token-to-weight ratio. It retains
bounded per-corpus shuffling, stratified held-out selection, exact consumption
accounting, and checkpoint resume.

## Rationale

Equal exposure is not suitable for every multi-corpus model. Integer weights
make the intended mixture explicit and auditable without floating-point order
ambiguity. Dropout is an architectural training behavior and therefore cannot
be an unrecorded backend option.

## Consequences

- Existing composes remain unchanged under profiles v1 and v2 with zero
  dropout.
- Weighted composes fail closed when weights are missing or name an unselected
  corpus.
- Changing dropout or corpus weights changes the relevant immutable model or
  run identity and prevents incompatible checkpoint resume.
