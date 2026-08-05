# ADR 0005: Keep fetchers external, bounded, and local-first

- Status: accepted
- Date: 2026-08-04

## Context

Source acquisition changes at the pace of upstream websites and APIs. It also
has different dependencies, operational failures, and contribution patterns
from the deterministic WALDO core.

## Decision

Ship source-specific fetchers from a separate repository as reviewed shell
scripts, not as WALDO commands or Go packages. Their responsibility is
acquisition into an explicit local directory with evidence of what was
downloaded. A fetch stops there; the user may later invoke WALDO ingestion as a
separate action.

Use `lookaside` for WALDO's object-storage domain and command vocabulary; a
fetcher does not become part of the lookaside merely because its acquired bytes
may later be uploaded there.

## Consequences

- Source-specific dependencies and network behavior remain outside the WALDO
  binary and Go module.
- Fetch failures cannot partially mutate an index or start publication.
- A future handoff schema must preserve raw evidence without accepting a
  fetcher's conclusions as authoritative.
- Users can inspect and retain an acquisition before choosing to ingest it.
- WALDO never discovers or executes fetcher scripts; runtime plugins and
  arbitrary downloaded code remain out of scope.
