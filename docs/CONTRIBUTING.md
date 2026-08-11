# Contributing

WALDO is being opened early so the community can participate while its
interfaces, workflows, and governance are still taking shape. Early feedback,
testing, documentation improvements, and focused code contributions are
welcome.

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

## Proposing a change

Use focused commits with clear messages. Open an issue before a large or
cross-domain change so the intended contract can be discussed first.

The project has not finalized a DCO/CLA or governance policy. Those choices
will be made explicitly with the project owner; contributors should not infer
additional legal terms from this development guide.
