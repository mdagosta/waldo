# ADR 0048: Make corpus weights and dropout durable training facts

## Decision

Schema-1 model composes may declare residual `architecture.dropout`. The value
is part of immutable architecture identity, is disabled for evaluation and
inference, and must be in `0..<1`. Every built-in training backend applies it
to both attention and feed-forward residual branches.

Training profile `causal-pretrain-weighted` requires positive integer
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

- Existing composes remain unchanged under shuffled and balanced profiles with zero
  dropout.
- Weighted composes fail closed when weights are missing or name an unselected
  corpus. Duplicate corpus paths are also rejected before materialization.
- Changing dropout or corpus weights changes the relevant immutable model or
  run identity and prevents incompatible checkpoint resume.
