# Command-line experience

This document is the user-facing command contract. Commands may be introduced
in phases, but implemented behavior should follow this vocabulary unless an ADR
changes it.

## Design rules

1. Organize commands by the object a user intends to manage, not by an
   implementation phase.
2. Keep the common path short. Put destructive and operational commands behind
   explicit nouns and verbs.
3. Resolve index paths consistently from an explicit checkout or a path inside
   one. Do not maintain an invisible index clone.
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
│   ├── configure
│   ├── login
│   ├── status
│   ├── verify
│   ├── mirror
│   └── gc
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

There is no `compose` command group. A declarative model recipe is one input to
`waldo model build`.

There is no `fetch` command group. Source-specific acquisition belongs to the
future fetchers repository. WALDO consumes its output through the fetcher
handoff contract.

## Primary journeys

### Inspect and verify an index

```bash
waldo index list
waldo index list science
waldo index summary
waldo index show science/plos
waldo index verify
waldo index verify --objects
```

`index list` is recursive and returns one row per corpus beneath the selected
path, including logical path, title, shard/document/token/byte totals, and a
license summary. `index show` is the detailed view for one corpus or directory.

Structural verification is local and fast. `--objects` explicitly opts into
network access and verifies lookaside availability and hashes.

### Add a corpus

```bash
waldo index add ~/data/books \
  --to core/books \
  --title "Example Books" \
  --license CC-BY-4.0 \
  --source https://example.org/books
```

The command should preflight source metadata, projected shard count, output
size, destination, and lookaside configuration before conversion. On success
it should explain the exact Git review and DCO commit steps without creating a
pull request itself.

When an external fetcher produced the input, the invocation names its deposit
or acquisition record rather than repeating facts by hand.

### Export a corpus selection

```bash
waldo index export core science \
  --license 'CC0-*,CC-BY-*' \
  --output ~/training-data
```

The command prints selection totals and disk requirements before fetching. It
writes verified native shard files and a corpus BOM by default. An explicit
format option may request canonical interchange output.

### Build a model from a recipe

```bash
waldo model build configs/smoke.yaml
```

The recipe declares model identity, architecture, and ordered training stages.
The command validates the complete recipe and corpus selections, forecasts
resources, creates the model if absent, and executes stages. Reusing a trained
model requires an explicit continuation option; replacing it requires a
separate explicit option.

### Inspect provenance

```bash
waldo model inspect smoke
waldo bom show model:smoke
waldo bom verify model:smoke
```

Inspection distinguishes declared inputs, verified materialization, backend-
reported consumption, and output hashes. It must not describe a baseline BOM
as proof against a dishonest trainer.

## Common options

The final spelling will be validated while implementing the first commands,
but these semantics should be consistent:

- `--index <path>` selects an index checkout explicitly.
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
