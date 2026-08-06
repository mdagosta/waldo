# ADR 0028: Make index metadata YAML-primary with JSON compatibility

- Status: accepted
- Date: 2026-08-05

## Context

Index navigation and corpus manifests are reviewed in Git by people. JSON is
machine-friendly but visually noisy for source, license, and shard metadata.
The existing index and compatibility fixtures use JSON, so changing the
preferred representation must not strand an existing checkout or create two
schema definitions.

## Decision

- Directory navigation and corpus manifests retain their existing `kind` and
  schema-1 data model.
- Readers accept `.yaml`, `.yml`, and `.json` metadata files. Directory
  discovery recognizes `index.yaml`, `index.yml`, or `index.json`.
- A directory containing more than one of those navigation files is invalid;
  WALDO never guesses which competing file is authoritative.
- Writers emit canonical `.yaml` files only. `index init` creates
  `index.yaml`; ingestion writes `<name>.yaml` manifests and `index.yaml`
  navigation.
- When an ingestion contribution touches navigation that currently uses JSON
  or `.yml`, the overlay contains the YAML replacement and explicitly lists
  the superseded path for removal and Git review.
- YAML is a single-document, JSON-compatible representation. Custom tags,
  aliases, non-string mapping keys, duplicate keys, non-finite numbers, and
  multiple documents are rejected. Unquoted YAML timestamps are retained as
  strings.
- Encoding passes through the existing JSON-tagged schema representation so
  custom behavior such as inline-versus-rollup `shards` has one owner.

## Consequences

New and touched index metadata is easier to review while old JSON trees remain
fully readable. A checkout can contain JSON metadata in untouched directories
and YAML metadata in new directories, but never two navigation files in the
same directory. This is an encoding transition, not a schema-version change;
all formats remain at schema 1.
