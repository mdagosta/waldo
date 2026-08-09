# Command-line experience

This document is the user-facing command contract. Commands may be introduced
in phases, but implemented behavior should follow this vocabulary unless an ADR
changes it.

## Design rules

1. Organize commands by the object a user intends to manage, not by an
   implementation phase.
2. Keep the common path short. Put destructive and operational commands behind
   explicit nouns and verbs.
3. Resolve relative index paths beneath the contributor checkout set by `waldo
   config set index`, or the managed read-only `~/.waldo/index` checkout when
   unset. Clone that managed checkout on first use. Before use, automatically
   fast-forward any selected Git checkout only when it is clean and behind its
   tracking branch. Absolute and `~/` paths explicitly discover another
   checkout. When a corpus-consuming command omits its selection, use the
   entire resolved index without a warning.
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
│   ├── pull
│   ├── list
│   ├── show
│   ├── summary
│   ├── verify
│   ├── audit
│   ├── ingest
│   ├── update
│   └── export
├── shard
│   ├── summary
│   ├── audit
│   ├── list-records
│   └── export-record
├── lookaside
│   ├── login
│   ├── logout
│   ├── list
│   ├── status
│   ├── verify
│   ├── mirror (reserved)
│   └── rm
├── model
│   ├── init
│   ├── pull
│   ├── list
│   ├── summary
│   ├── bom
│   ├── forecast
│   ├── train
│   ├── compose
│   ├── export
│   ├── chat
│   └── rm
├── bom
│   ├── show
│   ├── verify
│   └── export
├── config
│   ├── show
│   ├── get
│   ├── set
│   └── unset
└── completion
```

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

### Use the managed public index

With no configuration, read and model workflows use `~/.waldo/index`. The
first such command clones `https://github.com/openwaldo/waldo-index.git` branch
`main` through WALDO's built-in Go Git client. Index-consuming commands then
fetch and fast-forward the selected checkout automatically when safe:

```bash
waldo index pull
```

`pull` operates on the configured checkout when one is set, otherwise on the
managed default. Automatic and explicit pulls accept only a clean
fast-forward; they refuse local changes, local-only commits, divergence,
detached HEAD, or a missing tracking remote. `index verify --offline` is the
exception: it uses the current local revision without network access.

The CLI does not expose `clone`, `fetch`, or `status` as Git-shaped index
commands. Clone is an implementation detail of first use, fetch is part of the
safe pull operation, and unsafe states are reported by the command that needs
the checkout. Contributors clone their own writable checkout with their normal
Git tooling.

### Initialize an empty index

```bash
waldo index init /path/to/new-waldo-index
```

The destination must be new or empty. WALDO writes only the root schema-1
`index.yaml`; it does not initialize Git, configure a lookaside, or create a
corpus. It also refuses the reserved managed checkout path. Those remain
separate, visible actions.

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

`index audit` verifies each materialized object's SHA-256, Parquet schema,
embedded ingest attestation, and aggregate totals without repeating work
already completed by the immutable shard builder. Unattested legacy shards
fall back to a complete record scan. `index audit --deep` explicitly
recomputes every record hash and reference-token count.

`index list` is recursive and returns one row per corpus beneath the selected
path, including logical path, title, shard/document/token/byte totals, and a
license summary. `index show` is the detailed view for one corpus or directory.

`--offline` structural verification is local and fast. The default uses
header-only network requests to establish canonical availability and size;
`--objects` verifies lookaside availability and hashes every byte. For a
rollup-backed corpus it also expands the content-addressed submanifest tree,
checks every level's totals, and then verifies the resolved leaf objects.

### Add a corpus

Corpus authoring requires an explicit contributor checkout:

```bash
git clone https://github.com/openwaldo/waldo-index.git /path/to/waldo-index
waldo config set index /path/to/waldo-index
```

The managed `~/.waldo/index` checkout is never an ingest or update target.

```bash
waldo index ingest ~/data/books core/books \
  --title "Example Books" \
  --license CC-BY-4.0 \
  --source https://example.org/books \
  --source-category public-dataset \
  --dry-run
```

For existing local structured data, `--input-profile profile.yaml` applies the
same strict profile shape embedded under `input` in an ingest recipe.

The first implemented slice hashes and senses every input, reads Parquet
footers without loading their payload, resolves unambiguous column mappings,
and emits an immutable plan with `--dry-run`. Extensions are hints only; magic
bytes and container structure take precedence.

For the basic text and Markdown adapter, one file is one logical document and
its exact NUL-free UTF-8 bytes are preserved. The accepted plan pins a 16 MiB
adapter batch target, a 64 MiB default maximum indivisible record, and the
exact Parquet writer recipe. A reviewed ingest recipe may raise the record
maximum as high as 256 MiB, still bounded by half the plan memory budget.
Execution re-hashes each file before emitting it. A larger file is never
silently split, truncated, repaired, or rendered from Markdown.
Recipe-driven text and Markdown rows also retain their validated path relative
to the acquisition directory as `meta.source_path`; direct local ingestion
does not invent a relative root when none was declared.

JSONL ingestion projects one top-level string field named `text` per nonblank
line. Plain `.jsonl`, gzip, and zstd inputs stream through bounded decoding;
they are not expanded into an intermediate file. Unknown JSON members are not
copied into the canonical row. Missing, null, empty, oversized, non-UTF-8, and
NUL-containing text values fail the ingest.

Ingest recipes can replace the default single-field adapters with declarative,
corpus-neutral profiles. JSON is one object per file, JSONL is one object per
line (including streamed gzip and zstd), and Parquet is one row per record.
Supported mappings are `record-map`, `dialogue-pair`,
`ranked-conversation-tree`, `bounded-text`, and `xml-record`; source-specific
knowledge remains in the recipe or fetcher.
Mapped record profiles fail on embedded NULs unless they explicitly declare
`nul: space`; that deterministic replacement and any record-limit override
participate in the immutable plan identity.

The command preflights source metadata, projected shard count, output size,
destination, and lookaside configuration before conversion. On success
it should explain the exact Git review and DCO commit steps without creating a
pull request itself.

Configure the writable lookaside once, then store the bucket's access and
secret keys in `~/.waldo/credentials`. The secret prompt does not echo and
neither key is written to WALDO configuration:

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
does not replace previously saved credentials. The credential directory is
mode `0700`, the credential file is mode `0600`, and WALDO refuses a file
readable by group or others. Environment, shared-file, and workload-role
credentials remain supported when no WALDO login exists.

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

When `index` is configured, relative destinations resolve beneath that
checkout regardless of the current directory. An absolute or `~/` destination
explicitly selects another checkout:

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
`kind: waldo-ingest-recipe` and schema 1 or 2:

```bash
waldo index ingest ../waldo-fetchers/recipes/common-pile/foodista.yaml \
  core/common-pile/foodista --dry-run
```

Schema 1 supplies one license, source, input mapping, and ordered command list.
Schema 2 supplies `sources[]`, each with its own ID, license, source facts,
mapping, and commands. Bare command names resolve through `PATH`; commands containing a
path separator resolve explicitly from the recipe file unless absolute. WALDO
hashes the recipe and every resolved executable before execution, runs each
command directly without an intervening shell, and rechecks those hashes
afterward. Schema-1 commands share a private temporary directory; each
schema-2 source gets a separate one. Commands receive that directory's absolute
path in `WALDO_FETCH_DIR` and must write only acquired artifacts there.
`WALDO_INGEST_RECIPE` names the absolute recipe path.

Recipe input rejects all corpus-metadata flags so the reviewed file completely
describes the run. `--dry-run` validates the recipe, destination, commands, and
Git evidence but does not execute a command or create temporary files. A real
run probes the produced directory and enters the same immutable plan, adapter,
Parquet, upload, journal, and contribution backend as direct ingestion.
Successful runs purge the entire prepared input workspace. Failed runs retain
verified preparation state beneath `ingest.staging`; an unchanged retry reuses
it, while a partially executed preparation is cleared and rerun.

Generated manifests use the established compact index shape. The source record
contains one aggregate acquisition digest over its source declaration and
accepted input facts; it never embeds a per-file or per-record inventory. A
recipe-driven run uses the existing
`converted_by.collector` string to pin repository, commit, and recipe path.
Dirty or uncommitted recipes are marked and include the recipe SHA-256. The
manifest has one entry per published Parquet shard containing its URL,
SHA-256, represented source/license sets, exact per-license document and token
usage, total document count, reference-token estimate, and encoded byte size.
Detailed processing prose, command arrays, modality duplication, and input
inventories stay out of Git. Secrets and environment values are never written
to the manifest.

The canonical Parquet footer carries a subject-`shard` OpenWALDO BOM. It pins
the ingest plan, schema, writer, tokenizer, represented license set, aggregate
totals, and builder validation claims without adding a synthetic training row.
`waldo shard bom <path...>` displays that evidence directly. `waldo index
audit --show-boms <path>` lists every reconciled shard-BOM identity; JSON audit
output includes the verified corpus BOM with its per-shard attestations.
Recipe-driven ingestion excludes bounded malformed, empty, or unmappable
records and WALDO prints a prominent warning with counts by reason before
presenting the contribution for review.

### Update an existing corpus

An update target must resolve exactly one indexed manifest:

```bash
waldo index update ../waldo-fetchers/recipes/common-pile/foodista.yaml \
  /path/to/waldo-index/core/common-pile/foodista/foodista.json
```

Normal update materializes and audits the existing shards, streams their exact
content identities into the disk-backed deduplication set, and publishes only
records absent from the current corpus. A recipe receives a private
`UPDATE-STATE.json` through `WALDO_UPDATE_STATE`; it contains the pinned
manifest hash, existing source version and collection facts, and aggregate
shard totals. Source-specific fetchers may use those facts to request only a
new release, time range, commit, or cursor. WALDO's record-level check remains
authoritative even when a fetcher retrieves overlapping material. An entirely
duplicate update is a successful no-op.

For a complete authoritative rebuild from a fresh recipe acquisition:

```bash
waldo index update ../waldo-fetchers/recipes/common-pile/foodista.yaml \
  /path/to/waldo-index/core/common-pile/foodista/foodista.json \
  --rebuild-shards
```

Rebuild mode does not download or combine the old shards. It deduplicates the
recipe's complete output, writes new shards using the current 256 MiB target,
and replaces the manifest's shard and source set. Existing lookaside objects
remain untouched. Both modes pin the original manifest bytes before fetching,
recheck them before producing the contribution, and rewrite the touched
manifest as schema-2 YAML and navigation as schema-1 YAML. Superseded `.json` or
`.yml` paths are listed explicitly for removal.

Recipe `source` may include `version`, upstream `license_evidence`, `content`,
`acquisition`, and `collected_from`/`collected_to`. The collected fields are the
acquisition period; `content.from`/`content.to` are the distinct underlying
content period, and `content.selection` preserves any subset rule implemented
by fetcher arguments. These facts enter the immutable plan, source acquisition
identity, compact manifest, OpenWALDO BOM, and update state.

Index metadata is YAML-primary: new manifests use `<name>.yaml` and generated
navigation uses `index.yaml`. Readers retain schema-1 compatibility with
`.json`, `.yaml`, and `.yml`. When an overlay changes navigation that is still
JSON or `.yml`, its result lists both the YAML replacement and the superseded
path to remove before verification and commit. Two competing navigation files
in one directory are rejected.

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

# No selection exports the entire resolved index.
waldo index export ~/complete-training-data
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

Default locations keep durable state and temporary working data clearly separated:

```text
~/.waldo/models       durable model artifacts and their BOMs
~/.waldo/index        managed read-only public index checkout
<system temp>/waldo-<user>/cache    retained, verified lookaside objects
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

Unlike corpus-consuming commands, argumentless `lookaside list` remains an
unfiltered object inventory and emits no whole-index warning. Pass `.` to
filter it against the entire resolved index.

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
| `index` | Contributor checkout override; default is managed `~/.waldo/index`. |
| `lookaside` | Writable `s3://` production or `file://` test lookaside. |
| `lookaside.region` | AWS region when it cannot be inferred. |
| `lookaside.workers` | Concurrent completed-shard uploads, from 1 through 32. |
| `lookaside.mirrors` | Complete ordered list of read fallbacks. |
| `lookaside.cache` | Retained verified-object cache directory; default beneath system temporary storage. |
| `lookaside.cache.max-size` | LRU retention bound; default 20 GiB. |
| `lookaside.scratch` | Disposable partial-download directory beneath system temporary storage. |
| `ingest.staging` | Ingestion scratch and recovery directory beneath system temporary storage. |
| `model.root` | Durable local model, run, BOM, and artifact directory; default `~/.waldo/models`. |
| `model.backend` | `auto` (default), or explicit `mlx`, `torchtitan`, `pytorch`, or development-only `fake`. |

`waldo config get` lists every supported key and its effective or unset value.
Passing a prefix such as `lookaside` narrows the list to matching keys;
passing an exact leaf such as `lookaside.region` prints only that value.

`config set` replaces the named value, `config get` reads one value, `config
show` displays the complete effective configuration, and `config unset`
returns a key to its default. Backend schemes are values, not separate flags.
Changing `lookaside` preserves the existing `lookaside.region` and
`lookaside.workers` values; switching to `file://` clears the S3-only region.

### Create and train models

```bash
waldo model init smoke --preset 10m
waldo model pull llama-base huggingface://organization/model@main
waldo model train smoke core/books
waldo model train smoke core/books --epochs 3
waldo model train smoke core/books --audit
waldo model train smoke # entire resolved index, with a warning
waldo model compose smoke configs/smoke.yaml
waldo model compose smoke configs/smoke.yaml --audit
```

The CLI name is the local model handle. `model pull` resolves a Hugging
Face reference to an immutable revision and publishes a model only after its
source files, architecture, tokenizer, and Safetensors contract validate. It
honors `HF_TOKEN` and the standard Hugging Face token file. The initial profile
supports standard Llama plus OpenWALDO's byte tokenizer and fails closed for
other tokenizers.

A model compose declares architecture and ordered training stages using index
paths. It may optionally name a locally managed downloaded base and assert its
origin hash. The command validates every selection and hash-verifies every
materialized object. `--audit` additionally verifies shard structure,
attestations, and declared totals before any training begins. It creates the model if
absent, and executes its stages. Existing names are refused unless `--replace`
is explicitly supplied. The active compose always occupies its standard
`<model.root>/<name>` directory, so ordinary model inspection sees its current
run state. Content-identified transaction metadata and any replacement rollback
backup remain hidden. Held-out selection reports shard, record, and byte
progress before the training backend starts.
Repeating the exact command after interruption resumes its current stage and
run; different compose inputs are refused while that transaction is unfinished.

Direct `model train` defaults to one epoch. `--epochs` is the sole direct-run
training-budget flag and means complete passes over the training partition.
WALDO deterministically holds out a bounded one-percent sample, pins its
selection evidence in the run BOM, and excludes it from every epoch. Preflight
distinguishes index reference tokens from exact post-holdout model-token
targets, then shows epochs, holdout records, derived optimizer updates, batch
size, and sequence length before backend launch. A step is one optimizer
update, not one corpus pass.

Checkpoint events become durable only after WALDO verifies a complete atomic
bundle containing weights, optimizer/runtime state, and exact progress.
Repeating the same `model train` command after an interruption resumes the
same run and adds an attempt to `RUN.json`; it does not invent a new completed
history. Changed inputs, parameters, backend revision, or execution environment
do not qualify for resume.

On macOS, automatic backend resolution always selects MLX and requires Apple
Silicon; it probes candidate Python runtimes and accepts MLX only after
executing a real operation on Metal. On Linux, automatic resolution probes for
TorchTitan first and PyTorch second. PyTorch is an executable single-process
adapter for usable CPU, NVIDIA CUDA, and AMD ROCm installations. TorchTitan
launches one rank per visible GPU on a single Linux node and uses its device
mesh with PyTorch FSDP2. The real workers accept the built-in byte-tokenizer
presets, compute no-gradient held-out loss and perplexity, and produce
resumable checkpoint bundles plus terminal Safetensors weights with the same
internal tensor contract. If no
compatible real backend is available, training warns and fails with
installation guidance before creating a run; simulation is never an automatic
fallback. `waldo config set model.backend fake` is the explicit
development-test exception and permanently marks its artifacts simulated.
Backend selection is not part of the portable compose. The schema-1 compose
and durable layout are documented in `docs/MODEL-LIFECYCLE.md`.

### Inspect provenance

```bash
waldo index bom ~/training-data
waldo index verify ~/training-data
waldo model summary smoke
waldo model bom smoke
```

The model BOM is a portable inventory rooted at the model directory. A
pulled model selects its verified origin until a later real run supersedes
it. Artifact paths include the complete run directory, label simulated output
and artifact roles explicitly, and identify the current usable weights.
Absolute machine paths are intentionally excluded. The same paths therefore
resolve beneath either `model.root/<name>` or a relocated `model export`
directory.

`waldo model chat <name>` is interactive when standard input is a terminal.
An optional positional prompt or piped standard input selects one-shot
generation. Only generation controls are flags: `--max-tokens`,
`--temperature`, `--top-p`, and `--seed`. The command verifies the BOM-selected
artifacts before opening MLX for a compatible origin or the backend recorded by
a run. Human output streams with terminal controls escaped;
`--json` is one-shot and buffered into one result object. Models without a
chat template are labeled as raw causal continuation rather than presented as
instruction-tuned assistants.

Inspection distinguishes declared inputs, verified materialization, backend-
reported consumption, and output hashes. It must not describe a baseline BOM
as proof against a dishonest trainer.

The corpus form accepts an export directory or its `EXPORT.json` directly.
The model lifecycle also supports a fail-closed EU GPAI mapping:

```bash
waldo config set disclosure.provider docs/examples/eu-gpai-provider.json
waldo model bom smoke \
  --format eu-gpai
```

With no output path, the converted document is written to standard output. An
optional final positional path such as `training-content.json` writes it
atomically instead. `--format` remains a flag. `--allow-incomplete` is the
explicit exception that emits a marked draft with all required, review, and
optional gaps. A normal export emits nothing while required facts are missing.
The current output is the machine-readable mapping and audit record; it does
not pretend to be the Commission's official editable Word artifact.

`waldo model export smoke ./release` creates the default native package with
`BOM.json` and `EU-BOM.json`. Provider-level facts come from
`disclosure.provider`; model-release facts remain model-specific. If
`signing.method` is configured, WALDO automatically creates detached
`BOM.sigstore.json` and `EU-BOM.sigstore.json` bundles and aborts publication
on any signing failure. With no signing configuration it emits an unsigned
warning.

Exactly one runtime representation is selected with `--format waldo`,
`huggingface`, `mlx`, `gguf`, or `ollama`. The GGUF package contains
`model.gguf`; the Ollama package adds a `Modelfile` that can be passed directly
to `ollama create`. Neither package duplicates the weights as Safetensors.
GGUF and Ollama optionally accept `--quant 2|3|4|5|6|8` and
`--calibration <index-path>`. Calibration is a deterministic bounded
importance sample from hash-verified, audited index shards; it is not training.
See [the model export guide](MODEL-EXPORTS.md) for the package layouts, BOM
semantics, format conversions, disclosure requirements, and signing contract.

## Common options

The final spelling will be validated while implementing the first commands,
but these semantics should be consistent:

- Index locations are positional. Relative and omitted paths use the configured
  contributor checkout or managed default; absolute and `~/` paths explicitly
  discover another checkout.
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
