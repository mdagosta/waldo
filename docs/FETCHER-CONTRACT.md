# External fetcher handoff

Fetchers are source-specific acquisition scripts maintained in a separate,
future repository. They download upstream material and record what they
observed. WALDO interprets, normalizes, packs, and indexes that material.

This document reserves the boundary; its exact schema will be finalized with
the first corpus-ingestion slice.

## A fetcher owns

- Source-specific URLs, APIs, pagination, retries, and resumability
- Downloading upstream artifacts without silently changing their contents
- Stable artifact ordering
- SHA-256 and byte size of every acquired artifact
- Acquisition time and upstream revision where available
- The upstream's raw license declaration or evidence location, without mapping
  it to a different license
- An atomic, versioned handoff record

## WALDO owns

- License normalization and policy
- Source registry semantics
- Text extraction and document identity
- Language detection and token measurement
- Deduplication, partitioning, and deterministic packing
- Lookaside upload
- Manifest generation and index mutation
- Provenance and compatibility validation

## Two accepted shapes

The future contract may describe:

1. Raw acquired artifacts plus an acquisition record
2. A normalized, sorted deposit optimized for large columnar upstreams

A deposit carries documents and faithfully copied upstream evidence. It does
not carry WALDO's conclusions about canonical licenses, document hashes,
languages, or token counts.

## Non-goals

- Fetchers are not plugins loaded into the WALDO binary.
- WALDO does not execute arbitrary fetcher code during verification.
- A fetcher name is not provenance; the handoff must record concrete upstream
  artifacts and hashes.
- The future fetcher repository does not define index or shard schemas.
