# Reference model composes

These schema-1 composes form a capability ladder for blank WALDO models. Their
names describe the experiment's intended learning milestone, not a GPU model or
wall-clock promise.

```bash
waldo model forecast composes/babble.yaml
waldo model compose babble composes/babble.yaml
```

| Compose | Architecture | Planned byte tokens | Corpus progression | Intended milestone |
| --- | ---: | ---: | --- | --- |
| `babble.yaml` | 9.5M parameters | 1.05B | Gutenberg, recipes, essays, and Python specifications | Recognizable local continuations across more than one prose style |
| `reader.yaml` | 75.7M parameters | 4.84B | Open books plus the small mixed-text sources | Coherent sentences and short paragraphs |
| `writer.yaml` | 270.8M parameters | 4.92B | Open books and Wikimedia | Longer topical and document-structured prose |
| `knowledge.yaml` | 270.8M parameters | 8.12B | Books, Wikimedia, Python specifications, and PLOS | Broader factual and technical completion |
| `generalist.yaml` | 270.8M parameters | 16.25B | Literary, encyclopedic, Q&A, civic, and scientific text | More robust completion across several domains and styles |
| `assistant.yaml` | 270.8M parameters | 32.49B pretrain + 118M dialogue capacity | Broad pretraining followed by Aya, Dolly, OASST1, and OASST2 | Basic `User:`/`Assistant:` prompt-response behavior |

The milestones are hypotheses to evaluate, not quality guarantees. In
particular, `assistant.yaml` is a small research model, not a claim of factual
reliability, safety, or production instruction-following quality.

The byte-token capacities are `steps × batch_size × sequence_length`. Corpus
selections broaden with the ladder so larger experiments do not merely repeat
Gutenberg. `babble.yaml` deliberately remains small enough to exercise the
pipeline while adding recipes, public-domain essays, and technical prose to its
book data.

Hardware remains a deployment decision. Use `waldo model forecast` to compare
the same capability compose against the accelerator catalog and locally
observed calibration. WALDO selects the training backend from machine-local
configuration; compose identity does not change when hardware changes.

All templates use the currently executable byte tokenizer and
causal-language-modeling objective. The final rung adds a fine-tuning stage over
the complete `post-train/sft` index selection after broad pretraining.
