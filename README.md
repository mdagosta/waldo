# WALDO

WALDO is a single command-line tool for building, inspecting, and consuming an
auditable commons of AI training data. It connects a Git-governed metadata
index to content-addressed lookaside objects and carries the resulting corpus
provenance into model artifacts.

This repository is a clean-slate implementation. It preserves compatibility
with the existing `waldo-index` where that compatibility is part of the public
data contract; it does not preserve the old backend's internal structure or
command organization by default.

The project has completed its contracts and read-only index foundation and is
now in **Phase 2: verified OpenWALDO BOMs**. The binary can list, show,
summarize, and verify the public `waldo-index`; materialize objects through a
hash-verifying lookaside cache; scrub that cache; and export native selected
shards with an `EXPORT.json` provenance record. Other commands remain visible
as an honest roadmap and report that they are not implemented yet.

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
