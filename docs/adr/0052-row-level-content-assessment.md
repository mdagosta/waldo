# 0052: Preserve general content assessments as immutable row facts

## Status

Accepted.

## Decision

Canonical Parquet record schema 2 adds the required booleans
`email_addresses`, `repetitive_content`, and `boilerplate_content`. During every
ingestion WALDO evaluates the final retained text with three pinned detectors
and writes the results without redacting or otherwise changing the text.

`waldo/email-address-v1` identifies common email-shaped strings.
`waldo/gopher-ngram-repetition-v1` applies pinned repeated-token n-gram
thresholds. `waldo/gopher-structural-duplication-v1` applies pinned duplicate
line and paragraph thresholds. The latter two are deterministic,
language-neutral adaptations of the published Gopher quality rules. Exact
thresholds and normalization are part of the detector contract documented in
the source-directory specification.

Detector identities and flagged-record counts are preserved in Parquet footer
metadata, the embedded shard BOM, each manifest shard, the manifest aggregate,
and OpenWALDO BOMs. These flags are observations, not determinations that text
is personal data, unlawful, unsafe, or unsuitable for every training purpose.
The physical writer identity is
`parquet-go/0.30.1/zstd-6/page-1m/rg-64m/v7-content-assessment`.

Model composes may exclude any matching row with:

```yaml
filter:
  exclude:
    email_addresses: true
    repetitive_content: true
    boilerplate_content: true
    licenses: [CC-BY-NC-*]
```

Conditions are ORed. The established license-filter syntax remains readable
but cannot be mixed with the unified license exclusion in one filter.

## Compatibility

This expands the unreleased schema-2 contract in place rather than introducing
schema 3. No production schema-2 objects existed when the decision was made.
Existing schema-1 shards remain valid, readable, and unmodified. New ingestion
emits the expanded schema 2. Any content-assessment filter fails closed when
its selection contains schema-1 shards because absence means unassessed, not
false. Only affected corpora need an explicit rebuild; an index may contain
both schema versions.
