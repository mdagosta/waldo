# 0064: Make manifest-backed raw directories the ingestion boundary

Status: accepted
- Date: 2026-08-27

## Context

Acquisition and ingestion need a single, explicit trust boundary. The
`waldo-corpus-directory` manifest provides that boundary: acquisition stops
after producing a verified recursive raw tree, and WALDO begins with
declarative metadata and input semantics.

## Decision

The canonical ingestion input is a manifest-backed raw directory. Its root
`manifest.json` owns corpus and source facts, input format and mapping, artifact
evidence, and deterministic raw-tree evidence. The contract applies to every
producer, not only OpenWALDO fetchers.

WALDO recursively inventories and hashes all regular files inside declared
source boundaries. Raw-file inventory and logical-record cardinality are
separate. The manifest selects a built-in adapter, and that adapter defines how
the verified tree becomes records. Tree-aware adapters perform deterministic
root discovery and dependency resolution inside the boundary.

Acquisition never runs inside WALDO. Fetchers and other tools write the raw
directory and manifest, then stop. A manifest cannot name an executable,
external converter, or runtime adapter.

Direct file ingestion remains a local convenience. Reviewable and
reproducible corpus contributions use the manifest-backed directory.

## Consequences

- Ingestion has one documented trust boundary independent of acquisition.
- Corpus metadata and input interpretation travel with the verified raw tree.
- New formats require reviewed built-in WALDO adapters.
- Fetcher output is inspectable and reusable before an index destination is
  chosen.
