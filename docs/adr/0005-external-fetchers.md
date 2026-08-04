# ADR 0005: Keep fetchers external

- Status: accepted
- Date: 2026-08-04

## Context

Source acquisition changes at the pace of upstream websites and APIs. It also
has different dependencies, operational failures, and contribution patterns
from the deterministic WALDO core.

## Decision

Maintain fetcher scripts in a separate future repository. WALDO owns a
versioned handoff contract for raw acquisitions and normalized deposits but
does not embed or execute source-specific fetchers.

Use `lookaside` for WALDO's object-storage domain and command vocabulary; a
fetcher does not become part of the lookaside merely because its acquired bytes
may later be uploaded there.

## Consequences

- The core stays focused on interpretation, deterministic transformation,
  indexing, and verification.
- Fetchers can release and recover from upstream breakage independently.
- The handoff schema must preserve raw evidence without accepting a fetcher's
  conclusions as authoritative.
- End-to-end acquisition testing will eventually span two repositories.

