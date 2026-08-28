# ADR 0057: Preserve scored conversation metadata

Status: accepted

## Decision

Conversation ingestion profiles preserve declared named metadata mappings,
including ratings and other source annotations, alongside structured messages.
WALDO does not flatten conversations or apply a model prompt template during
ingestion.

`input.main_content` accepts a conjunction of exact scalar field matches. A
missing field fails ingestion; a differing value marks the row as auxiliary.
This permits a compose using `main_content: true` to select a reviewed subset
while preserving the original score metadata.

Ratings remain metadata. WALDO does not currently expose a preference-training
objective that interprets them directly.
