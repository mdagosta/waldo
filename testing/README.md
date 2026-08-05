# WALDO testing

This directory contains tests that exercise the built command across package
and process boundaries. Focused Go unit tests stay beside the packages they
cover; reusable system fixtures, executable integration tests, and end-to-end
lifecycles belong here.

## Run the local suite

Every test category has its own executable script:

```bash
./testing/unit.sh
./testing/vet.sh
./testing/e2e/ingest-direct.sh
./testing/e2e/ingest-recipe.sh
```

Run all of them in that order with:

```bash
./testing/all.sh
```

## Ingestion end to end

The two public ingest scripts run a shared private lifecycle harness and build
the current WALDO source entirely in a fresh temporary directory. They initialize an index, configure isolated machine
state, generates UTF-8 and multiline input plus a duplicate, ingests it,
applies the review overlay, recursively verifies the corpus, exports canonical
JSONL, verifies its OpenWALDO BOM, compares retained records with their source
bytes, performs a full index audit, exercises shard summary/audit/list/export
against local objects, and confirms that successful staging and scratch data
were purged.

The recipe mode additionally verifies:

- strict `waldo-ingest-recipe` recognition and JSON preflight shape;
- rejection and non-execution of the retired `waldo-ingest-compose` identity;
- explicit fetcher paths and bare command resolution through `PATH`;
- `WALDO_FETCH_DIR`, `WALDO_INGEST_RECIPE`, and the recipe working directory;
- cleanup of the WALDO-owned recipe preparation workspace.

Run either lifecycle directly:

```bash
./testing/e2e/ingest-direct.sh
./testing/e2e/ingest-recipe.sh
```

Set `WALDO_E2E_KEEP=1` to retain the temporary workspace for inspection.

## Guarded S3 E2E

The S3 path is opt-in, requires an explicit `waldo-e2e` prefix, and never
deletes remote objects:

```bash
WALDO_E2E_ALLOW_S3=1 \
WALDO_E2E_S3_PUBLIC=1 \
WALDO_E2E_AWS_REGION=us-west-2 \
./testing/e2e/ingest-recipe.sh s3://example-test-bucket/waldo-e2e
```

Use a lifecycle policy on the disposable prefix. Credentials come from the
AWS SDK environment/workload chain or a prior interactive
`waldo lookaside login`; they are never written into test fixtures.

## Intended growth

Future cross-cutting suites should be grouped beneath this directory rather
than added to production packages:

```text
testing/
  all.sh        convenience aggregator only
  unit.sh       Go package tests
  vet.sh        Go static analysis
  e2e/          complete user journeys using the built CLI
  integration/  multi-package or external-service contract tests
  fixtures/     small deterministic shared inputs
  live/         explicitly opted-in network or capacity checks
```

Shard corruption and schema-invalid fixtures belong in focused package tests;
complete shard and index audit journeys belong in `testing/e2e`.
