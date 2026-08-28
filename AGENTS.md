# WALDO agent guide

These rules apply to the entire repository.

## Find the right document

Start at [`docs/README.md`](docs/README.md). It identifies the authoritative
contract for each area.

- User workflows belong in the separate `openwaldo/docs` quickstarts.
- Current CLI behavior comes from `waldo <command> --help`.
- Programmatic contracts and developer requirements live under `docs/`.
- ADRs explain past decisions. They are not current instructions when a
  maintained contract says otherwise.

For cross-domain changes, read `docs/VISION.md`, `docs/ARCHITECTURE.md`, and
`docs/COMPATIBILITY.md` first. Record new durable decisions under `docs/adr/`.

## Ingestion boundary

The canonical ingestion input is a manifest-backed raw directory. Its root
`manifest.json` owns corpus facts, source facts, input mapping, and raw-tree
evidence. Acquisition tools create that directory and stop. WALDO never runs a
fetcher or converter named by a manifest.

Read these in order before changing ingestion:

1. `docs/INGESTION.md`
2. `docs/INGESTION-MANIFEST.md`
3. `docs/INGESTION-DESIGN.md`

Do not introduce new ingest recipes or external conversion pipelines. New
formats require a reviewed built-in adapter and an explicit documented status.

## Architecture rules

- Use **lookaside**, never `store`, for the content-addressed object domain.
- An **OpenWALDO BOM** is the immutable handoff from corpus selection to model
  workflows.
- A model **compose** belongs only to model training and lifecycle management.
- Data packages must not import model or training packages.
- Model and training code consumes resolved corpus BOMs; it does not traverse
  or reinterpret the index independently.
- One fact has one authoritative type and owner.
- Planned behavior must be labeled as planned and must not be presented as a
  working command or format.

## Before handoff

Run:

```
gofmt -w .
./testing/all.sh
```
