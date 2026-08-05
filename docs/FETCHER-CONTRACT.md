# Fetcher and local acquisition contract

Fetchers are source-specific shell scripts maintained in a separate repository.
They are not WALDO commands, Go packages, runtime plugins, or part of this
repository's release. They download upstream material into a supplied local
directory and stop. They never publish to a lookaside, mutate an index, convert
canonical Parquet, or start model training.

WALDO normally consumes ordinary local files and directories through
`waldo index ingest`. It can also consume a strict ingest recipe from the
fetcher repository. That recipe names reviewed scripts and corpus metadata;
WALDO executes the scripts into private staging and then treats their output as
ordinary local input. Conversely, `waldo index export` materializes an already
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
- WALDO does not discover, download, or install fetcher scripts.
- The user explicitly authorizes execution by passing one recipe file as the
  first positional argument to `waldo index ingest`.
- Each step uses `exec`. A bare name is resolved through the invoking process's
  `PATH`; a value containing `/` or `\\` is an explicit path, resolved relative
  to the recipe file unless absolute.
- WALDO resolves each command to a regular executable, pins and hashes that
  resolved file, and runs it in declaration order without an intervening shell.
- All steps share a WALDO-owned temporary working directory, also exposed as
  `WALDO_FETCH_DIR`. `WALDO_INGEST_RECIPE` identifies the recipe being run.
- Network, pagination, authentication, retries, and source-specific behavior
  remain inside the scripts. WALDO inherits the invoking environment but never
  records environment or secret values.
- Resolved executables and recipes are hashed before execution and rechecked
  afterward.
- WALDO independently probes and hashes every produced artifact before it can
  enter an immutable ingestion plan.

Fetcher execution is explicit trust, not an operating-system sandbox. A script
runs with the invoking user's permissions and inherited environment. The
reviewed contract requires it to write acquired artifacts only beneath
`WALDO_FETCH_DIR`; WALDO ensures that only regular non-symlink files found
there become ingestion inputs.

## Ingest recipe schema 1

Ingest recipe files are strict YAML or JSON. Unknown fields, multiple YAML documents,
duplicate step names, missing commands, and non-executable files are rejected.

```yaml
kind: waldo-ingest-recipe
schema: 1
title: Foodista
description: Community-contributed cooking and food articles.
license: CC-BY-3.0
source:
  name: common-pile/foodista_filtered
  url: https://huggingface.co/datasets/common-pile/foodista_filtered
  category: public-dataset
text_column: text
steps:
  - name: fetch
    exec: ../../fetchers/common-pile.sh
    args:
      - foodista_filtered
```

`description`, `source.name`, `text_column`, and each step's `args` are
optional. `title`, `license`, `source.url`, `source.category`, and at least one
step are required. The destination is never embedded; it remains the second
positional argument to `index ingest`.

`exec: curl` searches `PATH`; `exec: ./fetch.sh`, `exec: ../fetch.sh`, and
`exec: /opt/fetch.sh` are explicit paths. WALDO does not interpret pipelines,
redirections, variables, globbing, or shell built-ins. When shell evaluation is
intentionally required, declare the shell itself (for example `exec: sh`) and
pass its program through `args`.

The initial schema supports the same source categories as executable direct
ingestion: `public-dataset`, `private-third-party`, and `other`. Categories
whose EU GPAI evidence is structurally required will be enabled only when the
recipe can express that evidence without flags or inference.

## Non-goals

- A fetch does not upload to the lookaside or create an index contribution.
- A fetch does not invoke a training or model command.
- A fetcher name is not provenance; the handoff must record concrete upstream
  artifacts and hashes.
- A fetcher does not define index or shard schemas.
