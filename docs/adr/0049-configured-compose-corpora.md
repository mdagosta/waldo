# 0049: Configure and filter corpora at their compose selection

## Decision

Schema-1 model composes accept each `corpora` entry as either the existing path
string or an object containing `path`, optional `weight`, and an optional
record-level `filter`. A stage may also declare a global record filter.

Global and corpus-local filters are conjunctive. WALDO applies them to
canonical rows before held-out selection and training order. The effective,
versioned filter policy is pinned in the corpus OpenWALDO BOM used by the run.

The existing `parameters.corpus_weights` map remains readable and keeps its
identity. New composes should place weights beside their corpus paths. Mixing
the two weight representations in one stage is an error. Unknown fields and
empty filters fail closed.

## Rationale

Path, relative exposure, and subset policy describe one corpus selection.
Keeping them together prevents parallel lists and maps from drifting. The
scalar alternative avoids a schema migration and preserves existing compose
documents exactly.

Filtering at canonical-row streaming time supports mixed-license and
mixed-language shards without changing or duplicating indexed corpus objects.
Pinning the policy in the BOM makes the selected training population auditable
without materializing a second filtered corpus.

## Consequences

- License, language, source, and date filters are general canonical-row facts.
- Per-corpus filters can narrow, but never override, a stage-wide policy.
- Manifest totals remain reference totals; filters affect eligible records and
  token consumption, not the immutable index declarations.
- Adding materially different filter semantics requires a new record-filter
  policy schema.
