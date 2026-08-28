# ADR 0049: Configure and filter corpora at their compose selection

Status: accepted

## Decision

Each schema-1 compose corpus entry may be a path string or an object containing
`path`, optional `weight`, and an optional record filter. A stage may also
declare a global filter. Global and corpus-local filters are conjunctive.

WALDO applies filters before held-out selection and training order and pins the
effective policy in the run's corpus BOM.

Legacy `parameters.corpus_weights` remains readable. It cannot be combined
with inline weights. Unknown fields, duplicate paths, incomplete weights, and
empty filters fail closed.
