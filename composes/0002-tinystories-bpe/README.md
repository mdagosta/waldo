# Rung 0002: TinyStories BPE reference

This is the first promoted language-capability model. It proves coherent
narrow-domain generation, entity and causal continuity, repetition control,
and EOS behavior with a trained byte-BPE tokenizer.

## Promoted result

- Model: `tinystories-bpe-20tpp-01`
- Model ID: `5d18938fa102`
- Run ID: `3ee94d2f3e466ce2`
- Capacity: 32.8M total parameters and 30.7M core parameters
- Tokenizer: 4K PressBooks-trained byte BPE
- Context: 512 tokens
- Exposure: 613,613,568 packed targets, or 20 tokens per core parameter
- Held-out loss: 1.042866
- Held-out BPB: 0.498234
- Behavior: 7/8 sampled continuations retained prompt entities and causality
- Repetition: no sampled and one greedy continuation entered a clear loop
- Termination: 8/8 long-horizon samples emitted EOS

## Promotion gates

- Training metrics remain finite and the selected artifact reloads exactly.
- Held-out loss and BPB meet the frozen thresholds.
- At least 7/8 sampled continuations retain entities and causal direction.
- No sampled and at most one greedy continuation enters a repetition loop.
- Every long-horizon sampled continuation emits EOS.

## Run

From the repository root:

The exact tokenizer measurement is optional and is not reusable by training;
do not run it routinely before the training command. Use it only when
qualifying the tokenizer/corpus combination:

```console
go run ./cmd/waldo/ model forecast \
  composes/0002-tinystories-bpe/compose.yaml --measure-tokenizer
```

Train and evaluate with:

```console
go run ./cmd/waldo/ model train tinystories-bpe-20tpp-01 \
  composes/0002-tinystories-bpe/compose.yaml --hostfile ~/hostfile
./composes/0002-tinystories-bpe/evaluate.sh \
  tinystories-bpe-20tpp-01 /tmp/tinystories-bpe-eval.jsonl
./composes/0002-tinystories-bpe/evaluate.sh \
  tinystories-bpe-20tpp-01 /tmp/tinystories-bpe-greedy.jsonl 0 42
./composes/0002-tinystories-bpe/evaluate.sh \
  tinystories-bpe-20tpp-01 /tmp/tinystories-bpe-eos.jsonl 0.8 42 1536
```

## Contents

- `compose.yaml`: immutable promoted training recipe
- `compose_test.go`: architecture, tokenizer, data, and training-control guards
- `evaluate.sh`: fixed generation probes
- `README.md`: purpose, evidence, gates, and commands
