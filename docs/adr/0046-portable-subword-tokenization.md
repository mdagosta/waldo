# ADR 0046: Portable subword tokenization

Status: Accepted

## Context

The first reference models used one token per UTF-8 byte. This made context
windows inefficient, inflated the steps needed to learn words, and produced
misleadingly low byte-level perplexity without useful completion behavior.

## Decision

WALDO supports `tiktoken/cl100k_base@tiktoken-cl100k-base` as an executable,
offline subword tokenizer. Ordinary token IDs are 0 through 100255. WALDO owns
pad 100256, BOS 100257, and EOS 100258, for vocabulary size 100259.

Tokenization and decoding run in WALDO's Go process from the bundled offline
vocabulary. Framework workers receive token IDs, so PyTorch and MLX do not gain
a tokenizer dependency and use identical input. The tokenizer artifact pins
the algorithm, revision, vocabulary size, and special IDs. Inference uses the
same Go codec.

The byte tokenizer remains supported for existing models and resumes. New
reference composes use the subword tokenizer.

## Consequences

- Training and inference tokenizer parity is independent of framework code.
- Canonical corpus rows remain tokenizer-neutral text.
- Subword worker streams carry token IDs instead of raw text.
- Forecasts include the larger embedding table.
