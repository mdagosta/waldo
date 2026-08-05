# Fetcher and local acquisition contract

Fetchers are source-specific acquisition adapters in the single WALDO binary.
They download upstream material into a user-selected local directory and
record what they observed. A fetch ends there: it does not ingest, publish to a
lookaside, mutate an index, or start model training.

The first implemented adapter acquires one explicitly named Hugging Face
dataset file. The one-file boundary is deliberate: the first command cannot
accidentally begin downloading an entire large dataset. Multi-file selection
can be added after its selection UX is reviewed.

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

## Schema-1 local output

A deposit contains raw artifacts beneath `data/` and exactly one
`ACQUISITION.json`. The record is written last and atomically. It pins the
adapter revision, acquisition times, upstream source and resolved revision,
raw license evidence, and each artifact's path, URL, SHA-256, size, format, and
media type.

The first adapter accepts only Hugging Face LFS files for which upstream
exposes both SHA-256 and size. It streams into `.part`, verifies the completed
bytes, renames atomically, and only then writes the record. A completed rerun
hashes the local artifact and becomes a no-op. An interrupted partial is
discarded rather than byte-resumed without independent proof.

A deposit carries faithfully copied upstream evidence. It does not carry
WALDO's conclusions about canonical licenses, languages, or token counts.

The local acquisition must retain enough evidence for the source and corpus
manifests to support the EU GPAI training-content projection described in
`docs/EU-GPAI-DISCLOSURE.md`. Fetchers record what they did and observed; they
do not make a legal-compliance determination.

## Execution boundary

- Fetchers ship as reviewed adapters compiled into the WALDO binary.
- Fetchers are not runtime plugins and WALDO does not execute downloaded or
  arbitrary fetcher code.
- Network, pagination, and source-specific concerns remain behind a narrow
  acquisition interface; they do not enter index, corpus, or model packages.
- The user explicitly runs ingestion after reviewing a local acquisition.

## Non-goals

- A fetch does not upload to the lookaside or create an index contribution.
- A fetch does not invoke a training or model command.
- A fetcher name is not provenance; the handoff must record concrete upstream
  artifacts and hashes.
- A fetcher does not define index or shard schemas.

## First command

```bash
waldo fetch huggingface org/dataset data/train.parquet /tmp/dataset \
  --revision main
```

`--revision` is the only adapter option. It selects a branch, tag, or commit;
when omitted it defaults to `main`. Either way, the record pins the resolved
commit. `HF_TOKEN` is read from the environment for gated datasets and is
never persisted.

After review, ingestion consumes the deposit directly:

```bash
waldo index ingest /tmp/dataset /path/to/index/core/example \
  --license CC-BY-4.0
```

The license remains an explicit curator assertion. The verified acquisition
supplies the source facts and corpus proposal, which normal ingestion flags may
override.
