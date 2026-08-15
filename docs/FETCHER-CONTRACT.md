# Fetcher and local acquisition contract

The normative, deliberately short definition of what WALDO accepts from a
local or fetched source directory is [Source directory contract](SOURCE-DIRECTORY.md).
This document defines the broader acquisition and execution responsibilities.

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
- Deterministic output paths
- Verification of declared upstream checksums or immutable revisions
- Upstream revision, acquisition period, and underlying content period when known
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

## Local output shape

A fetcher produces only a directory satisfying the
[source directory contract](SOURCE-DIRECTORY.md). It may contain faithfully
copied raw artifacts or a normalized JSONL/Parquet deposit, but no sidecar
acquisition manifest. Shared acquisition facts belong in the recipe; per-record
upstream evidence belongs in the records. WALDO independently orders, hashes,
and probes every resulting file.

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
- Schema-1 steps share one WALDO-owned temporary working directory. Each
  schema-2 source has its own. The current directory is also exposed as
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
  version: 28ac18deab7ed2ec3580f4f13f0ed141e47957ef
  url: https://huggingface.co/datasets/common-pile/foodista_filtered
  category: public-dataset
  collected_to: 2026-07
  license_evidence:
    declaration: Creative Commons Attribution 3.0 Unported
    url: https://huggingface.co/datasets/common-pile/foodista_filtered/blob/28ac18deab7ed2ec3580f4f13f0ed141e47957ef/README.md
  content:
    types: [food and cooking articles]
    languages: [en]
    selection: The pinned foodista-dolma-0000.json.gz artifact from the filtered Common Pile v0.1 release.
    copyrighted: "yes"
    machine_generated: unknown
  acquisition:
    basis: Public dataset release at the pinned revision.
steps:
  - name: download
    exec: ../../fetchers/http.sh
    args:
      - https://huggingface.co/datasets/common-pile/foodista_filtered/resolve/28ac18deab7ed2ec3580f4f13f0ed141e47957ef/foodista-dolma-0000.json.gz?download=true
      - foodista.jsonl.gz
      - c39b3e7efc54ec03a38fbf2aa0e471d9bcb898879b2df1ff1e6646fdbcc4476f
```

`description`, `source.name`, `text_column`, and each step's `args` are
optional. `title`, `license`, `source.url`, `source.category`, and at least one
step are required. The destination is never embedded; it remains the second
positional argument to `index ingest`.

`text_column` is the legacy single-column Parquet mapping. A recipe may instead
declare one corpus-neutral `input` profile; the two fields are mutually
exclusive. Physical record cardinality is explicit: `.json` is exactly one
top-level object per file, `.jsonl` (plain, gzip, or zstd) is one object per
line, and `.parquet` is one record per row. Top-level JSON arrays are rejected.

Schema 2 is the multi-source form. It retains the top-level corpus title and
description but moves acquisition, license, input mapping, and steps beneath
`sources[]`. Every source has a stable `id`, its own metadata and license, and
one or more acquisition steps. WALDO runs each source in a separate
`WALDO_FETCH_DIR`, associates every resulting record with that source, and
packs records from different sources and licenses into the same size-bounded
Parquet shards.

For raw project trees, one selected UTF-8 file is one canonical text row.
`source_name` and `license` identify the project, while `meta.source_path`
preserves the path relative to that project's acquisition directory. Project
identity is the provenance and train/evaluation grouping boundary; shards do
not create a project boundary.

A complete public Git source uses the same evidence shape. Additional projects
are additional `sources[]` entries:

```yaml
kind: waldo-ingest-recipe
schema: 2
title: Open Source Project Code
description: Project-owned source files from pinned revisions.
sources:
  - id: kubernetes
    license: Apache-2.0
    source:
      name: kubernetes
      version: 0f29094e5b73085e3802ecc1298ecae13866bfe6
      url: https://github.com/kubernetes/kubernetes
      category: public-dataset
      license_evidence:
        declaration: Apache License 2.0
        url: https://github.com/kubernetes/kubernetes/blob/0f29094e5b73085e3802ecc1298ecae13866bfe6/LICENSE
      content:
        types: [source code]
        languages: [Go]
        selection: Project-owned Go files under cmd, pkg, plugin, and staging/src/k8s.io; vendor, testdata, and generated files excluded.
        copyrighted: "yes"
      acquisition:
        basis: Public Git repository at the pinned commit.
    steps:
      - name: fetch
        exec: ../../fetchers/git.sh
        args:
          - https://github.com/kubernetes/kubernetes.git
          - refs/tags/v1.36.3
          - 0f29094e5b73085e3802ecc1298ecae13866bfe6
          - :(glob)cmd/**/*.go
          - :(glob)pkg/**/*.go
          - :(glob)plugin/**/*.go
          - :(glob)staging/src/k8s.io/**/*.go
          - :(exclude,glob)**/vendor/**
          - :(exclude,glob)**/testdata/**
          - :(exclude,glob)**/zz_generated.*
          - :(exclude,glob)**/generated.pb.go
          - :(exclude,glob)**/generated.deepcopy.go
          - :(exclude,glob)**/generated.conversion.go
          - :(exclude,glob)**/generated.defaults.go
```

`collected_from`/`collected_to` describe acquisition, while
`content.from`/`content.to` describe the material itself. Values are ISO 8601
years, months, dates, or RFC3339 timestamps. Omit unknown facts; never invent
them. `content.selection` is required by policy whenever the fetcher arguments
select less than the complete pinned source.

`license_evidence.declaration` is preserved verbatim and
`license_evidence.url` is absolute. Neither replaces the enclosing normalized
`license`. `acquisition.basis` is mandatory for commercially licensed and
private-third-party sources; web crawls require crawler and acquired-domain
facts, user data requires service/interaction facts, and synthetic data
requires generator identity. Unknown categories and incomplete mandatory
category evidence are rejected.

Fetchers do not detect, redact, or annotate email addresses, repetition, or
boilerplate. WALDO applies its general versioned detectors to every retained
canonical row after acquisition, writes the schema-2 `email_addresses`,
`repetitive_content`, and `boilerplate_content` booleans without changing the
text, and records aggregate detector evidence in the generated manifest.

For structured sources that mix primary and auxiliary records, the recipe may
derive the general canonical `main_content` boolean from one exact scalar field
match. Fetchers still emit the unmodified source field. Omitting the declaration
marks every retained row as main content.

```yaml
input:
  type: record-map
  fields:
    text: [title, abstract, "sections[].text"]
    id: identifier
    date: publication.date
    language: metadata.language
    license: metadata.license
```

`record-map` supports dotted paths, terminal `[]` array expansion, and ordered
joining of text fields. `dialogue-pair` uses `fields.text` as the prompt,
optional `fields.context`, and `fields.response`. `ranked-conversation-tree`
uses configurable `tree.root`, `tree.replies`, `tree.text`, `tree.rank`, and
optional role fields, selecting the lowest numeric rank at each level.
Recipes whose source intentionally omits some ranks may declare
`tree.missing_rank: source-order`. Ranked candidates still take precedence;
when every candidate is unranked, WALDO deterministically selects the first
source candidate. Missing ranks otherwise fail closed.

Mapped records fail closed when a required text or response field is empty.
`record-map`, `dialogue-pair`, and `bounded-text` recipes may explicitly set
`on_empty: skip` to reject those physical records instead. WALDO reports
rejected-empty records separately from content duplicates, and the policy is
pinned in the plan.
Record profiles also fail closed on embedded NUL characters unless the recipe
declares `nul: space`. Recipes may set `record_maximum_bytes` between 16 MiB
and 256 MiB when one indivisible mapped record legitimately exceeds the 64 MiB
default; the accepted value remains bounded by the plan memory budget.

Two whole-file primitives are also available. `bounded-text` excludes the
first matching start boundary and the first end boundary after it:

```yaml
input:
  type: bounded-text
  on_empty: skip # optional; reject a file whose matched bounds contain no text
  bounds:
    start_pattern: '(?m)^=== START: .+ ===$'
    end_pattern: '(?m)^=== END: .+ ===$'
```

`xml-record` maps one XML file to one record. Selectors use an absolute XPath
subset: child `/`, descendant `//`, wildcard `*`, terminal attributes, repeated
nodes in document order, and Clark names such as `{urn:example}href` for exact
namespace matching. Prefixed names match their local name. Predicates,
functions, parent axes, and inferred namespace bindings are rejected.

```yaml
input:
  type: xml-record
  fields:
    text: [/doc/title, /doc/abstract, /doc/body]
    source: /doc/header/id
    date: /doc/header/date
    license: '/doc/header/license/@{urn:example}href'
    meta:
      publication: /doc/header/journal
  xml:
    exclude: [//figure, //references]
    source_prefix: 'urn:document:'
```

Profiles contain no corpus names or source-specific defaults. If an upstream
XML vocabulary needs transformations outside this XPath subset—such as
assembling a date from several nodes—the fetcher deposits a general JSONL or
Parquet record-map instead.

`exec: curl` searches `PATH`; `exec: ./fetch.sh`, `exec: ../fetch.sh`, and
`exec: /opt/fetch.sh` are explicit paths. WALDO does not interpret pipelines,
redirections, variables, globbing, or shell built-ins. When shell evaluation is
intentionally required, declare the shell itself (for example `exec: sh`) and
pass its program through `args`.

Recipes accept the controlled source categories `public-dataset`,
`commercially-licensed`, `private-third-party`, `web-crawl`, `user-data`,
`synthetic`, and `other`. Validation fails closed when a category's mandatory
`acquisition` evidence is absent or incomplete. Direct CLI ingestion cannot use
flags to bypass those requirements.

## Non-goals

- A fetch does not upload to the lookaside or create an index contribution.
- A fetch does not invoke a training or model command.
- A fetcher name is not provenance; the handoff must record concrete upstream
  artifacts and hashes.
- A fetcher does not define index or shard schemas.
