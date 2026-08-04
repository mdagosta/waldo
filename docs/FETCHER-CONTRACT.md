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
- Acquisition start/end time and the underlying content period when known
- The upstream's raw license declaration or evidence location, without mapping
  it to a different license
- Source category: public dataset, commercially licensed, private third party,
  direct web crawl, user data, synthetic data, or other
- Modalities, content types, languages, and known geographic or demographic
  characteristics
- The selection rule when only part of an upstream dataset was acquired
- For direct web acquisition, crawler identity, purpose, behaviour, honoured
  access/opt-out protocols, and domain-level acquired content-byte totals
- For user data, the collecting service/product and interaction type
- For synthetic data, generator model identity and lineage where available
- An atomic, versioned handoff record

## WALDO owns

- License normalization and policy
- Source registry semantics
- Text extraction and document identity
- Language detection and token measurement
- Deduplication, partitioning, and deterministic packing
- Exact post-processing usage measures by source and modality
- Retained content-byte totals by domain for direct web sources
- Structured records of filtering, rights-reservation, and illegal-content
  measures applied during WALDO processing
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

The handoff must retain enough acquisition evidence for the source and corpus
manifests to support the EU GPAI training-content projection described in
`docs/EU-GPAI-DISCLOSURE.md`. Fetchers record what they did and observed; they
do not make a legal-compliance determination.

## Non-goals

- Fetchers are not plugins loaded into the WALDO binary.
- WALDO does not execute arbitrary fetcher code during verification.
- A fetcher name is not provenance; the handoff must record concrete upstream
  artifacts and hashes.
- The future fetcher repository does not define index or shard schemas.
