# ADR 0014: Execute external fetchers only through explicit ingest recipe

- Status: superseded by ADR 0064
- Date: 2026-08-04

## Context

Most contributors prepare local source material themselves and should not need
an acquisition framework. Repeatable public corpora benefit from reviewed,
shared shell fetchers, but asking users to run fetching and ingestion as two
unrelated commands loses the exact recipe and script evidence that produced a
contribution. Source-specific fetchers still change independently from WALDO
and do not belong in its Go module.

## Decision

`waldo index ingest <input-or-recipe> <destination>` has one conversion and
publication backend. Ordinary files and directories use direct ingestion and
require corpus metadata flags. A regular file strictly identified by
`kind: waldo-ingest-recipe`, schema 1, uses recipe-driven preparation and rejects
all metadata flags.

An ingest recipe provides corpus/source metadata, an optional raw-Parquet text
column, and an ordered list of `exec` commands with literal arguments. A bare
command name resolves through `PATH`; a command containing a path separator is
an explicit path relative to the recipe file unless absolute. WALDO resolves
and hashes the recipe and executable before execution, runs commands directly
without a shell, and rechecks the bytes afterward. Each command receives the
same private temporary directory as its working directory
and through `WALDO_FETCH_DIR`. It may populate acquired artifacts there and
does not convert to canonical Parquet, publish objects, or mutate the index.

After preparation, WALDO content-probes the directory and enters the identical
immutable ingestion plan, canonical writer, journaled publication, purge, and
contribution path used for direct input. Successful completion purges prepared
source material. Failed preparation or ingestion retains deterministic,
verified preparation state for an unchanged retry.

Dry-run validates corpus metadata, destination resolution, recipe syntax,
command resolution, executable hashes, and available Git evidence. It does not
execute commands, create staging state, contact sources, publish, or write an
index contribution.

The generated manifest uses the existing `converted_by.collector` field to pin
the recipe repository, commit, and path. Dirty or uncommitted recipes
are marked and add the recipe SHA-256. Command and per-artifact details remain
execution evidence and are not copied into the Git manifest. Environment and
secret values are never persisted. ADR 0015 fixes this compact boundary
explicitly.

No other command executes index or fetcher code. Index inspection,
verification, BOM construction, export, and model workflows remain data-only.
Fetcher execution is not an OS sandbox: explicitly selecting a recipe trusts
its reviewed commands with the invoking user's permissions. No shell syntax is
implicitly interpreted; a recipe must explicitly execute a shell when that is
intended. Only regular files
beneath the WALDO-owned output directory are admitted as ingestion inputs.

## Consequences

- Direct local-directory ingestion remains the primary and shortest workflow.
- Reusable fetchers and ingest recipes can evolve in `waldo-fetchers` without
  entering the WALDO binary.
- Passing a recipe explicitly authorizes its reviewed commands to execute;
  merely cloning or inspecting an index does not.
- An ingest recipe is a complete auditable source-preparation declaration, so
  CLI metadata overrides are forbidden.
- Fetcher output cannot bypass WALDO's canonical conversion, lookaside
  verification, or contribution staging.
- Failed runs can consume substantial temporary space until retried or cleaned;
  a later operational command may make abandoned-workspace cleanup explicit.
