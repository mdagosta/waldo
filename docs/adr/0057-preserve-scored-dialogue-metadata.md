# 0057: Preserve scored dialogue metadata and explicit quality gates

## Status

Accepted.

## Decision

The general `dialogue-pair` input profile accepts named `fields.meta` mappings
and stores those values alongside its existing flattened-dialogue metadata.
This retains response ratings and other source annotations in canonical rows
without putting corpus-specific behavior in WALDO.

`input.main_content` accepts a conjunction of exact scalar field matches. A row
is main content only when every declared value matches. A missing field fails
ingestion; a differing value classifies the row as auxiliary. Single-condition
profiles retain their existing behavior.

This permits a reviewed scored-response corpus to retain every rating while a
compose using `main_content: true` selects a conservative SFT subset. It does
not create a preference-training objective: ratings and preference pairs must
remain available for a future objective that consumes them directly.

## Consequences

Dialogue metadata and the complete condition map participate in the existing
input-profile and acquisition identities. Fetchers continue to acquire raw
artifacts only. Complexity or verbosity ratings must not be treated as quality
unless a recipe explicitly and reviewably makes that choice.
