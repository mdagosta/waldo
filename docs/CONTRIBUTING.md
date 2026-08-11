# Contributing

This guide describes the contribution workflow while the first supported
public release is prepared.

## Before changing code

Read `AGENTS.md` and the relevant contract under `docs/`. Architectural or
durable format changes require an ADR in `docs/adr/`.

Keep changes small and include tests for observable behavior. Do not add
source-specific fetchers to this repository.

## Local checks

```bash
gofmt -w .
./testing/all.sh
```

Hardware-dependent tests skip when their runtime or accelerator is unavailable.
Networked and S3 tests require explicit opt-in; see [Testing](TESTING.md).

## Commits

Use focused commits with clear messages. Corpus-index contributions and future
code contributions are expected to use Developer Certificate of Origin (DCO)
sign-off:

```bash
git commit --signoff
```

The project must publish its final contribution policy, code of conduct, and
security reporting channel before accepting general contributions.
