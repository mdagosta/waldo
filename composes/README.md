# Reference model composes

This directory contains model composes that WALDO has exercised end to end.
Reference composes are release and experimentation baselines, not capability
or quality guarantees.

```bash
waldo model forecast composes/0000-canary.yaml
waldo model train canary composes/0000-canary.yaml
```

| Compose | Architecture | Planned tokens | Corpus selection | Purpose |
| --- | ---: | ---: | --- | --- |
| `0000-canary.yaml` | 13.6M parameters | 4.1M | Four small prose, technical, and dialogue selections | Release-gate CUDA/MLX, artifact reload, accounting, and chat |

The canary uses `causal-pretrain-v2`, which deterministically interleaves the
declared corpus selections, stratifies held-out evaluation across them, and
records exact consumed token targets per corpus. This prevents a finite run
from silently stopping before it reaches a declared source.

Hardware remains a deployment decision. Use `waldo model forecast` to compare
the compose against the accelerator catalog and locally observed calibration.
WALDO selects the training backend from machine-local configuration; compose
identity does not change when hardware changes.

The canary uses the portable `tiktoken/cl100k_base` subword tokenizer and
causal-language-modeling objective. Add larger reference composes only after
their budgets, accounting, saved artifacts, and observed behavior have been
measured on a real training run.
