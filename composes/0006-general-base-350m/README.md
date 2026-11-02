# Rung 0006: 350M general-language base

This is the promoted 350M general-language base. It proves that WALDO can
train a 345.4M-parameter decoder on the frozen rung-0005 mixture and sustain
longer coherent continuation. It remains a raw base model, not an assistant or
a reliable factual system.

## Promoted result

- Model: `general-base-350m-20tpp-01`
- Model ID: `e724bd198b94`
- Run ID: `126406ff4349f479`
- Capacity: 345.4M total and 314.6M core parameters
- Tokenizer: 32K PressBooks-trained byte BPE
- Context: 2,048 tokens
- Exposure: 6,292,013,056 packed targets, or 20 core TPP
- Selected checkpoint: step 382,000 of 384,034, inside the final 20%
- Held-out loss: 10.5195 initially, 2.1994 selected, and 2.2000 final
- Held-out BPB: 0.8009 selected, improved from 0.8178 at 10 TPP
- Reload: selected artifact reproduced held-out loss 2.1994 exactly
- Generation: 20/20 sampled outputs contained two complete sentences; 18/20
  were judged grammatical and on topic through three sentences
- Repetition: 0/20 sampled and 1/20 greedy outputs looped
- EOS diagnostic: 17/20 sampled outputs emitted EOS within 512 tokens

## Promotion gates

- Training and evaluation metrics remain finite, and the selected artifact
  reloads exactly.
- The selected checkpoint lies in the final 20% of training.
- Selected held-out BPB is no worse than 0.85 and does not regress from 10 TPP.
- At least 18/20 sampled outputs remain grammatical and on topic through three
  sentences.
- No more than 1/20 sampled or 2/20 greedy outputs enter a phrase loop.

Every gate passed. EOS remains diagnostic. The model is not promoted for
factual accuracy, instruction following, coding, or multi-step reasoning.

## Controlled exposure result

The 5-, 10-, and 20-core-TPP phases changed only token exposure. Architecture,
tokenizer, source views, mixture weights, optimizer, schedule, seed, held-out
selection, and native evaluation prompts remained fixed. Selected BPB improved
from 0.8442 at 5 TPP to 0.8178 at 10 TPP and 0.8009 at 20 TPP. The selected
checkpoint remained near the end of training and generation met the frozen
final gate, so 20 TPP is the promoted recipe. No extension beyond 20 TPP is
authorized without a new evidence review.

## Run and evaluate

From the repository root:

```console
go run ./cmd/waldo/ model forecast \
  composes/0006-general-base-350m/compose.yaml
go run ./cmd/waldo/ model train general-base-350m-20tpp-01 \
  composes/0006-general-base-350m/compose.yaml

./composes/0006-general-base-350m/evaluate.sh \
  general-base-350m-20tpp-01 /tmp/rung0006-sampled.jsonl 0.8 42 512
./composes/0006-general-base-350m/evaluate.sh \
  general-base-350m-20tpp-01 /tmp/rung0006-greedy.jsonl 0 42 512
python3 composes/0006-general-base-350m/score.py \
  /tmp/rung0006-sampled.jsonl /tmp/rung0006-greedy.jsonl
```

Use `--hostfile` only when additional hosts are available. The promoted result
used one host with two H200 GPUs.

## Directory contents

- `compose.yaml`: exact promoted training recipe
- `compose_test.go`: architecture, tokenizer, data, and training guards
- `suite.json`: immutable native generation prompts
- `evaluate.sh`: native `waldo model chat` runner
- `score.py`: sentence-completion, EOS, and repetition diagnostics
- `evidence.json`: immutable promoted result
- `README.md`: purpose, evidence, gates, and commands
