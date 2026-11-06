# ADR 0085: Merge models of one family into a merged origin

Status: accepted
- Date: 2026-11-05

## Context

Averaging the weights of models fine-tuned from common weights is an
established way to combine them, but merges made outside WALDO discard
provenance. A merged model has no training run, and ADR 0026 forbids recording
a synthetic one.

## Decision

A compose base may be `merge`: two or more managed models with identical
architectures and the byte tokenizer, each with an optional weight given for
every parent or none.

- **Merge-base.** WALDO walks each parent's history (ADR 0084) by weights hash,
  as git walks commits, to the common ancestors no other common ancestor
  descends from. Every step above them must be in the history, with no gap or
  end, and every earlier merge there must have parents that reach them. Gaps
  below are recorded, not refused; there is no override.
- **`MERGE.json`** (`waldo-model-merge`, schema 1) records the method
  (`mean-f32-v1` or `weighted-mean-f32-v1`) and each parent's making run or
  origin BOM, weights hash, license, and weight, sorted by weights hash. It
  names no models or places.
- **Merged origin.** WALDO averages in float32 in that order, rounds to
  nearest-even bfloat16 or float32, and writes an origin under
  `<model.root>/.merges/<hash of MERGE.json>`, with provider `merge` and that
  hash as its revision. Its `LINEAGE.json` holds `MERGE.json` and every
  parent's history, so the parents can be deleted.
- A merge-only compose has no stages and needs no training backend; its model
  serves, exports, and bases other composes. With stages, the merge
  initializes training like any origin.

## Consequences

- No record changes: earlier WALDO versions read merged models as origins.
- The same parents give the same `MERGE.json` and the same weights, so
  independent merges compare byte for byte.
- Earlier merges in a history are checked by their documents, not recomputed.
- A merge never invents a training run; changing the arithmetic needs a new
  method name.
- Parents trained from random weights, or pulled without history, can't be
  merged, except copies of the same weights.
- A merged model needs its `.merges` entry to be composed again.
