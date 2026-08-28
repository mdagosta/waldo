# ADR 0038: Preserve recipe-relative paths for file records

Status: superseded by [0064](0064-manifest-backed-ingestion.md)

## Context

Recipe acquisition already validates and pins every input path in
`PlanInput.SourcePath`, but the basic text and Markdown adapter discarded that
path after setting the canonical row source to the artifact content hash. This
is insufficient for file-oriented corpora such as pinned source repositories:
the manifest identifies the repository and commit, while the row must retain
which tracked file supplied its text.

Putting every path in the Git manifest would make lightweight metadata scale
with record count. Inventing a URL from a repository landing page and relative
path would claim a source address that may not actually resolve.

## Decision

For recipe-driven text and Markdown inputs, copy the already validated
acquisition-relative path into the existing canonical JSON `meta` column as
`source_path`. Keep `source` as the exact artifact content hash and
`source_name` as the corpus-level source identity.

Direct local ingestion has no declared acquisition root and therefore does not
gain path metadata implicitly. No canonical schema or manifest field is added.

## Consequences

Repository and commit remain compact source facts, each canonical file row is
attributable to a stable recipe output path, and Git metadata remains bounded
by shard count. Recipe authors must keep completed output paths deterministic.
