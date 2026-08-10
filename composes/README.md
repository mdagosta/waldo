# Reference model composes

These schema-1 composes form a capability ladder for blank WALDO models. Their
names describe the experiment's intended learning milestone, not a GPU model or
wall-clock promise.

```bash
waldo model forecast composes/0000-canary.yaml
waldo model train canary composes/0000-canary.yaml
```

| Compose | Architecture | Planned tokens | Corpus progression | Intended milestone |
| --- | ---: | ---: | --- | --- |
| `0000-canary.yaml` | 13.6M parameters | 16.4M | Four distinct prose and technical selections | Release-gate CUDA/MLX, artifact reload, accounting, and chat |
| `0001-babble.yaml` | 47.9M parameters | 1.05B | Gutenberg, recipes, essays, and Python specifications | Recognizable local continuations across more than one prose style |
| `0002-reader.yaml` | 152.5M parameters | 4.84B | Open books plus the small mixed-text sources | Coherent sentences and short paragraphs |
| `0003-writer.yaml` | 373.2M parameters | 4.92B | Open books and Wikimedia | Longer topical and document-structured prose |
| `0004-knowledge.yaml` | 373.2M parameters | 8.12B | Books, Wikimedia, Python specifications, and PLOS | Broader factual and technical completion |
| `0005-generalist.yaml` | 373.2M parameters | 16.25B | Literary, encyclopedic, Q&A, civic, and scientific text | More robust completion across several domains and styles |
| `0006-assistant.yaml` | 373.2M parameters | 32.49B pretrain + 118M dialogue capacity | Broad pretraining followed by Aya, Dolly, OASST1, and OASST2 | Basic `User:`/`Assistant:` prompt-response behavior |

The milestones are hypotheses to evaluate, not quality guarantees. In
particular, `0006-assistant.yaml` is a small research model, not a claim of factual
reliability, safety, or production instruction-following quality.

The token capacities are `steps × batch_size × sequence_length`. Corpus
selections broaden with the ladder so larger experiments do not merely repeat
Gutenberg. `0001-babble.yaml` deliberately remains small enough to exercise the
pipeline while adding recipes, public-domain essays, and technical prose to its
book data.

The templates use `causal-pretrain-v2`, which deterministically interleaves the
declared corpus selections, stratifies held-out evaluation across them, and
records exact consumed token targets per corpus. This prevents a finite run from
silently stopping before it reaches a declared source.

Hardware remains a deployment decision. Use `waldo model forecast` to compare
the same capability compose against the accelerator catalog and locally
observed calibration. WALDO selects the training backend from machine-local
configuration; compose identity does not change when hardware changes.

All templates use the portable `tiktoken/cl100k_base` subword tokenizer and
causal-language-modeling objective. The final rung adds a fine-tuning stage over
the complete `post-train/sft` index selection after broad pretraining.
