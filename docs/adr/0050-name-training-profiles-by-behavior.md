# 0050: Name training profiles by behavior

## Decision

The public causal-pretraining profile names are:

- `causal-pretrain-shuffled`
- `causal-pretrain-balanced`
- `causal-pretrain-weighted`

The former `causal-pretrain-v1`, `causal-pretrain-v2`, and
`causal-pretrain-v3` names remain accepted as deprecated input aliases. They
resolve to the behavior-named identity before a new run BOM is created.

Each named profile begins at its own `profile_schema: 1`. WALDO normalizes the
old name/schema pairs when deciding whether a persisted interrupted run is
compatible with checkpoint resume.

## Rationale

The numbered names described different corpus-ordering policies, not successive
model architectures. Users reasonably interpreted the higher number as a newer
or more capable architecture. Behavior names state the actual choice and keep
contract versioning in the resolved schema field where it belongs.

## Consequences

- Reference composes and advisor-generated composes use behavior names.
- Existing compose inputs continue to parse.
- Existing interrupted runs remain resumable under the equivalent new name.
- A future change to one profile's behavior increments that named profile's
  schema without renaming unrelated profiles.
