# ADR 0006: Use an index-centered CLI

Status: accepted

## Decision

Expose corpus workflows beneath `waldo index`. The public commands are `init`,
`pull`, `list`, `show`, `summary`, `bom`, `verify`, `audit`, `ingest`, and
`export`.

Corpus creation and replacement both use `waldo index ingest`. Passing
`--update` performs an authoritative rebuild of an existing corpus. Corpus
replacement is a flag on ingestion, not a separate command.

Unadorned logical paths resolve beneath the configured contributor checkout,
or the managed read-only checkout when `index` is unset. Explicit filesystem
paths use the named local checkout. Authoring commands never mutate the
managed checkout.

The corpus remains a separate internal domain. CLI organization follows the
user workflow rather than the package graph.
