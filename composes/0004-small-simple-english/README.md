# Rung 0004: small simple-English base

This is the first promoted base model beyond the TinyStories-only language
distribution. It generates short narratives, descriptions, dialogue, and
elementary explanatory prose. It is not an assistant and does not claim
reliable factual recall or question answering.

## Design

- 64.7M total and 59.5M non-embedding parameters
- 12 decoder layers, width 640, SwiGLU width 1,728, and 10 full-attention heads
- tied 8K PressBooks-trained byte-BPE embeddings and 1,024-token context
- equal packed-token weights for GPT-4 TinyStories V2 and the
  `young_children`/`textbook_narrative` Cosmopedia v2 view
- 1,189,806,080 packed targets, or 20.0 tokens per core parameter
- single-host training on two H200 GPUs; distributed training is not required

The architecture was selected by a matched-compute pilot. The proposed
24-layer, width-448 shape reached held-out loss 2.9966 after roughly 315M
tokens; the 12-layer, width-640 control reached 2.9279 after roughly 346M.
The deeper shape was rejected.

Preflight measured 865.8M unique targets. The qualification run exposed the
GPT-4 TinyStories view about 0.92 times and the filtered Cosmopedia view about
2.73 times, both below the four-pass limit. The broader unfiltered Cosmopedia
corpus was not used.

## Promoted result

- Model: `small-simple-english-65m-gpt4-education-01`
- Model ID: `e1ad80e6a6f4`
- Run ID: `6b78d8d47cdc0545`
- Selected step: 72,620 of 72,620
- Initial held-out loss: 9.0928
- Selected, final, and reloaded held-out loss: 1.6312
- Packed targets: 594.6M filtered Cosmopedia and 595.2M GPT-4 TinyStories
- Frozen broad-form BPB: 1.1189, versus 1.2964 for rung 0002
- Paired BPB delta: -0.1775, 95% bootstrap interval [-0.4021, -0.0017]
- Explanation BPB: 1.0909, versus 1.8259 for rung 0002
- Correct-context advantage: 0.4701 BPB, versus 0.0034 untrained
- Generation: 20/20 sampled outputs produced two complete sentences
- Degeneration: 0/20 sampled loops and 0/20 greedy loops
- EOS diagnostic: 15/20 sampled outputs emitted EOS within 512 tokens

The correct-context control rotates the same continuation texts among prompts
of the same form. Correct and mismatched sets therefore contain identical
continuation bytes; their aggregate difference measures prompt/continuation
fit rather than prose difficulty or response length.

## Promotion gates

- paired 95% confidence interval for candidate minus rung-0002 BPB is below
  zero;
- correct-context advantage is at least 0.25 BPB and the deterministic
  untrained control remains within 0.05 BPB of zero;
- explanation BPB improves by at least 25%, with no form regressing by more
  than 0.15 BPB;
- at least 18/20 sampled outputs contain two complete sentences; and
- no more than 2/20 sampled or 3/20 greedy outputs enter a phrase loop.

Every gate passed. EOS remains a diagnostic because this is a document base
model: a valid explanation can exceed an arbitrary generation horizon. A
future assistant rung must separately prove bounded response and EOS behavior.

## Corpus

`core/synthetic/tinystories-gpt4-v2` is the official
`TinyStoriesV2-GPT4-train.txt` release pinned at dataset revision
`f54c09fd23315a6f9c86f9dc80f725de7d8f9c64`, SHA-256
`6418d412de72888f52b5142c761ac21a582f7d1166f0bfbdb5f03ccfdec90443`.
The separator-aware ingest adapter maps each literal `<|endoftext|>` segment to
one canonical record without rewriting the source artifact.

## Run and evaluate

The exact tokenizer measurement is optional and is not reusable by training;
do not run it routinely before the training command. Use it only when
qualifying the tokenizer/corpus combination:

```console
go run ./cmd/waldo/ model forecast \
  composes/0004-small-simple-english/compose.yaml --measure-tokenizer
```

Train and evaluate with:

```console
go run ./cmd/waldo/ model train small-simple-english-65m-gpt4-education-01 \
  composes/0004-small-simple-english/compose.yaml

./composes/0004-small-simple-english/evaluate.sh \
  small-simple-english-65m-gpt4-education-01 /tmp/rung0004-sampled.jsonl \
  0.8 42 512
./composes/0004-small-simple-english/evaluate.sh \
  small-simple-english-65m-gpt4-education-01 /tmp/rung0004-greedy.jsonl \
  0 42 512
python3 composes/0004-small-simple-english/score.py \
  /tmp/rung0004-sampled.jsonl /tmp/rung0004-greedy.jsonl

./composes/0004-small-simple-english/evaluate-likelihood.sh \
  small-simple-english-65m-gpt4-education-01 \
  tiny-grounded-responder-30m-01 3914b731488b404a
```

The baseline run ID selects the rung-0002 foundation checkpoint preserved as
the first run of the promoted rung-0003 model.

## Contents

- `compose.yaml`: exact promoted training recipe
- `compose_test.go`: architecture, tokenizer, data-view, and training guards
- `suite.json`: immutable broad-form prompts and reference continuations
- `evaluate.sh` and `score.py`: fixed generation and degeneration diagnostics
- `evaluate-likelihood.sh`, `likelihood.py`, `compare.py`, and
  `tools/encode-suite`: exact BPB and context-coherence evaluation
