# 0061: Stream record arrays in the JSON container

Status: accepted

## Decision

The general `json` container accepts either one record object or one top-level
array of record objects. Array elements are decoded and mapped one at a time;
the array is not loaded into memory as one value. JSONL remains the preferred
streaming format when upstream offers it.

`chat-messages` may prepend a separately mapped `system` field, and
`dialogue-pair` may preserve a mapped `tools` value. These are general record
mappings and do not recognize particular corpora or rewrite upstream files.

## Consequences

Original dataset artifacts such as ToolACE and xLAM can be retained without a
fetch-time conversion. Fetcher validation and WALDO ingestion both reject
non-object array elements, empty arrays, malformed trailing data, and mappings
that do not match sampled records.
