# ADR 0036: Managed default index checkout

## Status

Accepted.

## Context

Requiring every consumer to locate and maintain a separate `waldo-index`
checkout makes the default read workflow unnecessarily difficult. At the same
time, a checkout maintained automatically by WALDO must not become an implicit
authoring workspace: synchronization could overwrite contributor state, and
ingestion should always produce changes against a checkout the contributor
chose explicitly.

WALDO must not depend on an installed `git` executable for this workflow.

## Decision

When `config.index` is unset, WALDO uses `~/.waldo/index` as its managed index
checkout. The first read workflow that needs it clones
`https://github.com/openwaldo/waldo-index.git`, branch `main`. Commands that
consume an index automatically fetch and fast-forward the selected checkout
when it is clean and behind its configured tracking branch. This applies to a
configured or explicitly supplied checkout as well as the managed default;
synchronization policy is based on Git state, not filesystem location.

The managed checkout is read-only from the corpus-authoring perspective.
`index init`, `index ingest`, and `index update` reject it. Contributors use an
explicit Git checkout and select it with `waldo config set index <directory>`
or an absolute path.

Git transport is implemented in `internal/git` with `go-git`. Fetch updates
remote references only. Pull derives the current branch's tracking remote and
performs only a clean fast-forward; it refuses dirty worktrees, local commits,
divergence, detached HEAD, and missing tracking configuration.

Only `waldo index pull` is exposed as a synchronization command. WALDO does not
recapitulate Git with public `index clone`, `index fetch`, or `index status`
commands. Read commands invoke the same safe update policy automatically, and
the managed checkout is created automatically when absent.

## Consequences

The normal read workflow works without prior configuration and normally uses
the latest clean fast-forward revision. Every BOM still pins the exact commit
used. The lookaside object cache and managed Git index remain distinct
concepts. WALDO never overwrites contributor changes or resolves divergence.
