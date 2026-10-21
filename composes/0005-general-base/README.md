# Rung 0005: 125M general-language base

This is the first promoted general-language base model. It proves that WALDO
can train a roughly 125M-parameter decoder on a controlled four-source mixture
and produce grammatical, locally relevant prose without treating the model as
an assistant or reliable factual system.

## Promoted result

- Model: `general-base-125m-20tpp-01`
- Model ID: `2d21eb00a261`
- Run ID: `50fdb491d54e0bdf`
- Capacity: 124.6M total and approximately 106.2M core parameters
- Tokenizer: 32K PressBooks-trained byte BPE
- Context: 2,048 tokens
- Exposure: 2,124,005,376 packed targets, or 20 core TPP
- Selected checkpoint: step 114,000 of 129,639
- Held-out loss: 10.4318 initially, 2.4652 selected, and 2.4856 final
- Held-out BPB at the selected checkpoint: 0.8977
- Reload: selected artifact reproduced held-out loss 2.4652 exactly
- Generation: 20/20 sampled outputs contained two complete sentences; 16/20
  were judged grammatical and on topic for both sentences
- Repetition: 0/20 sampled and 2/20 greedy outputs looped
- EOS diagnostic: 16/20 sampled outputs emitted EOS within 512 tokens

## Promotion gates

- Training and evaluation metrics remain finite, and the selected artifact
  reloads exactly.
- The selected checkpoint lies in the final 20% of training.
- No source view exceeds four effective passes.
- At least 16/20 sampled continuations are grammatical and stay on topic for
  two sentences.
- At least 18/20 sampled outputs contain two complete sentences.
- No more than 2/20 sampled or 3/20 greedy outputs enter a phrase loop.

Every gate passed. EOS is diagnostic because this is a document base model.
Open-domain factual accuracy, chat behavior, coding, and multi-step reasoning
are explicitly outside this rung's claims.

The supplementary public evaluation recorded WikiText BPB 1.0633, ARC-Easy
normalized accuracy 0.3754, PIQA normalized accuracy 0.5680, SciQ normalized
accuracy 0.4910, and LAMBADA exact-match accuracy 0.2467. These measurements
support comparison with public models; they do not replace the native WALDO
generation gates used throughout the ladder.

## Data

The training mixture is 45% Wikimedia, 30% filtered young-child Cosmopedia
textbook narrative, 15% StackExchange, and 10% GPT-4 TinyStories. Actual
consumption matched those weights. The largest measured source exposure was
3.40 effective passes, below the frozen maximum of four.

## Run and evaluate

From the repository root:

```console
go run ./cmd/waldo/ model forecast composes/0005-general-base/compose.yaml
go run ./cmd/waldo/ model train general-base-125m-20tpp-01 \
  composes/0005-general-base/compose.yaml

./composes/0005-general-base/evaluate.sh \
  general-base-125m-20tpp-01 /tmp/rung0005-sampled.jsonl 0.8 42 512
./composes/0005-general-base/evaluate.sh \
  general-base-125m-20tpp-01 /tmp/rung0005-greedy.jsonl 0 42 512
python3 composes/0005-general-base/score.py \
  /tmp/rung0005-sampled.jsonl /tmp/rung0005-greedy.jsonl
```

Use `--hostfile` with `model train` only when the additional hosts are
available. The promoted result used one host with two H200 GPUs.

## Directory contents

- `compose.yaml`: exact promoted training recipe
- `compose_test.go`: architecture, tokenizer, data, and training guards
- `suite.json`: immutable native generation prompts
- `evaluate.sh`: native `waldo model chat` runner
- `score.py`: sentence-completion, EOS, and repetition diagnostics
- `evidence.json`: immutable promoted result
- `README.md`: purpose, evidence, gates, and commands
