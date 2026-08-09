# 0040: Make pre-training shard audit explicit

Status: accepted

## Decision

Direct and compose-driven `waldo model train` always materialize selected
objects through the lookaside cache, enforcing the manifest size and SHA-256.
They do not additionally audit shard structure, embedded attestations, or
declared aggregate totals by default.

Passing `--audit` performs that additional verification before training. An
audited run BOM includes verified embedded attestation evidence or an explicit
legacy/deep-validation status. A default run BOM remains honest by omitting
attestation evidence it did not verify.

Training still reads canonical records through the normal strict shard reader;
the option controls the separate pre-training audit pass, not object identity
verification or runtime parsing.

## Consequences

Starting a normal training run no longer includes an unrequested extra audit
pass. Operators who require preflight attestation evidence can request it with
one consistent flag on direct and compose-driven training.
