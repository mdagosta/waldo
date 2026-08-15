# 0052: Detect email-shaped strings as immutable row facts

## Status

Accepted.

## Decision

Canonical Parquet record schema 2 adds the required boolean
`email_addresses`. During every ingestion WALDO evaluates the final retained
text with the versioned `waldo/email-address-v1` detector and writes the result
without redacting or otherwise changing the text.

Detector identity and flagged-record counts are preserved in Parquet footer
metadata, the embedded shard BOM, each manifest shard, and the manifest
aggregate. The flag means only that an email-shaped string was detected; it is
not a determination that the string identifies a natural person or that any
processing is lawful or unlawful.

Model composes may exclude flagged rows with:

```yaml
filter:
  exclude:
    email_addresses: true
    licenses: [CC-BY-NC-*]
```

Any matching exclusion rejects the row. The established license-filter syntax
remains readable but cannot be mixed with the unified license exclusion in one
filter.

## Compatibility

Existing schema-1 shards remain valid, readable, and unmodified. New ingestion
emits schema 2. A filter on `email_addresses` fails closed when its selection
contains schema-1 shards because absence of the column means unassessed, not
false. Only affected corpora need an explicit rebuild; the index may contain
both schema versions.
