# 0054: Retain unassessed rows when applying assessment filters

Status: accepted

## Decision

Content-assessment exclusions remain part of the immutable compose and corpus
BOM policy. WALDO applies them to schema-2 rows, where the assessed booleans
exist. For schema-1 rows, WALDO retains the row and ignores only assessment
conditions whose facts are unavailable. Every other declared filter condition
continues to apply.

Before materialization, WALDO warns once per affected training stage. The
warning identifies the number of schema-1 shards and the assessment fields
that cannot be applied. This makes partial enforcement visible without making
the published mixed-version index unusable.

## Consequences

- A compose preserves its intended policy as corpora are rebuilt to schema 2.
- Schema-1 rows are never misrepresented as having clean assessments.
- Training against older shards is permissive for unavailable assessment facts
  and explicitly disclosed to the operator.
- Rebuilding affected corpora remains the way to obtain complete enforcement.
