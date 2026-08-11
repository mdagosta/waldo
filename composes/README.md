# Reference model composes

This directory contains measured reference baselines and the next candidates in
their capability progression. A candidate becomes a validated baseline only
after a complete real training run. Names describe intended milestones rather
than general capability or quality guarantees.

```bash
waldo model forecast composes/0000-canary.yaml
waldo model train canary composes/0000-canary.yaml
waldo model forecast composes/0001-babble.yaml
waldo model train babble-test composes/0001-babble.yaml
waldo model forecast composes/0002-basic.yaml
waldo model train basic-test composes/0002-basic.yaml
```

| Compose | Architecture | Planned tokens | Corpus selection | Purpose |
| --- | ---: | ---: | --- | --- |
| `0000-canary.yaml` | 13.6M parameters | 4.1M | Four small prose, technical, and dialogue selections | Release-gate CUDA/MLX, artifact reload, accounting, and chat |
| `0001-babble.yaml` | 49.9M parameters | 1.05B | Gutenberg and PLOS, balanced by token exposure | Coherent local continuations from a compact base model |
| `0002-basic.yaml` | 114.1M parameters | 3.93B | Gutenberg, Wikimedia, and PLOS, balanced by token exposure | Basic cross-domain language-model capability over a longer context |

`0000-canary.yaml` has been validated end to end on a single H200. The first
babble experiment used `cl100k_base`, which spent 80% of its 47.9M parameters
on token embeddings and failed its generation acceptance test. The replacement
uses the GPT-2 `r50k_base` vocabulary and assigns about half of its similar total
size to the transformer backbone. Its single-H200 validation completed in 69m
50s: held-out loss improved from 4.7500 to 3.6145, the reloaded artifact measured
3.6109, corpus exposure remained balanced, and generated prose avoided the
previous repetition collapse.

`0002-basic.yaml` is the next unvalidated candidate. It expands the backbone
to 114.1M parameters, doubles context to 1,024 tokens, broadens the corpus with
Wikimedia, and plans 3.93B tokens. Based on the observed babble run, it targets
approximately ten hours on one H200; the measured run remains authoritative.

The composes use `causal-pretrain-v2`, which deterministically interleaves the
declared corpus selections, stratifies held-out evaluation across them, and
records exact consumed token targets per corpus. This prevents a finite run
from silently stopping before it reaches a declared source.

Hardware remains a deployment decision. Use `waldo model forecast` to compare
the compose against the accelerator catalog and locally observed calibration.
WALDO selects the training backend from machine-local configuration; compose
identity does not change when hardware changes.

The canary uses `tiktoken/cl100k_base`; the compact babble experiment uses
`tiktoken/r50k_base`. Both are portable offline subword tokenizers and use the
causal-language-modeling objective. Add or promote further composes only after
their budgets, accounting, saved artifacts, and observed behavior have been
measured on a real training run.
