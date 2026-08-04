# WALDO

WALDO is a single command-line tool for building, inspecting, and consuming an
auditable commons of AI training data. It connects a Git-governed metadata
index to content-addressed lookaside objects and carries the resulting corpus
provenance into model artifacts.

This repository is a clean-slate implementation. It preserves compatibility
with the existing `waldo-index` where that compatibility is part of the public
data contract; it does not preserve the old backend's internal structure or
command organization by default.

The project has completed its contracts, read-only index foundation, and
**Phase 2: verified OpenWALDO BOMs**, and now has the first end-to-end corpus
contribution path. The binary can list, show,
summarize, and verify the public `waldo-index`; materialize objects through a
hash-verifying lookaside cache with ordered fallback mirrors; scrub that cache;
and export selected shards as native objects or canonical JSONL with an
`EXPORT.json` provenance record. It can also probe text/Markdown/raw Parquet,
write canonical schema-1 Parquet, publish shards to S3 with bounded concurrency
and remote checksum verification, reclaim staging safely, and create a Git
review overlay. A filesystem-backed publisher exercises the same path locally
for integration tests. Other commands remain visible as an honest roadmap.

The stable corpus contract is documented in
[the OpenWALDO BOM guide](docs/OPENWALDO-BOM.md). The next phase is corpus
contribution; its proposed ingestion and training-data shape is documented in
[the ingestion design](docs/INGESTION-DESIGN.md) for review before
implementation.
The accompanying [EU GPAI disclosure mapping](docs/EU-GPAI-DISCLOSURE.md)
defines the provenance needed to render the Commission's public training-content
template from later model BOMs.

## Principles

- One binary, with clear one-way dependencies between its domains.
- The index defines corpus meaning; lookaside storage only holds bytes.
- Model tooling consumes an immutable, verified OpenWALDO BOM. It does not
  reinterpret index manifests or licenses.
- Provenance records observed facts and states the limits of its guarantees.
- The common path should be obvious; advanced maintenance should remain out of
  the way until it is needed.
- Fetchers are external producers. A future fetcher repository will implement
  the acquisition side of the documented handoff contract.

## Development

```bash
go test ./...
go run ./cmd/waldo --help
```

Start with [VISION.md](VISION.md), then read [the UX contract](docs/UX.md),
[architecture](docs/ARCHITECTURE.md), and
[compatibility policy](docs/COMPATIBILITY.md). Current sequencing and exit
criteria are in [the roadmap](docs/ROADMAP.md).
