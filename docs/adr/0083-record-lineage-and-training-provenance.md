# ADR 0083: Record model lineage and training provenance as content-addressed nodes

Status: proposed

## Context

WALDO records a model's own training runs and a single parent pin or origin,
so one model traces back to its data. Weights that pass through several
models, such as a fine-tune of a fine-tune, a merge of fine-tunes, or rounds of
distributed training that merge and restart every round, lose that trail: a
merge has no runs, a pulled release keeps only a hash of its source model BOM,
and the run BOMs that pin the training data stay on the trainer's disk. The
EU disclosure and the distributable licence review therefore cover only a
model's own runs. Copying every ancestor's facts forward instead would make
each record grow with the length of its history.

## Decision

Every model has a lineage node: the SHA-256 of its current weights, its direct
parents (each by weights hash and by the hash of the parent's own node), and
the run BOMs that produced its weights from those parents. Like a git commit,
a node names only its direct parents, so its size does not depend on the
history behind it. A fine-tune's parent pin records the parent's node hash; a
merged origin records its own node; a release's `BOM.json` carries the
released model's node; a pull keeps the release's node in the origin.

Releases publish the evidence beside the weights. A package includes the run
BOMs behind its weights as `provenance-<sha256>.json`, named by the hash of the
run BOM's canonical JSON, which is how models already pin runs: a trained
model's own runs, a merged model's parents' runs, or a pulled release's runs.
Older runs stay published by the releases they came from. A package also
includes `TRAINING-CLOSURE.json`: every distinct corpus BOM the lineage trained
on, with use counts, and whether that history is complete. It is keyed by
corpus BOM, so it grows with the corpora a lineage uses rather than its length.
Pulls verify and keep both with the origin, checking each run BOM against the
hash it is named by and refusing a closure that is malformed or leaves out the
release's own runs; merges keep their parents' run BOMs and the union of their
closures.

Disclosure and attribution cover the whole lineage. `EU-BOM.json` adds the
inherited corpora and reports unrecorded history as a gap.
`ATTRIBUTION.md` and its distributable review take the inherited corpus BOMs
too, so a non-distributable corpus anywhere in a lineage blocks a distributable
release. `waldo model gate` scans every corpus in the closure for contamination,
and refuses a model whose history is incomplete.

Run BOMs record the Python runtime by executable name and version only, never
an absolute local path, because they are now published.

## Consequences

- Any released model answers which data trained it, and under what licences,
  from published artifacts: its weights, its run BOMs, their corpus BOMs, and
  the index commit and shards they pin.
- A merge's package proves its round on its own, so contributors' submission
  packages can be pruned after a round closes.
- Records stay bounded: a node names direct parents, a package republishes one
  generation of run BOMs, and a closure grows only with distinct corpora.
- Parent pins recorded before lineage nodes keep matching; their history ends
  at the immediate parent and is reported as incomplete where it matters.
- Declared lineage from an unsigned release is recorded as the release declared
  it. Verifying it against signed releases is future work.

Amends [0026](0026-pinned-model-origin.md), [0066](0066-pin-trained-model-parents.md),
and [0022](0022-model-release-exports.md); relates to
[0011](0011-model-plan-and-boms.md) and
[0074](0074-fail-closed-distributable-corpus-gate.md).
