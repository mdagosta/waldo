# ADR 0008: Expand submanifest trees before materialization

Status: accepted
- Date: 2026-08-04

## Context

Schema-1 manifests encode `shards` polymorphically. Small corpora contain an
inline array. Large corpora may instead contain a content-addressed rollup
whose JSON object points to a submanifest tree in the lookaside. The root
rollup makes offline totals available, but its leaf shard list and any
per-shard license overrides cannot be known without reading that tree.

An OpenWALDO BOM used for object verification or export must identify the
actual leaf objects. Treating the root's declared totals as if they were the
resolved selection would make license filtering and replay incomplete.

## Decision

The index reader accepts an array or rollup object in the manifest's `shards`
field. Offline list, summary, and structural verification use the root's
declared totals and do not imply that the external tree was inspected.

Before object materialization, BOM construction recursively fetches every
submanifest through verified lookaside scratch. It checks each object's
SHA-256, validates its `kind` and `schema`, resolves leaf fields against the
Git-pinned manifest, and verifies that every parent's count, document, token,
and byte totals exactly equal its descendants. Repeated submanifest hashes are
rejected because they would make membership and totals ambiguous.

The resulting OpenWALDO BOM pins every fetched submanifest with its manifest,
parent hash, URL, hash, declared totals, and encoded size. It also contains the
fully resolved leaf shard pins after license policy is applied.

## Consequences

- A rollup-backed corpus can use the same verification and export path as an
  inline corpus.
- Network access remains limited to commands that explicitly materialize
  objects; ordinary index inspection stays local.
- A root hash commits transitively to the tree, while explicit submanifest pins
  make the resolution independently inspectable and replayable.
- License filtering is based on resolved leaf facts, not only the manifest's
  default license.
- Resolving a very large corpus incurs one verified read per submanifest before
  leaf objects are fetched.
