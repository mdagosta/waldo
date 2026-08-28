# ADR 0030: Pin a bounded deterministic held-out set

Status: accepted

## Decision

Training selects held-out records by the lowest seeded SHA-256 scores over
canonical shard and row identities. The default is one percent of records,
capped at 256 records and 1 MiB of source content while leaving at least one
training record.

The run BOM pins the selection algorithm, seed, record count, selected-model
token targets, content bytes, and digest of the ordered identities. Selected
records are excluded from every training epoch.

Evaluation uses the model's configured tokenizer and per-record EOS packing.
The worker records no-gradient held-out loss and perplexity at the resolved
cadence.
