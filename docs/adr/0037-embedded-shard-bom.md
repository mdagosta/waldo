# ADR 0037: Embed ingest attestations in canonical shards

Status: accepted

## Decision

Current canonical text writer v9 and conversation writer `conversation-v1`
embed a schema-1 subject-`shard` OpenWALDO BOM in the Parquet footer. It pins
the ingestion plan, record kind and schema, writer recipe, reference tokenizer,
records, tokens, content bytes, licenses, assessments, redaction, and completed
checks.

Ingestion verifies canonical rows, footer aggregates, object identity, and the
embedded BOM before publication. `waldo index audit` verifies the object,
physical schema, footer, embedded BOM, and resolved corpus totals. `--deep`
also revalidates every record hash and token count.

Recognized older writer recipes remain readable under their explicit
compatibility rules. Unattested legacy writers require a deep scan.
