# Architecture

## System shape

WALDO is one binary containing several bounded domains. The binary is the
distribution and user-experience boundary; it is not permission for arbitrary
package coupling.

```text
CLI
 ├─ index ─────────────────────────────┐
 ├─ corpus ── index + lookaside ──────┼─ provenance
 ├─ lookaside                         │
 └─ model ── OpenWALDO BOM ─────────┘
              └─ training backend
```

Dependencies point from model workflows toward the corpus contract. Index,
corpus, and lookaside code never depend on model or training code.

## Domain responsibilities

### Index

Owns the Git metadata tree and its meaning:

- Index and manifest schemas
- Canonical metadata encoding
- Path resolution and traversal
- Manifest inheritance
- Structural verification
- Queries, summaries, and generated navigation files
- Index revision identity and dirty-checkout reporting

The index never reads model state and never runs a trainer.

### Record and license

Own the canonical document schema, record identity, license normalization, and
policy matching used by corpus construction and selection. These definitions
must not be duplicated in a fetcher or training backend.

### Lookaside

Owns content-addressed object transport and lifecycle:

- Verified download scratch, purged after successful consumers
- Header-only canonical-object reachability and size probes
- Anonymous HTTP and S3 reads
- Authenticated S3 reads and writes through the internal AWS SDK
- Bucket-scoped interactive credentials in the OS keychain, with the AWS
  environment and workload-role chain as the headless fallback
- Mirrors
- Availability and integrity checks
- Whole-bucket S3 object inventory with configured-prefix markers and optional
  recursive index-reference annotations
- Explicit removal of fully named objects; no index-free garbage collection

A lookaside object contains bytes. The index contains their meaning. Lookaside
code must not treat an object's URL or location as provenance.

### Corpus

Owns workflows that turn index meaning and lookaside objects into useful,
immutable data selections:

- Corpus ingestion and deterministic packing
- Corpus selection and license policy
- Export
- OpenWALDO BOM construction

Its central output is an OpenWALDO BOM (`corpus.BOM`), the only normal handoff to model
workflows.

Conceptually:

```go
type BOM struct {
    Kind      "openwaldo-bom"
    Subject   "corpus"
    Index     IndexIdentity
    Selection Selection
    Manifests []ManifestPin
    Shards    []ShardPin
    Totals    Totals
}
```

The BOM contains resolved facts, not pointers to mutable manifests or implicit
access to an index checkout.

### Provenance

Owns the vocabulary and serialization of:

1. OpenWALDO BOMs and export records
2. Training run records
3. Model lineage and aggregate model BOMs

These are related records, not one giant optional structure. A run references
an OpenWALDO BOM and adds planned parameters, observed consumption, backend
identity, status, and outputs.

### Model

Owns model identity, immutable architecture, recipes, lifecycle, lineage, and
artifact export. It asks the corpus domain for OpenWALDO BOMs and gives a fully
resolved execution request to a training backend.

It must not parse index files, normalize licenses, choose lookaside mirrors, or
accept an unverified shard path from CLI code.

### Training

Owns the adapter boundary to an execution framework. A backend receives an
explicit request and returns observed results. It does not own model state or
BOM persistence.

The application writes a `planned` run before launching the backend, advances
it to `running`, and persists exactly one terminal state: `complete`, `failed`,
or `interrupted`. Backend-reported consumed tokens and output hashes are
recorded as observations rather than replaced by projected corpus totals.

## Expected package layout

```text
cmd/waldo/          process entry point
internal/cli/       parsing, help, and presentation
internal/config/    machine-local transport and execution preferences
internal/canon/     canonical JSON primitives shared by durable formats
internal/index/     metadata schemas, tree, resolver, verification
internal/record/    document schema and canonical representation
internal/shard/     native shard decoding and interchange conversion
internal/license/   normalization and selection policy
internal/lookaside/ verified object access and lifecycle
internal/acquire/   bounded source adapters and local acquisition records
internal/corpus/    ingestion, selection, OpenWALDO BOMs, export
internal/provenance/BOM types and verification
internal/model/     model lifecycle and recipes
internal/training/  backend interface and adapters
internal/platform/  narrow OS, Git, and process adapters when needed
```

This is a map, not a requirement to create empty packages. Packages are added
with the first vertical slice that needs them.

## Implementation rules

- Domain types do not depend on CLI types.
- CLI handlers translate arguments into explicit application requests.
- External systems sit behind narrow interfaces owned by their consumer.
- File writes that establish state use temporary files plus atomic rename.
- Large-object operations stream; corpus size must not imply equivalent memory
  use.
- Network access is explicit in the command and injectable in tests.
- Deterministic formats use golden-byte tests on Linux and macOS.
- Errors retain the relevant path, hash, source, or run identifier.
- Configuration contains machine preferences, never corpus meaning.

## Fetcher boundary

Fetchers live in a separate repository as reviewed shell scripts, as defined in
`docs/FETCHER-CONTRACT.md`. Direct ingestion consumes an independently prepared
local directory. Composed ingestion is an explicit alternative: when the user
passes a strict `waldo-ingest-compose` file, WALDO executes only its named
commands in sequence with a private temporary directory as their working
directory. Fetchers stop after populating that directory. WALDO then owns
probing, conversion, sharding, publication, cleanup, provenance, and the index
contribution. Dry-run resolves and hashes commands but never executes them.

Source-specific network logic and scripts do not enter this Go module. Merely
reading or verifying an index never executes code; script execution is
authorized only by the compose path passed positionally to `index ingest`.
