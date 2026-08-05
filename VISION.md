# WALDO product charter

## Purpose

WALDO makes AI training material behave more like open-source inputs: named,
reviewable, versioned, attributable, content-addressed, removable, and usable
without trusting a central data service.

The public commons is represented by a Git metadata index. Large shard objects
live in federated lookaside storage. A consumer resolves an index selection to
an immutable OpenWALDO BOM, verifies every object named by it, and can carry
that BOM into an export or model training record.

WALDO is one binary because a complete provenance chain is one user journey.
It is not one undifferentiated subsystem: index, corpus, lookaside, provenance,
and model code have explicit responsibilities and one-way dependencies.

## Primary users

1. A corpus contributor who wants to turn acquired source material into a
   reviewable, DCO-signed index contribution.
2. A data consumer who wants to inspect, verify, select, and export a corpus by
   source or license policy.
3. A model builder who wants each run and exported model to retain an exact
   account of the indexed material it consumed.
4. A curator or operator who maintains the index namespace or lookaside
   availability without conflating metadata governance with object hosting.

## Product promises

WALDO will make it possible to answer:

- What corpus material was selected?
- Which index revision and manifests described it?
- Where did the material originate and what licenses were asserted?
- Did the bytes fetched by WALDO match their declared hashes?
- What did a training backend report consuming and producing?
- What model lineage and training runs led to an exported artifact?

WALDO does not claim that a hash proves legal rights, that a DCO assertion is
legally correct, or that a declared training BOM proves consumption by an
untrusted process. Rights claims are made specific, attributable, and
falsifiable. Stronger consumption and execution attestations are separate,
future capabilities and must never be implied by the baseline BOM.

## Scope of the first useful release

- Read and verify the existing `waldo-index` format.
- Inspect and summarize an index checkout.
- Select corpora and materialize hash-verified shards through a local cache.
- Export native Parquet or canonical interchange data with a corpus BOM.
- Add a basic corpus deterministically and prepare its index contribution.
- Build a small model from a declarative compose through one real backend.
- Record durable run state, actual backend results, model lineage, and weight
  hashes.
- Export a model with its provenance attached.

## Explicitly deferred

- A remote index API or centrally managed index checkout.
- Automated pull-request creation.
- Fetcher implementations in this repository.
- SFT, preference optimization, and frontier-scale orchestration.
- Multiple competing configuration mechanisms.
- Compatibility with every old CLI spelling.

## Success criteria

The implementation is successful when a new user can understand the normal
workflow from `waldo --help`, a curator can verify the real index without
special knowledge, and a small model can be rebuilt from a compose with a clear
machine-readable chain from index commit to output weights.
