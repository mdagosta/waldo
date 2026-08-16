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

During canonical assembly WALDO automatically assesses, but does not alter,
every row for common email-shaped strings, repetitive token sequences, and
duplicated structural boilerplate. This applies uniformly to every input type,
including books, mailing lists, and source code. The booleans are stored in
record schema 2; acquisition scripts must not add assessment sidecars.

Recipe acquisition may leave empty regular files; WALDO ignores them. Each
declared source must still produce at least one supported non-empty input.

## Shared source evidence

Shared facts live in the CLI or recipe, never in directory sidecars:

- `license` is WALDO's normalized effective/default license;
  `source.license_evidence` preserves the upstream declaration and/or URL.
- `source.collected_from` and `source.collected_to` are the acquisition period.
- `source.content.from` and `source.content.to` are the underlying content
  period; `source.content.selection` states any subset rule.
- `source.content` also carries types, languages, geography/demography, and
  tri-state content characteristics. `source.acquisition` carries general or
  category-specific acquisition evidence.

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

Structured record profiles may classify primary material with one or more
exact scalar conditions:

```yaml
input:
  main_content:
    metadata.namespace: 0
```

Rows matching every declared condition receive `main_content: true`; any other
value produces `false`, and a missing declared field rejects ingestion as
source-schema drift. When the mapping is omitted, every retained row is main
content. Older canonical schemas also read as `main_content: true`.

## Recipe application

Schema 1 has one source directory. All steps share its `WALDO_FETCH_DIR`, and
the recipe's source metadata, license, and input profile apply to every record.

Schema 2 has one private source directory per `sources[]` entry. That entry's
steps receive its directory as `WALDO_FETCH_DIR`; its metadata, license, and
profile apply only to records beneath that directory. WALDO may pack records
from several source directories into the same size-bounded Parquet shard while
preserving source path, source identity, and license on every row.

## Automatic privacy redaction and row assessment

Before canonical identity is calculated, WALDO applies
`waldo/privacy-redaction-v1` to every record from every source. It retains names
and public attribution, replaces email addresses, IP addresses, phone numbers,
and high-confidence credentials with typed placeholders, and removes routing
headers from recognized RFC 822 message blocks. The redacted text is then
hashed, deduplicated, measured, assessed, and packed. Raw values never enter a
canonical shard.

Schema-2 rows carry replacement/removal counts in
`redacted_email_addresses`, `redacted_ip_addresses`,
`redacted_phone_numbers`, `removed_mail_routing_headers`, and
`redacted_credentials`. Footer, shard BOM, manifest, and OpenWALDO BOM evidence
pins the policy and aggregates those counts. Existing v8/v7 schema-2 and
schema-1 shards remain readable but have no redaction guarantee.

Every newly ingested row receives three required booleans:

| Column | Detector | Meaning |
| --- | --- | --- |
| `email_addresses` | `waldo/email-address-v1` | A common Internet email-shaped string remains after redaction; current ingestion rejects this condition. |
| `repetitive_content` | `waldo/gopher-ngram-repetition-v1` | Repeated token n-grams exceeded a pinned within-document threshold. |
| `boilerplate_content` | `waldo/gopher-structural-duplication-v1` | Duplicate lines or paragraphs exceeded a pinned within-document threshold. |

The repetition rules are a deterministic, language-neutral adaptation of the
[Gopher quality filters](https://arxiv.org/abs/2112.11446). Documents with fewer than 50 alphanumeric tokens are
not marked repetitive. Longer documents are marked when the most frequent
trigram covers more than 18% of tokens or non-overlapping duplicate 8-grams
cover more than 12%. At least four non-empty lines or paragraphs are required
for structural assessment; a document is marked as boilerplate when duplicates
exceed 30% of those elements or 20% of the source bytes. Whitespace is
normalized for structural comparison and tokens are Unicode letters/numbers
lowercased for n-gram comparison.

Assessment does not make a legal, safety, or overall-quality determination.
Manifests preserve detector identities and aggregate flagged-row
counts. Existing schema-1 shards remain readable but unassessed and are upgraded
only through an explicit corpus rebuild.
