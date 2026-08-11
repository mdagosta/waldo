# Command guide

This guide describes the current CLI. `waldo <command> --help` is authoritative
for flags and detailed behavior.

## Command tree

```text
waldo
├── advisor
├── config
│   ├── show
│   ├── get
│   ├── set
│   └── unset
├── index
│   ├── init
│   ├── pull
│   ├── list
│   ├── show
│   ├── summary
│   ├── verify
│   ├── audit
│   ├── bom
│   ├── ingest
│   ├── update
│   └── export
├── shard
│   ├── summary
│   ├── audit
│   ├── bom
│   ├── list-records
│   └── export-record
├── lookaside
│   ├── login
│   ├── logout
│   ├── list
│   ├── status
│   ├── verify
│   ├── mirror
│   └── rm
├── model
│   ├── advisor
│   ├── init
│   ├── pull
│   ├── list
│   ├── summary
│   ├── bom
│   ├── forecast
│   ├── train
│   ├── continue
│   ├── export
│   ├── chat
│   └── rm
└── completion
```

There is no top-level `waldo bom` command. Corpus BOM operations are under
`waldo index bom`; model BOM operations are under `waldo model bom`.

## Index selection

When `config.index` is unset, read-only index commands use a managed checkout
at `~/.waldo/index`. WALDO creates it on first use and safely fast-forwards it
when the checkout is clean and behind its tracking branch.

Set `config.index` to use a writable contributor checkout:

```bash
waldo config set index /path/to/waldo-index
```

Relative index paths resolve beneath the selected checkout. Absolute and `~/`
paths explicitly select another checkout. Omitting a path selects the whole
resolved index. `waldo index verify --offline` performs local structural
validation without synchronizing or accessing object storage.

## Inspect and verify data

```bash
waldo index list
waldo index show core/example
waldo index summary
waldo index verify --offline
waldo index verify core/example
waldo index verify core/example --objects
waldo index audit core/example
```

Default verification checks metadata and canonical object availability without
downloading object bodies. `--objects` downloads and hashes every selected
object. `audit` additionally validates canonical shard contents.

Local Parquet files can be inspected without an index:

```bash
waldo shard summary shard.parquet
waldo shard audit shard.parquet
waldo shard bom shard.parquet
waldo shard list-records shard.parquet
```

## Contribute data

Authoring commands refuse the managed read-only checkout. Clone the index and
configure a writable checkout first:

```bash
git clone https://github.com/openwaldo/waldo-index.git
waldo config set index /path/to/waldo-index
waldo config set lookaside file:///tmp/waldo-lookaside
```

Inspect the complete ingest interface before use:

```bash
waldo index ingest --help
waldo index update --help
```

Ingestion accepts supported local files or a reviewed ingest recipe. It writes
a contribution overlay for normal Git review; WALDO does not commit or open a
pull request.

## Export data and inspect its BOM

```bash
waldo index export core/example /path/to/export
waldo index bom /path/to/export
waldo index verify /path/to/export
```

An export contains `EXPORT.json` plus the selected data files. `index verify`
checks the persisted BOM and hashes each exported file.

## Lookaside storage

```bash
waldo lookaside status
waldo lookaside list
waldo lookaside verify
waldo lookaside mirror --help
waldo lookaside rm --help
```

For S3 publication, configure the destination and store bucket-scoped
credentials outside corpus metadata:

```bash
waldo config set lookaside s3://bucket/prefix
waldo config set lookaside.region us-west-2
waldo lookaside login
```

Removal requires explicit object names. Review command help before any
consequential lookaside operation.

## Models

The model lifecycle can start from an index selection, a compose file, or a
supported open-weight origin. Available training backends depend on the host.

```bash
waldo model forecast composes/0000-canary.yaml
waldo model train canary composes/0000-canary.yaml
waldo model summary canary
waldo model bom canary
waldo model chat canary
waldo model export --help
```

Run `forecast` before allocating substantial compute. Training and generation
fail when the selected host lacks a compatible runtime or artifacts.

Reference composes under `composes/` are test and experiment inputs, not model
quality guarantees.

## Output and help

Human-readable output is the default. Global `--json` emits structured results
while progress remains on standard error.

```bash
waldo --json config get
waldo index verify --help
waldo completion --help
```
