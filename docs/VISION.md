# Product vision

WALDO makes AI training inputs and outputs reviewable, versioned,
content-addressed, attributable, and reproducible.

The system connects three independent concerns:

1. A Git index records corpus meaning, source and license assertions, counts,
   and object hashes.
2. Lookaside storage serves canonical content-addressed Parquet objects.
3. Model workflows carry resolved corpus and model provenance into run records
   and release artifacts.

## Users

- Corpus contributors preparing reviewable index changes.
- Data consumers selecting, verifying, and exporting training material.
- Model builders retaining the inputs and lineage of each run.
- Curators operating an index or lookaside without combining their authority.

## Guarantees

WALDO aims to answer:

- Which index revision, manifests, sources, licenses, and objects were used?
- Did retrieved objects match their declared hashes?
- What did a training backend report consuming and producing?
- Which origins and runs led to a released artifact?

Hashes prove identity and integrity. They do not prove legal rights, safe model
behavior, truthful declarations by an untrusted process, or regulatory
compliance.

## Scope

The first public release is a single CLI that can inspect and contribute to an
index, verify and export corpora, train a small model through a supported local
backend, and export the model with machine-readable provenance.

Remote index services, automatic pull-request creation, source-specific
fetchers, supervised fine-tuning, preference optimization, and multi-node
orchestration are outside that initial scope.
