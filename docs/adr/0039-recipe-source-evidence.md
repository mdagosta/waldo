# 0039: Preserve source evidence in ingest recipes

Status: superseded by [0064](0064-manifest-backed-ingestion.md)

## Decision

Recipe schemas 1 and 2 use the same corpus-neutral source evidence carried by
index manifests. `content` records content types, languages, characteristics,
the underlying content period, and the declared selection rule. `acquisition`
records general basis and category-specific crawler, domain, user-data, or
synthetic evidence.

`collected_from` and `collected_to` retain their existing meaning as the
acquisition period; they are not duplicated inside `acquisition`.
`content.from` and `content.to` are the distinct underlying content period.

`source.license` remains WALDO's normalized effective/default license. A new
`license_evidence` object independently preserves the upstream declaration
and/or an absolute evidence URL. Per-record raw declarations remain in
canonical `license_raw`.

These facts are part of the immutable plan and aggregate source acquisition
identity and pass unchanged into compact `index.Source` records and OpenWALDO
BOM source pins. Selection arguments may control a fetcher, but
`content.selection` is the durable human-auditable declaration.

Unknown source categories fail closed. Commercially licensed and private
third-party sources require an acquisition basis; web crawls require crawler
and acquired-domain evidence; user data requires service and interaction; and
synthetic data requires generator identity.

## Consequences

Source directories still contain only ingestible primitive files. Evidence is
not communicated through sidecars, and no corpus-specific adapters or fields
are introduced. Recipe and manifest size scales with sources, not files or
records.
