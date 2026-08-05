# Command-line experience

This document is the user-facing command contract. Commands may be introduced
in phases, but implemented behavior should follow this vocabulary unless an ADR
changes it.

## Design rules

1. Organize commands by the object a user intends to manage, not by an
   implementation phase.
2. Keep the common path short. Put destructive and operational commands behind
   explicit nouns and verbs.
3. Resolve index paths consistently from positional paths, discovering the
   enclosing checkout by walking upward like Git. Do not maintain an invisible
   index clone.
4. Show a human-readable result by default and offer `--json` wherever output
   is useful to automation.
5. Perform a preflight before moving large data or starting paid compute.
6. Never silently continue, replace, delete, or mutate model history.
7. Use `--dry-run` for consequential corpus and lookaside operations when it
   can provide a faithful preview.

## Command tree

```text
waldo
├── index
│   ├── init
│   ├── list
│   ├── show
│   ├── summary
│   ├── verify
│   ├── add
│   ├── update
│   ├── export
│   └── remove
├── lookaside
│   ├── login
│   ├── logout
│   ├── list
│   ├── status
│   ├── verify
│   ├── mirror
│   └── rm
├── model
│   ├── create
│   ├── build
│   ├── train
│   ├── inspect
│   ├── test
│   ├── chat
│   ├── fork
│   ├── export
│   └── remove
├── bom
│   ├── show
│   ├── verify
│   └── export
└── config
    ├── show
    ├── get
    └── set
```

`index update`, advanced lookaside maintenance, and several model commands are
post-MVP capabilities. Their placement is decided now so the initial commands
do not grow into the wrong namespace.

The CLI intentionally groups corpus workflows under `index`. Users encounter a
corpus as an indexed path, and should not have to choose between two command
groups that appear to own the same data. The internal corpus domain remains a
separate boundary for selection, OpenWALDO BOMs, ingestion, and export; package
architecture does not have to appear as product vocabulary.

Ingest recipes are inputs to `waldo index ingest`, not a separate command
group. Model composition terminology remains reserved for the model lifecycle.

Source-specific acquisition implementations remain outside this binary. A
separate repository contains reviewed shell scripts and strict ingest recipe
files. Normal ingestion consumes a user-prepared local directory. When the user
explicitly supplies a recipe to `index ingest`, WALDO runs only the scripts
named by that file; they populate private temporary input space and stop.

## Primary journeys

### Initialize an empty index

```bash
waldo index init /path/to/new-waldo-index
```

The destination must be new or empty. WALDO writes only the root schema-2
`index.json`; it does not initialize Git, configure a lookaside, or create a
corpus. Those remain separate, visible actions.

### Inspect and verify an index

```bash
waldo index list
waldo index list science
waldo index summary
waldo index show science/plos
waldo index verify
waldo index verify /path/to/waldo-index/science/plos
waldo index verify --offline
waldo index verify science --objects
```

All forms recurse beneath the selected path. The default validates local
metadata and checks that every canonical object URL is reachable with the
declared size, using HTTP/S3 headers or local file metadata without downloading
object bodies. `--offline` performs only local structural validation.
`--objects` is the expensive proof: it downloads each object, verifies its
SHA-256, and purges successful scratch material afterward. Availability checks
always probe the manifest URL itself; a mirror must not hide a missing
canonical object.

`index list` is recursive and returns one row per corpus beneath the selected
path, including logical path, title, shard/document/token/byte totals, and a
license summary. `index show` is the detailed view for one corpus or directory.

`--offline` structural verification is local and fast. The default uses
header-only network requests to establish canonical availability and size;
`--objects` verifies lookaside availability and hashes every byte. For a
rollup-backed corpus it also expands the content-addressed submanifest tree,
checks every level's totals, and then verifies the resolved leaf objects.

### Add a corpus

```bash
waldo index ingest ~/data/books core/books \
  --title "Example Books" \
  --license CC-BY-4.0 \
  --source https://example.org/books \
  --source-category public-dataset \
  --dry-run
```

The first implemented slice hashes and senses every input, reads Parquet
footers without loading their payload, resolves unambiguous column mappings,
and emits an immutable plan with `--dry-run`. Extensions are hints only; magic
bytes and container structure take precedence.

For the basic text and Markdown adapter, one file is one logical document and
its exact NUL-free UTF-8 bytes are preserved. The accepted plan pins a 16 MiB
adapter batch target, a 64 MiB maximum indivisible record, and the exact
Parquet writer recipe. Execution re-hashes each file before emitting it. A
larger file requires an explicit, named splitter recipe; it is never silently
split, truncated, repaired, or rendered from Markdown.

JSONL ingestion projects one top-level string field named `text` per nonblank
line. Plain `.jsonl`, gzip, and zstd inputs stream through bounded decoding;
they are not expanded into an intermediate file. Unknown JSON members are not
copied into the canonical row. Missing, null, empty, oversized, non-UTF-8, and
NUL-containing text values fail the ingest.

The command preflights source metadata, projected shard count, output size,
destination, and lookaside configuration before conversion. On success
it should explain the exact Git review and DCO commit steps without creating a
pull request itself.

Configure the writable lookaside once, then store the bucket's access and
secret keys in the operating system credential vault. The secret prompt does
not echo and neither key is written to WALDO configuration:

```bash
waldo config set lookaside s3://openwaldo/lookaside/v1
waldo config set lookaside.region us-east-2
waldo config set lookaside.workers 4
waldo lookaside login
```

`lookaside login` is bucket-scoped, so changing prefixes in the same bucket
does not require another login. `waldo lookaside logout` removes the saved
keys. Before saving a new login, WALDO writes, inspects, reads, and deletes a
tiny unique probe object beneath the configured prefix. A login therefore
proves the credentials have `PutObject`, `GetObject` (including metadata), and
`DeleteObject` access without leaving test data behind. A failed validation
does not replace previously saved credentials. Environment and workload-role
credentials remain supported for headless execution when no WALDO keychain
login exists.

For a complete local integration test, configure a filesystem-backed writable
lookaside instead:

```bash
waldo config set lookaside file:///tmp/waldo-published
waldo config set lookaside.workers 2
```

This runs the normal publish, verify, journal, purge, and overlay path and uses
`file://` shard URLs. Such an overlay is deliberately test-only and must not be
committed to a shared index. The directory contains only content-addressed
Parquet objects; WALDO keeps journals and contribution files in staging.

Then execute ingestion without transport or scratch flags:

```bash
waldo index ingest ~/data/books core/books/example \
  --title "Example Books" \
  --description "Books from the example public collection." \
  --license CC-BY-4.0 \
  --source https://example.org/books \
  --source-category public-dataset
```

When the current directory is outside the checkout, make the destination an
absolute or filesystem-relative positional path instead:

```bash
waldo index ingest ~/data/books /path/to/waldo-index/core/books/example \
  --title "Example Books" --license CC-BY-4.0 \
  --source https://example.org/books --source-category public-dataset
```

WALDO discovers the checkout, then records only `core/books/example` in the
ingestion plan and generated index metadata.

WALDO assembles and verifies shards while a bounded worker pool publishes
earlier shards to S3. It verifies the remote size and SHA-256, journals that
fact, and then purges the staged copy.
The contribution overlay is created only after every referenced remote object
is verified. Machine-specific staging is configured separately when the OS
default is unsuitable.
Git editing, committing, pushing, and opening a pull request remain explicit
user actions. Human output lists every overlay file and prints copy, full-index
verification, staged-diff checking, and `git commit -s` commands. WALDO does
not run those commands on the user's behalf.

The command deliberately converges on one conversion and publication path. Its
direct-input arguments are:

| Argument | When to use it |
| --- | --- |
| `<input>` | Always. A file or recursively scanned directory. |
| `<destination>` | Always. The new corpus path inside the index. |
| `--title` | Always. Human-readable corpus title. |
| `--license` | Always. License applying to this contribution. |
| `--source` | Always. Acquisition/source URL recorded in provenance. |
| `--source-category` | Always. GPAI-compatible source category. |
| `--description` | Optional. WALDO generates a plain default otherwise. |
| `--source-name` | Optional. Defaults from the destination name. |
| `--text-column` | Only when raw Parquet has no uniquely inferable text column. |
| `--dry-run` | Probe and print the immutable plan without writing or publishing. |
| `--json` | Global option for structured output and progress events. |

The same command accepts a strict YAML or JSON recipe identified by
`kind: waldo-ingest-recipe` and `schema: 1`:

```bash
waldo index ingest ../waldo-fetchers/recipes/common-pile/foodista.yaml \
  /path/to/waldo-index/core/common-pile/foodista --dry-run
```

An ingest recipe supplies title, description, license, source facts, optional Parquet
text-column mapping, and an ordered list of `exec` commands and literal
arguments. Bare command names resolve through `PATH`; commands containing a
path separator resolve explicitly from the recipe file unless absolute. WALDO
hashes the recipe and every resolved executable before execution, runs each
command directly without an intervening shell, and rechecks those hashes
afterward. Commands share a private temporary directory as their working directory and receive its
absolute path in `WALDO_FETCH_DIR`; they must write only acquired artifacts
there. `WALDO_INGEST_RECIPE` names the absolute recipe path.

Recipe input rejects all corpus-metadata flags so the reviewed file completely
describes the run. `--dry-run` validates the recipe, destination, commands, and
Git evidence but does not execute a command or create temporary files. A real
run probes the produced directory and enters the same immutable plan, adapter,
Parquet, upload, journal, and contribution backend as direct ingestion.
Successful runs purge the entire prepared input workspace. Failed runs retain
verified preparation state beneath `ingest.staging`; an unchanged retry reuses
it, while a partially executed preparation is cleared and rerun.

Generated manifests use the established compact index shape. The source record
contains one aggregate acquisition digest; it never embeds a per-file or
per-record inventory. A recipe-driven run uses the existing
`converted_by.collector` string to pin repository, commit, and recipe path.
Dirty or uncommitted recipes are marked and include the recipe SHA-256. The
manifest has one entry per published Parquet shard containing its URL,
SHA-256, document count, reference-token estimate, and encoded byte size.
Detailed processing prose, command arrays, modality duplication, and input
inventories stay out of Git. Secrets and environment values are never written
to the manifest.

Publication is configured once through `waldo config set`; ingestion
has no second per-run destination or alternate partial execution mode.

When an external fetcher produced the input, the user passes its local files or
directory and the required source facts explicitly. A structured fetcher
handoff may be designed later, but is not part of the current CLI contract.

### Export a corpus selection

```bash
waldo index export core science \
  --license 'CC0-*,CC-BY-*' \
  ~/training-data

waldo index export core \
  --format jsonl \
  ~/portable-training-data
```

This is the first local fetch/materialization workflow. The command prints
selection totals and disk requirements before fetching. It
writes verified native shard files and an OpenWALDO BOM by default. `--format
jsonl` streams native Parquet rows into canonical interchange records, checks
each record's required fields and text hash, and records both the lookaside-object
and exported-file hashes in `EXPORT.json`. It does not build or train a model.

### Configure machine-local behavior

```bash
waldo config set lookaside.cache /fast-disk/waldo-cache
waldo config set lookaside.cache.max-size 100GiB
waldo config set lookaside.scratch /fast-disk/waldo-scratch
waldo config set ingest.staging /fast-disk/waldo-ingest
waldo config set model.root /fast-disk/waldo-models
waldo config set index /path/to/waldo-index
waldo config set lookaside.mirrors https://mirror.example/openwaldo/v1
waldo config get lookaside
waldo config show
waldo lookaside status
```

`lookaside.cache` stores complete, hash-verified objects for reuse, bounded by
`lookaside.cache.max-size` (20 GiB by default). `lookaside.scratch` contains
only partial downloads and is cleaned after both success and failure. A
materialization tries the manifest URL and then configured mirrors while
checking size and SHA-256, then atomically admits the object to the cache.
`lookaside status` reports both locations and `lookaside verify` scrubs every
retained object.

Default locations keep durable state and disposable work clearly separated:

```text
~/.waldo/models       durable model artifacts and their BOMs
~/.waldo/cache        retained, verified lookaside objects
<system temp>/waldo-<user>/scratch  partial object downloads
<system temp>/waldo-<user>/ingest   resumable ingestion working state
```

On macOS and Linux, `<system temp>` is normally beneath `/tmp` or the
user-specific temporary directory selected by the operating system. Explicit
configuration remains useful for large fast disks and shared compute nodes.

Unsetting a key restores its default:

```bash
waldo config unset lookaside.scratch
```

Inventory the S3 bucket containing the configured writable lookaside without
downloading object bodies. A `file://` lookaside inventories its configured
root:

```bash
waldo lookaside list
waldo lookaside list /path/to/waldo-index/or/subtree
waldo lookaside list /path/to/waldo-index/or/subtree --all
```

Human output is deliberately quiet: one fixed-width header followed by object
rows. It shows a shortened object hash, size, absolute UTC storage time, object
prefix, and index manifest. JSON output preserves full object paths,
hashes, timestamps, inventory context, missing references, and totals.

Canonical objects are recognized by a trailing
`<sha[0:2]>/<sha[2:4]>/<sha256>` path at any bucket prefix. With no index path,
all objects are shown and `INDEX` is `--`. The optional index path follows the
same recursive positional rules as other index commands and filters the table
to matching objects. `--all` includes unmatched bucket objects in the same
table with `INDEX` set to `--`. An unmatched object is never implicitly safe to
remove; another index, Git revision, or BOM may still reference it.

Remote or local published objects are removed only by exact content hash:

```bash
waldo lookaside rm <sha256> [<sha256>...]
```

Every name must be a complete lowercase SHA-256. WALDO confirms that every
listed object exists in the configured writable lookaside before deleting any
of them. It does not infer unused objects from a partial or absent index, and it
does not accept URLs, prefixes, or globs.

Configuration keys are positional and intentionally limited:

| Key | Purpose |
| --- | --- |
| `index` | Default local index checkout for logical model and index paths. |
| `lookaside` | Writable `s3://` production or `file://` test lookaside. |
| `lookaside.region` | AWS region when it cannot be inferred. |
| `lookaside.workers` | Concurrent completed-shard uploads, from 1 through 32. |
| `lookaside.mirrors` | Complete ordered list of read fallbacks. |
| `lookaside.cache` | Retained verified-object cache directory; default `~/.waldo/cache`. |
| `lookaside.cache.max-size` | LRU retention bound; default 20 GiB. |
| `lookaside.scratch` | Disposable partial-download directory beneath system temporary storage. |
| `ingest.staging` | Ingestion scratch and recovery directory beneath system temporary storage. |
| `model.root` | Durable local model, run, BOM, and artifact directory; default `~/.waldo/models`. |

`waldo config get` lists every supported key and its effective or unset value.
Passing a prefix such as `lookaside` narrows the list to matching keys;
passing an exact leaf such as `lookaside.region` prints only that value.

`config set` replaces the named value, `config get` reads one value, `config
show` displays the complete effective configuration, and `config unset`
returns a key to its default. Backend schemes are values, not separate flags.
Changing `lookaside` preserves the existing `lookaside.region` and
`lookaside.workers` values; switching to `file://` clears the S3-only region.

### Build a model from a compose

```bash
waldo model build configs/smoke.yaml
```

The model compose declares model identity, architecture, and ordered training stages.
The command validates the complete compose and corpus selections, forecasts
resources, creates the model if absent, and executes stages. Reusing a trained
model requires an explicit continuation option; replacing it requires a
separate explicit option.

The current development backend resolves to `fake@builtin-fake-schema-1`. It
exercises the complete state and provenance path but emits an artifact that
explicitly contains no trained weights. Backend selection is not part of the
portable compose. The schema-1 compose and durable layout are documented in
`docs/MODEL-LIFECYCLE.md`.

### Inspect provenance

```bash
waldo bom show ~/training-data
waldo bom verify ~/training-data
waldo model inspect smoke
```

Inspection distinguishes declared inputs, verified materialization, backend-
reported consumption, and output hashes. It must not describe a baseline BOM
as proof against a dishonest trainer.

The corpus form accepts an export directory or its `EXPORT.json` directly.
The model lifecycle also supports a fail-closed EU GPAI mapping:

```bash
waldo bom export smoke \
  --format eu-gpai \
  --provider docs/examples/eu-gpai-provider.json
```

With no output path, the converted document is written to standard output. An
optional final positional path such as `training-content.json` writes it
atomically instead. `--format` remains a flag. `--allow-incomplete` is the
explicit exception that emits a marked draft with all required, review, and
optional gaps. A normal export emits nothing while required facts are missing.
The current output is the machine-readable mapping and audit record; it does
not pretend to be the Commission's official editable Word artifact.

## Common options

The final spelling will be validated while implementing the first commands,
but these semantics should be consistent:

- Index locations are positional. Absolute paths, `./`/`../` paths, corpus
  directories, and manifest paths discover their enclosing checkout; omitted
  paths discover from the current directory.
- `--json` emits stable machine-readable output.
- `--quiet` suppresses progress but not errors or final machine output.
- `--dry-run` performs resolution and preflight without mutation.
- `--yes` confirms a previously displayed non-destructive plan; it is not a
  general bypass for irreversible operations.
- `--version` prints the binary version and source revision.

## Compatibility aliases

Old command spellings may be added only after the new workflows are complete.
Aliases must be thin translations into the new application layer, carry a
deprecation message when appropriate, and never preserve old internal logic.
