# Fetcher contract

Fetchers acquire raw upstream material and stop. They do not invoke WALDO,
upload shards, mutate an index, schedule work, or train models.

The maintained fetcher is a separate Go program driven by reviewed INI files:

```sh
fetcher corpora/example.ini /path/to/handoff
waldo index ingest /path/to/handoff core/example
```

The definitive handoff format is [SOURCE-DIRECTORY.md](SOURCE-DIRECTORY.md).

## Fetcher responsibilities

- Validate the complete INI file before network access.
- Check approximate free space, including acquisition/ingestion headroom.
- Use pinned checksums or immutable revisions when available.
- Resume protocols that safely support it; preserve partial state on failure.
- Retain general raw formats without corpus-specific text or conversation
  rendering.
- Declare each source's physical format and logical mapping in the INI, copy
  them into its manifest, and validate fetched files before publication.
- Preserve fetched data and emit a source/file/mapping-specific error when
  post-fetch validation fails.
- Safely unpack general archive containers only when WALDO cannot read them.
- Record corpus identity, source URL/category, effective license, upstream
  license evidence, content selection, declared human languages, separately
  declared programming languages, and known provenance.
- Keep different sources or licenses in separate manifest boundaries.
- Reject unsafe paths, symlinks, special files, and silent output conflicts.
- Write compact manifests last, after raw-tree hashing succeeds.
- Never write secrets into configurations or manifests.

## WALDO responsibilities

- Recursively discover only the files inside declared manifest boundaries.
- Verify raw-tree evidence and pin every input's size and SHA-256.
- Detect general physical formats, including compressed JSONL and mbox, and
  reject disagreement with the source manifest's declared format.
- Apply declarative logical mappings from the source manifest.
- Reject unsupported or unmapped raw formats instead of creating fallback
  training records.
- Preserve source identity and effective/per-record licenses.
- Apply the pinned privacy-redaction and content-assessment policies.
- Deduplicate, tokenize, partition, and write canonical Parquet.
- Audit and publish lookaside objects.
- Generate compact index manifests containing exact retained document, token,
  byte, license, source, assessment, and redaction measures.
- Produce an index contribution; never infer the index destination from the
  fetch configuration.

## Acquisition boundary

The fetcher runs with the invoking user's permissions; it is not an operating
system sandbox. It may write only under the supplied handoff directory. Network
credentials are supplied through the local environment or normal credential
files and are never persisted in corpus metadata.

A completed handoff is immutable input. Updating an upstream corpus is a new
explicit acquisition snapshot; incremental/delta acquisition may reuse safely
verified partial state, but must produce fresh raw-tree evidence before WALDO
accepts it.

Legacy WALDO ingest recipes and the legacy `waldo-source-directory/raw` layout
remain readable for compatibility. They are not the format for new fetchers.
