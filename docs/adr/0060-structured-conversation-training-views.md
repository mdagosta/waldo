# 0060: Preserve conversations and render them in training views

## Status

Accepted. Supersedes ADR 0058's decision to render conversations during
ingestion. Its optional worker loss-mask protocol remains valid.

## Decision

WALDO ingestion maps dialogue sources into a distinct `conversation` logical
record kind. A canonical conversation contains ordered typed messages and
optional tool definitions encoded as canonical JSON in a tokenizer-neutral
Parquet shard. Ingestion may normalize declared source roles, validate
structure, select a configured ranked branch, and apply the versioned privacy
policy. It does not apply a model prompt template.

A model training stage that consumes conversation records declares a
`conversation` transformation. The declaration pins a versioned template and
the roles whose content receives loss. WALDO applies the template immediately
before tokenization, derives loss masks from structured roles, and records the
transformation in the run BOM. The model interaction template must match the
training template.

Schema-1 model composes initially support `user-assistant-v1` and `chatml-v1`.
Tool definitions are deterministically added to the leading system message by
these template versions. A template behavior change requires a new identifier.

Existing flattened text shards remain readable as `pretrain` records. They are
not accepted by `assistant-response-modeling`, because reconstructing message
boundaries from text prefixes is ambiguous.

## Consequences

- One source corpus can be reused with different compatible model templates
  without reingestion.
- Canonical identity covers roles, message ordering, content, and tools.
- Ingestion recipes control only source interpretation; model composes control
  model-visible rendering and supervision.
- Previously flattened SFT corpora must be explicitly rebuilt to gain
  structured semantics.
- Compiled training-view identity includes the conversation transformation,
  tokenizer, objective, packing, and split policies through the immutable run
  plan and BOM.
