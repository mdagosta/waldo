# ADR 0085: Merge models of one family into a merged origin

Status: accepted
- Date: 2026-11-05

## Context

Averaging the weights of models fine-tuned from common weights is an
established way to combine them. Merges made outside WALDO discard provenance.
A merged model also fits none of WALDO's existing model kinds: it has no
training run, and ADR 0026 forbids recording a synthetic one.

## Decision

A compose base may be `merge`: two or more managed models, each with an
optional weight given for every parent or none. WALDO resolves each parent's
current weights and requires identical architectures and the byte tokenizer.

- **Merge-base.** WALDO walks each parent's history (ADR 0084) by weights hash,
  as git walks commits, and finds the merge-base: the common ancestors no other
  common ancestor descends from. There may be more than one.
- **One family.** Every step from each parent down to the merge-base must be in
  its history, with no gap and no end, and every earlier merge above it must
  have parents that reach it. Below the merge-base, gaps are recorded, not
  refused. There is no override.
- **`MERGE.json`.** The merge's one source document (`waldo-model-merge`,
  schema 1): the versioned method (`mean-f32-v1` or `weighted-mean-f32-v1`) and
  each parent by the run or origin BOM that made its weights, its weights hash,
  license, and weight, sorted by weights hash. It names no models or places.
- **Merged origin.** WALDO computes the mean in float32 in that order, rounds
  to nearest-even bfloat16 or float32, and writes an origin under
  `<model.root>/.merges/<hash of MERGE.json>`: provider `merge`, revision that
  hash, `MERGE.json` as its source artifact. Its `LINEAGE.json` holds
  `MERGE.json` and every parent's history, so the parents can be deleted.
- A compose with `base.merge` may have no stages: the destination is then the
  merged model, servable, exportable, and usable as another compose's base;
  with stages, the merge initializes training like any origin. `model train`
  needs no training backend for a merge-only compose.

## Consequences

- No record changes: earlier WALDO versions read merged models as origins.
- Everyone who merges the same parents writes the same `MERGE.json` and the
  same weights, so independent merges can be compared byte for byte.
- A new merge is always computed from its parents' weights. Earlier merges in
  a history are checked by their documents; their weights aren't kept.
- A merge never invents a training run, and changing the arithmetic requires
  a new method name.
- Parents trained from random weights, or pulled without history, cannot be
  merged, except copies of the same weights.
- A merge-based model keeps the merge it was built from under `.merges`; with
  that entry gone it can no longer be composed again or extended.

Relates to [0026](0026-pinned-model-origin.md), [0051](0051-direct-compose-model-origins.md),
[0043](0043-unified-model-training.md), [0066](0066-pin-trained-model-parents.md),
and [0084](0084-record-model-lineage.md).
