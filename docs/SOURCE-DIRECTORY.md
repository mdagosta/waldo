# Source directory contract

A completed fetcher handoff is a corpus directory. WALDO ingests it with an
explicit index destination:

```sh
waldo index ingest /path/to/handoff core/example
```

The fetcher owns acquisition and shared source facts. WALDO owns format
detection, logical mapping, redaction, canonical Parquet, token/document
counts, lookaside publication, and index contribution generation.

## One source

A single-source corpus places `manifest.json` and raw files in the same
directory. Raw files may be nested arbitrarily.

```text
handoff/
├── manifest.json
├── archive.jsonl.gz
└── nested/document.txt
```

The schema-1 manifest is:

```json
{
  "kind": "waldo-corpus-directory",
  "schema": 1,
  "corpus": {
    "id": "example",
    "title": "Example",
    "description": "Example training material."
  },
  "source": {
    "id": "example",
    "license": "CC-BY-4.0",
    "source": {
      "name": "Example upstream",
      "url": "https://example.org/data",
      "category": "public-dataset",
      "license_evidence": {
        "declaration": "Creative Commons Attribution 4.0",
        "url": "https://example.org/license"
      }
    },
    "input": {}
  },
  "fetcher": {
    "name": "waldo-fetcher-1",
    "retrieved_at": "2026-08-19T00:00:00Z"
  },
  "raw": {
    "file_count": 2,
    "byte_count": 1234,
    "tree_sha256": "64-lowercase-hex-characters"
  }
}
```

The source ID may be omitted; WALDO then uses the corpus ID. `input` is omitted
when the raw format has an automatic reader.

## Multiple sources

Different sources or effective licenses use separate child directories. The
root manifest lists every allowed child explicitly:

```text
handoff/
├── manifest.json
├── source-one/
│   ├── manifest.json
│   └── records.jsonl.gz
└── source-two/
    ├── manifest.json
    └── messages.mbox.gz
```

Root manifest:

```json
{
  "kind": "waldo-corpus-directory",
  "schema": 1,
  "corpus": {
    "id": "example-suite",
    "title": "Example Suite",
    "description": "Two independently sourced collections."
  },
  "sources": ["source-one", "source-two"]
}
```

Each child manifest has this shape:

```json
{
  "kind": "waldo-source-directory",
  "schema": 1,
  "source": {
    "id": "source-one",
    "license": "CC0-1.0",
    "source": {
      "name": "Source one",
      "url": "https://example.org/one",
      "category": "public-dataset",
      "license_evidence": {"declaration": "CC0 1.0"}
    },
    "input": {
      "type": "record-map",
      "fields": {"text": ["text"]}
    }
  },
  "fetcher": {"name": "waldo-fetcher-1"},
  "raw": {
    "file_count": 1,
    "byte_count": 100,
    "tree_sha256": "64-lowercase-hex-characters"
  }
}
```

The child directory name and source ID must match. Undeclared root entries,
symlinks, special files, and nested manifests are rejected.

## Raw-tree evidence

For every regular raw file, calculate its SHA-256 and byte count. Sort entries
by slash-separated relative path, then hash the UTF-8 inventory:

```text
FILE_SHA256<TAB>BYTES<TAB>RELATIVE_PATH<LF>
```

`file_count`, `byte_count`, and `tree_sha256` describe only raw files; the
boundary's `manifest.json` is excluded. WALDO independently probes each exact
file and rejects a mismatch.

## General input formats

WALDO detects the physical format independently of filenames:

| Format | Physical record |
| --- | --- |
| UTF-8 text or Markdown | one file |
| mbox, plain/gzip/zstd | one RFC 822 message |
| JSON | one top-level object |
| JSONL, plain/gzip/zstd | one object per nonblank line |
| Parquet | one row |
| XML | one file |

JSON/JSONL/Parquet mappings use `record-map`, `dialogue-pair`,
`chat-messages`, or `ranked-conversation-tree`. Whole-file text may use
`bounded-text`; XML uses `xml-record`. Fetchers retain general raw upstream
formats and do not render corpus-specific conversation templates.

Text must be NUL-free UTF-8. Archives that WALDO does not read directly must be
safely unpacked by acquisition. Empty files are not trainable records. WALDO
pins file identity before conversion and rejects files that change afterward.
Every non-empty file must resolve to one of the supported adapters above.
Structured JSON, JSONL, Parquet, and XML must carry the required logical input
mapping. WALDO fails before conversion on unsupported, ambiguous, or unmapped
formats; it does not silently convert raw markup to text or binary bytes to
base64 training records.

Before hashing, deduplication, measurement, and packing, WALDO applies its
pinned privacy-redaction policy. Canonical shard and manifest statistics—not
the handoff manifest—are authoritative for retained documents and tokens.

During ingestion, the verified raw `file_count` and `byte_count` provide the
known progress totals. WALDO reports completed files/bytes plus live retained
document and token counts as canonical batches are processed. Exact final
counts are persisted in the generated index manifest; fetcher manifests never
guess token or retained-document totals.

The earlier `waldo-source-directory` root containing a `raw/` directory remains
readable for compatibility. New fetchers must produce the corpus-directory
layout above.
