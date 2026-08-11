# Reference model composes

This directory contains validated reference composes and clearly identified
experimental candidates. Neither is a capability or quality guarantee.

```bash
waldo model forecast composes/0000-canary.yaml
waldo model train canary composes/0000-canary.yaml
waldo model forecast composes/0001-babble.yaml
waldo model train babble-test composes/0001-babble.yaml
```

| Compose | Architecture | Planned tokens | Corpus selection | Purpose |
| --- | ---: | ---: | --- | --- |
| `0000-canary.yaml` | 13.6M parameters | 4.1M | Four small prose, technical, and dialogue selections | Release-gate CUDA/MLX, artifact reload, accounting, and chat |
| `0001-babble.yaml` | 47.9M parameters | 1.05B | Gutenberg and PLOS, balanced by token exposure | Experimental candidate for coherent local continuations |

`0000-canary.yaml` has been validated end to end on a single H200. The babble
compose is intentionally marked experimental until a real run establishes its
loss curve, artifact parity, corpus accounting, runtime, and generated output.

The composes use `causal-pretrain-v2`, which deterministically interleaves the
declared corpus selections, stratifies held-out evaluation across them, and
records exact consumed token targets per corpus. This prevents a finite run
from silently stopping before it reaches a declared source.

Hardware remains a deployment decision. Use `waldo model forecast` to compare
the compose against the accelerator catalog and locally observed calibration.
WALDO selects the training backend from machine-local configuration; compose
identity does not change when hardware changes.

Both use the portable `tiktoken/cl100k_base` subword tokenizer and
causal-language-modeling objective. Promote experimental composes to reference
status only after their budgets, accounting, saved artifacts, and observed
behavior have been measured on a real training run.
