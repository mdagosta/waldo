# Reference model composes

This directory contains model composes validated through complete real training
runs. They are measured reference baselines, not general capability or quality
guarantees.

```bash
waldo model forecast composes/0000-canary.yaml
waldo model train canary composes/0000-canary.yaml
waldo model forecast composes/0001-babble.yaml
waldo model train babble-test composes/0001-babble.yaml
```

| Compose | Architecture | Planned tokens | Corpus selection | Purpose |
| --- | ---: | ---: | --- | --- |
| `0000-canary.yaml` | 13.6M parameters | 4.1M | Four small prose, technical, and dialogue selections | Release-gate CUDA/MLX, artifact reload, accounting, and chat |
| `0001-babble.yaml` | 49.9M parameters | 1.05B | Gutenberg and PLOS, balanced by token exposure | Coherent local continuations from a compact base model |

`0000-canary.yaml` has been validated end to end on a single H200. The first
babble experiment used `cl100k_base`, which spent 80% of its 47.9M parameters
on token embeddings and failed its generation acceptance test. The replacement
uses the GPT-2 `r50k_base` vocabulary and assigns about half of its similar total
size to the transformer backbone. Its single-H200 validation completed in 69m
50s: held-out loss improved from 4.7500 to 3.6145, the reloaded artifact measured
3.6109, corpus exposure remained balanced, and generated prose avoided the
previous repetition collapse.

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
