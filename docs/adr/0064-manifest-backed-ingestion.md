# 0064: Make manifest-backed raw directories the ingestion boundary

- Status: accepted
- Date: 2026-08-27
- Supersedes: [0005](0005-external-fetchers.md),
  [0014](0014-explicit-ingest-recipe.md),
  [0038](0038-preserve-recipe-source-paths.md), and
  [0039](0039-recipe-source-evidence.md)
- Amends: [0033](0033-corpus-update-modes.md) and
  [0034](0034-declarative-ingest-profiles.md)

## Context

Ingest recipes combine acquisition authorization, external command execution,
metadata, and input interpretation in one WALDO input. That makes the normal
ingestion boundary harder to explain and duplicates the separate fetcher
system. It also encourages format adapters to appear as external converters
instead of reviewed WALDO behavior.

The existing `waldo-corpus-directory` manifest already provides the cleaner
boundary: acquisition stops after producing a verified recursive raw tree, and
WALDO begins with declarative metadata and input semantics.

## Decision

The canonical ingestion input is a manifest-backed raw directory. Its root
`manifest.json` owns corpus and source facts, input format and mapping, artifact
evidence, and deterministic raw-tree evidence. The contract applies to every
producer, not only OpenWALDO fetchers.

WALDO recursively inventories and hashes all regular files inside declared
source boundaries. Raw-file inventory and logical-record cardinality are
separate. The manifest selects a built-in adapter, and that adapter defines how
the verified tree becomes records. Tree-aware adapters perform deterministic
root discovery and dependency resolution inside the boundary.

Acquisition never runs inside WALDO. Fetchers and other tools write the raw
directory and manifest, then stop. A manifest cannot name an executable,
external converter, or runtime adapter.

`waldo-ingest-recipe` schemas are obsolete. Existing readers may remain for
compatibility, but new corpora, fetchers, features, and documentation must not
depend on them. No new recipe schema or recipe execution feature will be added.

Direct file ingestion remains a local convenience. Reviewable and
reproducible corpus contributions use the manifest-backed directory.

## Consequences

- Ingestion has one documented trust boundary independent of acquisition.
- Corpus metadata and input interpretation travel with the verified raw tree.
- New formats require reviewed built-in WALDO adapters.
- Fetcher output is inspectable and reusable before an index destination is
  chosen.
- Recipe execution code remains compatibility debt and may be removed in a
  later compatibility-breaking release.
