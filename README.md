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
hash-verifying lookaside scratch with ordered fallback mirrors; inspect failed-run leftovers;
and export selected shards as native objects or canonical JSONL with an
`EXPORT.json` provenance record. It can also probe text/Markdown/raw Parquet,
stream plain, gzip, and zstd JSONL, write canonical schema-1 Parquet, and
publish shards to S3 with bounded concurrency
and remote checksum verification, reclaim staging safely, and create a Git
review overlay. Direct local inputs and strict external ingest composes converge
on that same backend; composed fetcher scripts populate temporary source space,
are pinned in manifest evidence, and are purged after successful contribution.
A filesystem-backed publisher exercises the same path locally for integration
tests. Phase 4 now includes strict model recipes, verified
corpus inputs, immutable build plans, durable model/run OpenWALDO BOMs, and a
fake backend that proves complete/failure/interruption orchestration without
claiming to train real weights. A fail-closed EU GPAI export maps the model and
its training BOMs to the pinned Commission template fields and reports missing
facts without claiming legal compliance. Other commands remain visible as an
honest roadmap.

Recursive `waldo index verify` checks index structure plus the reachability and
declared size of every canonical object using header-only requests. Use
`--offline` for metadata alone or `--objects` to download and hash every byte.

The stable corpus contract is documented in
[the OpenWALDO BOM guide](docs/OPENWALDO-BOM.md). Corpus ingestion and its
training-data shape are documented in
[the ingestion design](docs/INGESTION-DESIGN.md).
The framework-neutral model formats and fake-backend boundary are documented
in [the model lifecycle guide](docs/MODEL-LIFECYCLE.md).
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
- Fetchers are external shell scripts maintained in a separate repository.
  WALDO consumes local input directly or executes only scripts explicitly named
  by a supplied ingest compose; canonical conversion remains inside WALDO.

## Development

```bash
go test ./...
go run ./cmd/waldo --help
./scripts/e2e/ingest-smoke.sh local
./scripts/e2e/ingest-smoke.sh local compose
```

Start with [VISION.md](VISION.md), then read [the UX contract](docs/UX.md),
[architecture](docs/ARCHITECTURE.md), and
[compatibility policy](docs/COMPATIBILITY.md). Current sequencing and exit
criteria are in [the roadmap](docs/ROADMAP.md).
