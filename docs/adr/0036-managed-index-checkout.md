# ADR 0036: Managed default index checkout

Status: accepted

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
`https://github.com/openwaldo/waldo-index.git`, branch `main`. Read commands
that consume an index may fetch and fast-forward the selected checkout when it
is clean and behind its configured tracking branch. Mutation commands never
implicitly synchronize a contributor checkout.

The managed checkout is read-only from the corpus-authoring perspective.
`index init` and `index ingest` reject it. Contributors use an explicit Git
checkout and select it with `waldo config set index <directory>` or a
filesystem destination. Paths beginning with `/`, `./`, `../`, or `~/` always
name the filesystem; an unprefixed relative path is also local when it or its
parent already exists.

Git transport is implemented in `internal/git` with `go-git`. Read commands
may refresh the configured or managed checkout selected by a logical path.
Explicit filesystem paths and mutation commands such as `index ingest` trust
the local checkout and never contact its remote; users explicitly synchronize
it with `waldo index pull`. Pull derives the current
branch's tracking remote and performs only a clean fast-forward; it refuses
dirty worktrees, local commits, divergence, detached HEAD, and missing tracking
configuration.

The managed default is the one deliberate exception for rewritten upstream
history. Its origin URL and branch are fixed and authoring commands cannot use
it. After force-refreshing its remote-tracking reference, WALDO may reset a
clean managed checkout to the canonical upstream commit when the histories are
ahead or diverged. A dirty managed checkout is refused. Configured and explicit
contributor checkouts continue to refuse ahead or diverged states.

Only `waldo index pull` is exposed as a synchronization command. WALDO does not
recapitulate Git with public `index clone`, `index fetch`, or `index status`
commands. Read commands invoke the same safe update policy automatically, and
the managed checkout is created automatically when absent.

## Consequences

The normal read workflow works without prior configuration and normally uses
the latest canonical revision. Every BOM still pins the exact commit used. The
lookaside object cache and managed Git index remain distinct concepts. WALDO
never overwrites contributor changes or resolves divergence in a contributor
checkout; only a clean read-only managed checkout automatically recovers from
an upstream rewrite.
