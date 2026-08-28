# ADR 0015: Keep corpus manifests compact

Status: accepted

## Decision

Generated corpus manifests use schema 2 and contain compact source, conversion,
record-kind, assessment, redaction, shard, license, and aggregate facts. They
do not copy the complete raw-file inventory or record-level evidence into Git.

The manifest-backed input directory owns raw-tree evidence and input mapping.
The immutable ingestion plan and recovery journal may retain machine-local
execution facts while a command is active. Canonical Parquet rows retain
record-level source, license, and content identity.

Manifest token totals use the named embedded reference tokenizer. Changing the
counter, canonical row shape, or physical writer changes the recorded recipe
identity.
