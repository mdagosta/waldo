# ADR 0047: Right-size tokenizer vocabularies for compact models

Status: accepted

## Context

The first compact subword experiment paired a 100259-token `cl100k_base`
vocabulary with a 47.9 million parameter model. Its tied embedding table used
38.5 million parameters, leaving only 9.4 million for transformer blocks. After
1.05 billion training tokens, held-out loss improved but generation collapsed
into repetitive, incoherent continuations. The total parameter count obscured
the imbalance.

## Decision

WALDO additionally supports `tiktoken/r50k_base@tiktoken-r50k-base` as a
portable offline tokenizer. Ordinary GPT-2 BPE token IDs are 0 through 50255.
WALDO owns pad 50256, BOS 50257, and EOS 50258, for vocabulary size 50259.

Tokenizer identity, revision, vocabulary size, and special IDs remain pinned in
the model architecture and tokenizer artifact. Go performs encoding and
decoding; framework workers continue receiving tokenizer-independent integer
IDs. Existing byte and `cl100k_base` models remain readable and executable.

Compact reference experiments should report total and non-embedding parameter
allocation during review. The initial `r50k_base` experiment keeps the prior
corpus and token budget while reallocating roughly half of a 49.9 million
parameter model to transformer blocks.

## Consequences

- Compact English-language experiments spend substantially less capacity on
  token embeddings.
- Existing model and worker schemas do not change; this is an additive pinned
  tokenizer identity.
- Unsupported tokenizer revisions and vocabulary sizes continue to fail closed.
- The smaller vocabulary is an experiment contract, not a general claim that
  one tokenizer is superior for every language or model scale.
