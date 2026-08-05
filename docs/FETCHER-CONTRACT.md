# Fetcher and local acquisition contract

Fetchers are source-specific shell scripts maintained in a separate repository.
They are not WALDO commands, Go packages, runtime plugins, or part of this
repository's release. They download upstream material into a user-selected
local directory and record what they observed. A fetch ends there: it does not
invoke WALDO, publish to a lookaside, mutate an index, or start model training.

The separate fetcher project will finalize its script conventions and local
acquisition schema later. Until then, this document records only the ownership
boundary. WALDO consumes ordinary local files and directories through
`waldo index ingest`; it does not currently interpret a fetcher-specific
acquisition record. Conversely, `waldo index export` materializes an already
indexed corpus and its OpenWALDO BOM into a local directory.

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
- An atomic, versioned acquisition record beside the local bytes

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

## Local output shapes

The contract may describe:

1. Raw acquired artifacts plus an acquisition record
2. A normalized, sorted deposit optimized for large columnar upstreams

A deposit carries documents and faithfully copied upstream evidence. It does
not carry WALDO's conclusions about canonical licenses, document hashes,
languages, or token counts.

The local acquisition must retain enough evidence for the source and corpus
manifests to support the EU GPAI training-content projection described in
`docs/EU-GPAI-DISCLOSURE.md`. Fetchers record what they did and observed; they
do not make a legal-compliance determination.

## Repository and execution boundary

- Fetchers ship as reviewed shell scripts from their own repository.
- WALDO does not discover, download, install, or execute fetcher scripts.
- Network, pagination, authentication, retries, and source-specific concerns
  do not enter this repository's CLI or Go packages.
- The user explicitly runs ingestion after reviewing a local acquisition.
- Any future machine-readable handoff must be reviewed as a versioned contract;
  it is not implied by this document.

## Non-goals

- A fetch does not upload to the lookaside or create an index contribution.
- A fetch does not invoke a training or model command.
- A fetcher name is not provenance; the handoff must record concrete upstream
  artifacts and hashes.
- A fetcher does not define index or shard schemas.
