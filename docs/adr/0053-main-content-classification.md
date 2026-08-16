# 0053: Classify main content with a default-true row fact

## Status

Accepted.

## Decision

The current canonical record schema 2 adds the required `main_content` boolean.
All retained rows default to `true`. A structured input profile may instead
declare one or more exact scalar conditions:

```yaml
input:
  main_content:
    metadata.namespace: 0
```

Every declared value must match to produce `true`; any other value produces
`false`. A missing declared field fails ingestion so upstream schema drift
cannot silently change classification. The recipe mapping is general and
participates in plan and acquisition identity; WALDO contains no Wikimedia- or
corpus-specific logic. The conjunction extension is recorded in ADR 0057.

Composes select primary rows directly:

```yaml
filter:
  main_content: true
```

Schema-1 rows and the initial schema-2 physical layout predate the column and
read as `true`. This preserves existing corpus behavior; a corpus that needs
real primary/auxiliary separation must be rebuilt with an explicit mapping.
The physical writer identity is
`parquet-go/0.30.1/zstd-6/page-1m/rg-64m/v8-main-content`.
