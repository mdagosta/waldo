# Rung 0003: narrow grounded responder

This rung answers one reasonable question about the passed 30.7M-core model:
can it read a short supplied fact, return a relevant answer, and stop?

It does not test open-domain knowledge, arithmetic, coding, or multi-step
reasoning. The supervised corpus contains only five TinyStories-level task
families: place extraction, recipient extraction, yes/no classification,
explicit-cause questions, and one-name rewriting.

## Design

- The architecture, tokenizer, and first 613.6M-token stage are byte-for-byte
  the promoted rung-0002 recipe. This is necessary because the promoted model
  artifact is no longer present on `reno-gpu-01`.
- The second stage runs 2.5M packed tokens (less than one effective corpus
  pass) with assistant-only loss, a `2e-5` peak learning rate, and `1%` weight
  decay.
- The generator emits 80,000 deterministic examples; canonical ingestion
  retains 37,632 unique conversations after exact deduplication. The independent
  20-prompt evaluation uses names, colors, objects, and places absent from the
  training generator.
- Training is single-host on `reno-gpu-01`; `reno-gpu-02` must not be used.

## Promoted result

- Model: `tiny-grounded-responder-30m-01`
- Model ID: `5627235b6d60`
- Foundation run: `3914b731488b404a`
- Supervised run: `68b7a196f319ce09`
- Capacity: 32.8M total parameters and 30.7M core parameters
- Foundation exposure: 613,613,568 packed targets, or 20 core TPP
- Foundation held-out loss: `1.0422`, versus `1.042866` for rung 0002
- Supervised exposure: 2,506,752 packed tokens and 360.6K assistant targets
- Supervised held-out loss: `0.0096`
- Grounded behavior: 20/20 bounded EOS, 17/20 relevant starts, and
  14/15 exact extraction or classification answers
- Repetition: 0/20 interaction loops and no clear retained-story loop
- Retention: 7/8 conservative entity/causal passes, 8/8 sampled EOS,
  7/8 greedy EOS, and 8/8 long-horizon sampled EOS

The one grounded failure answered `Yes` instead of `No`. Two rewriting probes
changed a supplied noun while preserving the requested person and sentence
shape. These errors remain within the frozen gates and define concrete limits
of this 30M model; the rung does not claim reliable general instruction
following.

## Promotion gates

- 18/20 responses obey the assistant boundary and emit EOS within 128 tokens.
- 14/20 begin with the expected relevant answer.
- 12/15 extraction and classification answers are exact.
- No more than 1/20 responses enters a repetition loop.
- The final weights retain at least 6/8 rung-0002 entity/causal passes and 7/8
  long-horizon EOS.

Every gate passed on the immutable suite. The scorer consumes WALDO's
concatenated pretty-printed JSON objects and records the per-prompt decision.

## Corpus preparation

Generate the manifest-backed source directory, then ingest it at
`post-train/sft/tiny-grounded-responses-v1`:

```console
python3 composes/0003-narrow-grounded-responder/prepare-corpus.py \
  /tmp/tiny-grounded-responses-v1
go run ./cmd/waldo/ index ingest /tmp/tiny-grounded-responses-v1 \
  ~/.waldo/index/post-train/sft/tiny-grounded-responses-v1
```

## Training and evaluation

```console
go run ./cmd/waldo/ model forecast \
  composes/0003-narrow-grounded-responder/compose.yaml
go run ./cmd/waldo/ model train tiny-grounded-responder-30m-01 \
  composes/0003-narrow-grounded-responder/compose.yaml
./composes/0003-narrow-grounded-responder/evaluate.sh \
  tiny-grounded-responder-30m-01 /tmp/tiny-grounded-responder-30m-01.jsonl
./composes/0002-tinystories-bpe/evaluate.sh \
  tiny-grounded-responder-30m-01 /tmp/tiny-grounded-responder-retention.jsonl
./composes/0002-tinystories-bpe/evaluate.sh \
  tiny-grounded-responder-30m-01 /tmp/tiny-grounded-responder-eos.jsonl 0.8 42 1536
```

The specialized `forecast --measure-tokenizer` analysis currently accepts text
causal-language-modeling records only. `model train` still performs the
mandatory structured-conversation selection, holdout, tokenization, packing,
and budget preflight before starting the supervised worker.
