# Ingestion end-to-end smoke test

The smoke test builds the current WALDO source and works entirely in a fresh
temporary directory. It initializes an empty index, configures isolated
machine state, dry-runs and executes a two-document ingestion, applies the
generated contribution overlay to that disposable index, recursively verifies
the new corpus, exports canonical JSONL, verifies its OpenWALDO BOM, and checks
that successful staging and scratch objects were purged.

Run the filesystem-backed path with no credentials or network access:

```bash
./scripts/e2e/ingest-smoke.sh local
```

Set `WALDO_E2E_KEEP=1` to retain the temporary workspace for inspection.

The S3 path intentionally has two guards. Its URL must contain an explicit
`waldo-e2e` prefix, and the caller must confirm both the write and public-read
requirements:

```bash
WALDO_E2E_ALLOW_S3=1 \
WALDO_E2E_S3_PUBLIC=1 \
WALDO_E2E_AWS_REGION=us-west-2 \
./scripts/e2e/ingest-smoke.sh s3://example-test-bucket/waldo-e2e
```

AWS credentials come from the standard credential chain and are never written
to WALDO configuration. The selected prefix must be publicly readable because
canonical WALDO S3 reads are anonymous. The script never deletes remote S3
objects; use a lifecycle policy on the disposable prefix.
