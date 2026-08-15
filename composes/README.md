# Reference model composes

This directory contains measured reference baselines and the next candidates in
their capability progression. A candidate becomes a validated baseline only
after a complete real training run. Names describe intended milestones rather
than general capability or quality guarantees. See the
[model compose guide](../docs/MODEL-COMPOSE.md) for the complete schema and
field reference.

```bash
waldo model forecast composes/0000-canary.yaml
waldo model train canary composes/0000-canary.yaml
waldo model forecast composes/0001-babble.yaml
waldo model train babble-test composes/0001-babble.yaml
waldo model forecast composes/0002-basic.yaml
waldo model train basic-test composes/0002-basic.yaml
waldo model forecast composes/0003-intermediate.yaml
waldo model train intermediate-test composes/0003-intermediate.yaml
waldo model forecast composes/0004-conversation.yaml
waldo model train conversation-test composes/0004-conversation.yaml
```

| Compose | Architecture | Planned tokens | Corpus selection | Purpose |
| --- | ---: | ---: | --- | --- |
| `0000-canary.yaml` | 13.6M parameters | 4.1M | Four small prose, technical, and dialogue selections | Release-gate CUDA/MLX, artifact reload, accounting, and chat |
| `0001-babble.yaml` | 49.9M parameters | 1.05B | Gutenberg and PLOS, balanced by token exposure | Coherent local continuations from a compact base model |
| `0002-basic.yaml` | 114.1M parameters | 3.93B | Gutenberg, Wikimedia, and PLOS, balanced by token exposure | Basic cross-domain language-model capability over a longer context |
| `0003-intermediate.yaml` | 336.6M parameters | 12.0B | Books, encyclopedic, civic, and scientific text with declared weights | Broader intermediate base-model capability over a 2,048-token context |
| `0004-conversation.yaml` | 139.3M parameters | 6.04B | Clean books, scientific and civic text, followed by Dolly and OpenAssistant dialogue | First candidate for basic user/assistant interaction |

`0000-canary.yaml` has been validated end to end on a single H200. The first
babble experiment used `cl100k_base`, which spent 80% of its 47.9M parameters
on token embeddings and failed its generation acceptance test. The replacement
uses the GPT-2 `r50k_base` vocabulary and assigns about half of its similar total
size to the transformer backbone. Its single-H200 validation completed in 69m
50s: held-out loss improved from 4.7500 to 3.6145, the reloaded artifact measured
3.6109, corpus exposure remained balanced, and generated prose avoided the
previous repetition collapse.

The first `0002-basic.yaml` run completed in 6h 46m on one H200 and improved
held-out loss from 3.8348 to 3.0598, but generation remained prone to repeated
phrase loops. The revised candidate keeps its 114.1M-parameter and 3.93B-token
budget while adding 10% residual dropout, gentler optimization, a larger
shuffle window, and a 25/50/25 percent Gutenberg/Wikimedia/PLOS mixture.

`0003-intermediate.yaml` is an unvalidated 336.6M-parameter, 12.0B-token
candidate. Its estimate of approximately 48 hours on one H200 is scaled from
the measured basic run with additional allowance for its larger softmax,
longer context, dropout, checkpointing, and evaluation. Actual runtime remains
authoritative. An initial physical batch of 32 exhausted a 141 GB H200 while
materializing the FP32 loss input. The corrected batch of 16 doubles optimizer
steps to preserve the exact token budget, lowers peak memory substantially, and
uses a correspondingly gentler learning rate.

`0004-conversation.yaml` is an unvalidated two-stage candidate. Its 6.0B-token
pretraining stage establishes a compact language base without Wikimedia talk
pages; its 40.0M-token fine-tuning stage then repeats three human-written
dialogue corpora under a lower learning rate. It requires main content but does
not require schema-2 content assessments, so it remains executable against the
published index while older shards are being rebuilt. WALDO currently applies
causal loss to the complete formatted dialogue rather than masking user tokens,
and does not yet persist a chat prompt template. Test the learned training
format explicitly with a prompt such as `User: Hello\n\nAssistant:`.

The babble compose uses equal-exposure `causal-pretrain-balanced`. Later
composes use `causal-pretrain-weighted`, which applies declared integer corpus
weights while retaining deterministic shuffling, stratified evaluation, and
exact consumed token accounting. This prevents a finite run from silently
stopping before it reaches a declared source.

Hardware remains a deployment decision. Use `waldo model forecast` to compare
the compose against the accelerator catalog and locally observed calibration.
WALDO selects the training backend from machine-local configuration; compose
identity does not change when hardware changes.

The canary uses `tiktoken/cl100k_base`; the compact babble experiment uses
`tiktoken/r50k_base`. Both are portable offline subword tokenizers and use the
causal-language-modeling objective. Add or promote further composes only after
their budgets, accounting, saved artifacts, and observed behavior have been
measured on a real training run.
