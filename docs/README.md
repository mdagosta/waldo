# WALDO documentation

The root README is a project overview. Maintained documentation lives here.

## Start here

- [Vision](VISION.md): purpose, users, guarantees, and non-goals.
- [Command guide](UX.md): the current CLI and common workflows.
- [Architecture](ARCHITECTURE.md): package boundaries and data flow.
- [Contributing](CONTRIBUTING.md): development and contribution process.
- [Testing](TESTING.md): local, end-to-end, and live test suites.
- [Roadmap](ROADMAP.md): current release status and remaining work.
- [Open-source release plan](RELEASING.md): gates for the first public release.

## Data contracts

- [Ingestion and canonical Parquet](INGESTION-DESIGN.md)
- [Source directory contract](SOURCE-DIRECTORY.md)
- [Fetcher and ingest-recipe contract](FETCHER-CONTRACT.md)
- [Corpus OpenWALDO BOM](OPENWALDO-BOM.md)
- [Index compatibility](COMPATIBILITY.md)

## Model contracts

- [Model lifecycle](MODEL-LIFECYCLE.md)
- [Training and calibration](TRAINING-AND-CALIBRATION.md)
- [Model exports](MODEL-EXPORTS.md)
- [EU GPAI disclosure mapping](EU-GPAI-DISCLOSURE.md)

## Design history

[Architectural decision records](adr/README.md) explain durable decisions and
their rationale. They are historical context, not a substitute for current
command help or user documentation.
