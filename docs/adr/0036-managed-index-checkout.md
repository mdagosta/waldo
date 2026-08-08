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
`https://github.com/openwaldo/waldo-index.git`, branch `main`. Subsequent
network synchronization is explicit through `waldo index fetch` and `waldo
index pull`; ordinary reads do not silently change the selected revision.

The managed checkout is read-only from the corpus-authoring perspective.
`index init`, `index ingest`, and `index update` reject it. Contributors use an
explicit checkout, created with `waldo index clone <directory>` or otherwise,
and select it with `waldo config set index <directory>` or an absolute path.

Git transport is implemented in `internal/git` with `go-git`. Fetch updates
remote references only. Pull requires a clean `main` worktree and performs
only a fast-forward to `origin/main`; it refuses local commits, divergence,
unexpected remotes, and unexpected branches.

## Consequences

The normal read workflow works without prior configuration, while network
revision changes remain visible and user-requested. The lookaside object cache
and managed Git index remain distinct concepts. Contributor checkouts retain
ordinary Git behavior and are never synchronized implicitly by WALDO.
