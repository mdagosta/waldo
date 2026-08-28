# 0051: Resolve direct compose model origins through the shared importer

Status: accepted

## Decision

Schema-1 model composes may declare exactly one of `base.model` or
`base.source`. A source is resolved and verified by the same importer used by
`waldo model pull`; provider-specific downloading and normalization must not be
implemented in compose orchestration.

The first supported source form is
`huggingface://organization/model@<immutable-commit>`. Moving references are
rejected. A compose may omit `architecture` when it declares `base.source`, in
which case WALDO inherits the verified source architecture. A declared
architecture remains an exact assertion.

WALDO caches verified direct origins in a hidden, content-addressed namespace
beneath the managed model root. Visible target models retain the origin BOM and
use hard links when possible, with a copy fallback. Hidden origins do not appear
in `model list`.

## Consequences

`model pull` remains useful when a user wants a named, inspectable base model,
but it is not a prerequisite for compose training. Both entry points share one
compatibility matrix and fail closed for unsupported architectures,
tokenizers, tensor layouts, or source providers.
