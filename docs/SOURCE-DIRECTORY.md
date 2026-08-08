# Source directory contract

A source directory is a recursive collection of raw inputs for one declared
source. WALDO converts its records to canonical Parquet; the directory itself
contains no WALDO-specific manifest.

## Directory rules

- Every entry below the root is examined in stable path order.
- Files must be regular files. Symlinks and special files reject the source.
- Every non-empty file must use a supported container. A source has at most one
  input profile, and it must apply to every file in that source.
- Unknown, binary, HTML, WARC, archive, and unsupported compressed files reject
  the source. Archives must be safely extracted by acquisition first.
- Files must not change after probing. WALDO pins their paths, sizes, and
  SHA-256 identities before conversion.
- Do not add metadata sidecars: WALDO will treat them as inputs. Put shared
  source facts in the CLI/recipe declaration and per-record facts in the
  records themselves.

Recipe acquisition may leave empty regular files; WALDO ignores them. Each
declared source must still produce at least one supported non-empty input.

## Ingestible containers

| Container | Physical record | Without an input profile |
| --- | --- | --- |
| UTF-8 text or Markdown | one file | the complete file is `text` |
| `.json` | one top-level object; arrays rejected | profile required |
| `.jsonl` | one object per nonblank line | top-level string `text` required |
| `.jsonl.gz`, `.jsonl.zst` | streamed; one object per nonblank line | top-level string `text` required |
| Parquet | one row | one flat, non-null string column named `text`, `content`, or `document`, or an explicit `text_column` |
| XML | one file | `xml-record` profile required |

Text must be valid UTF-8 and NUL-free. One logical record is limited to 64 MiB
by default; a reviewed recipe may set `record_maximum_bytes` from 16 MiB through
256 MiB. WALDO never silently splits a file, JSON value, line, or Parquet row.

Profiles change only how physical records become canonical text:

- `record-map` and `dialogue-pair`: JSON, JSONL, compressed JSONL, or Parquet.
- `ranked-conversation-tree`: JSON or JSONL, including compressed JSONL.
- `bounded-text`: UTF-8 text files.
- `xml-record`: XML files.

## Recipe application

Schema 1 has one source directory. All steps share its `WALDO_FETCH_DIR`, and
the recipe's source metadata, license, and input profile apply to every record.

Schema 2 has one private source directory per `sources[]` entry. That entry's
steps receive its directory as `WALDO_FETCH_DIR`; its metadata, license, and
profile apply only to records beneath that directory. WALDO may pack records
from several source directories into the same size-bounded Parquet shard while
preserving source path, source identity, and license on every row.
