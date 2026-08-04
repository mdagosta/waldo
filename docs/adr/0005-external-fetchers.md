# ADR 0005: Keep fetchers bounded and local-first

- Status: accepted
- Date: 2026-08-04

## Context

Source acquisition changes at the pace of upstream websites and APIs. It also
has different dependencies, operational failures, and contribution patterns
from the deterministic WALDO core.

## Decision

Ship source-specific fetchers as bounded adapters in the single WALDO binary.
Their first responsibility is acquisition into an explicit local directory
with an atomic evidence record. A fetch stops there; ingestion, lookaside
publication, index mutation, and model training remain separate commands.

Use `lookaside` for WALDO's object-storage domain and command vocabulary; a
fetcher does not become part of the lookaside merely because its acquired bytes
may later be uploaded there.

## Consequences

- Source-specific dependencies and network behavior remain behind a narrow
  acquisition interface.
- Fetch failures cannot partially mutate an index or start publication.
- The handoff schema must preserve raw evidence without accepting a fetcher's
  conclusions as authoritative.
- Users can inspect and retain an acquisition before choosing to ingest it.
- Runtime plugins and arbitrary downloaded code remain out of scope.
