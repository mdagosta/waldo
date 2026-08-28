# ADR 0050: Name training profiles by behavior

Status: accepted

## Decision

The public causal-pretraining profiles are:

- `causal-pretrain-shuffled`
- `causal-pretrain-balanced`
- `causal-pretrain-weighted`

The former `causal-pretrain-v1`, `causal-pretrain-v2`, and
`causal-pretrain-v3` names remain accepted as deprecated aliases. WALDO
normalizes them to the behavior name before creating new run identity and when
checking resume compatibility.
