# ADR 0014: Execute external fetchers only through explicit ingest compose

- Status: accepted
- Date: 2026-08-04

## Context

Most contributors prepare local source material themselves and should not need
an acquisition framework. Repeatable public corpora benefit from reviewed,
shared shell fetchers, but asking users to run fetching and ingestion as two
unrelated commands loses the exact compose and script evidence that produced a
contribution. Source-specific fetchers still change independently from WALDO
and do not belong in its Go module.

## Decision

`waldo index ingest <input-or-compose> <destination>` has one conversion and
publication backend. Ordinary files and directories use direct ingestion and
require corpus metadata flags. A regular file strictly identified by
`kind: waldo-ingest-compose`, schema 1, uses composed preparation and rejects
all metadata flags.

An ingest compose provides corpus/source metadata, an optional raw-Parquet text
column, and an ordered list of relative executable paths with literal
arguments. WALDO resolves and hashes the compose and scripts before execution,
runs scripts directly without a shell, and rechecks the bytes afterward. Each
script receives the same private temporary directory as its working directory
and through `WALDO_FETCH_DIR`. It may populate acquired artifacts there and
does not convert to canonical Parquet, publish objects, or mutate the index.

After preparation, WALDO content-probes the directory and enters the identical
immutable ingestion plan, canonical writer, journaled publication, purge, and
contribution path used for direct input. Successful completion purges prepared
source material. Failed preparation or ingestion retains deterministic,
verified preparation state for an unchanged retry.

Dry-run validates corpus metadata, destination resolution, compose syntax,
script executability, script hashes, and available Git evidence. It does not
execute scripts, create staging state, contact sources, publish, or write an
index contribution.

The generated manifest uses the existing `converted_by.collector` field to pin
the compose repository, commit, and path. Dirty or uncommitted compositions
are marked and add the compose SHA-256. Script and per-artifact details remain
execution evidence and are not copied into the Git manifest. Environment and
secret values are never persisted. ADR 0015 fixes this compact boundary
explicitly.

No other command executes index or fetcher code. Index inspection,
verification, BOM construction, export, and model workflows remain data-only.
Fetcher execution is not an OS sandbox: explicitly selecting a compose trusts
its reviewed scripts with the invoking user's permissions. Only regular files
beneath the WALDO-owned output directory are admitted as ingestion inputs.

## Consequences

- Direct local-directory ingestion remains the primary and shortest workflow.
- Reusable fetchers and corpus composes can evolve in `waldo-fetchers` without
  entering the WALDO binary.
- Passing a compose explicitly authorizes its reviewed scripts to execute;
  merely cloning or inspecting an index does not.
- A compose is a complete auditable source-preparation declaration, so CLI
  metadata overrides are forbidden.
- Fetcher output cannot bypass WALDO's canonical conversion, lookaside
  verification, or contribution staging.
- Failed runs can consume substantial temporary space until retried or cleaned;
  a later operational command may make abandoned-workspace cleanup explicit.
