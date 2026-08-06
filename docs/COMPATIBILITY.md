# Compatibility policy

The rebuild distinguishes durable public data from replaceable implementation.
Compatibility is intentional, tested, and limited to the surfaces below.

## Required compatibility

### Existing index checkouts

The binary and the unreleased `waldo-index` use one clean baseline:

- Directory navigation kind `index`, schema 1, read from `index.yaml`,
  `index.yml`, or the established `index.json`
- Manifest kind `manifest`, schema 1, read from `.yaml`, `.yml`, or `.json`
- Existing source, conversion, shard, rollup, and inheritance fields
- Current Git checkout path semantics
- Current Parquet shards and legacy shard formats still referenced by a valid
  manifest
- SHA-256 object identity and existing lookaside URLs

The pre-release schema-2 directory indexes were migrated in place. WALDO does
not retain a schema-2 compatibility reader: an unsupported schema fails closed
and must be migrated explicitly. The acceptance suite proves that the schema-1
tree's corpus, shard, document, token, byte, and license totals remain intact.

Schema-1 `shards` is deliberately polymorphic: readers accept either the
inline array or the documented rollup object. Object-enabled operations verify
and expand rollup trees; local summaries use their Git-pinned aggregate totals.

YAML is the canonical encoding for new writes. This does not increment the
schema because the represented fields and meaning are unchanged. Existing
JSON navigation and manifests remain readable indefinitely for the schema-1
compatibility surface. A touched JSON navigation file is replaced explicitly
with YAML; WALDO rejects two competing navigation files in one directory.

### Existing object identities

The rebuild must not produce different bytes under an existing recipe identity.
Before writing new index contributions, canonical metadata and shard encoding
will be locked by fixtures derived from the authoritative specification and
existing public objects.

If the new packer intentionally changes bytes, it must use a new recorded
recipe identifier. It must never claim to reproduce an old recipe.

### Provenance meaning

Existing manifest license and source assertions retain their current meaning.
The rebuild may represent them differently internally but may not silently
strengthen, weaken, or discard them.

## Compatibility that is not automatic

- Old CLI command organization and flag spellings
- Old configuration files
- The old model store's internal JSON layout
- Checkpoint files produced by old training harnesses
- Internal Go packages or APIs
- Old help text and interactive prompts
- Undocumented behavior

Compatibility adapters may be added after the new primary workflows are
complete. They must translate into the new domain model rather than reintroduce
parallel execution paths.

## Initial acceptance fixture

The real public index currently provides the first read-compatibility target:

- 20 corpora
- 1,087 shards
- 75.1 million documents
- approximately 124.0 billion tokens
- approximately 157.7 GB of compressed objects

Exact integer totals and the license partition will be captured from the
checkout in automated compatibility fixtures during Phase 1. Rounded values in
this document are descriptive, not test constants.

## Evolution

Every persistent format has an explicit `kind` and `schema`. A change is one of:

1. Additive and readable by the current schema rules
2. A new schema with a defined reader and migration story
3. A new artifact kind with a separate contract

Format changes require an ADR, fixtures, and cross-platform tests. Go package
refactors do not.
