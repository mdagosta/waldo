# Reference model composes

These schema-1 composes are runnable examples for training blank WALDO models.
They use the currently executable byte tokenizer and causal pre-training
profile. Run one with:

```bash
waldo model forecast composes/babble-mac.yaml
waldo model compose babble composes/babble-mac.yaml
```

The names describe approximate catalog compute budgets, not deadlines or
quality guarantees. Forecasts use WALDO's
`openwaldo-training-hardware-2026-08-05` assumptions; real time varies with the
host, backend, storage, and locally observed calibration. The H200 templates
target **one NVIDIA H200 SXM**, not an eight-GPU node.

| Compose | Architecture | Planned byte tokens | Catalog target | Corpus selection | Intent |
| --- | ---: | ---: | ---: | --- | --- |
| `babble-mac.yaml` | 9.5M parameters | 1.05B | about 1h on the catalog M4 Max; several hours on slower or locally calibrated Macs | Gutenberg | Smallest from-scratch model expected only to produce continuations |
| `h200-02h.yaml` | 75.7M parameters | 4.84B | about 2h on 1× H200 | Gutenberg | Small accelerator trial |
| `h200-06h.yaml` | 270.8M parameters | 4.06B | about 6h on 1× H200 | Gutenberg | Short base-model run |
| `h200-12h.yaml` | 270.8M parameters | 8.12B | about 12h on 1× H200 | Open books | Longer base-model run |
| `h200-24h.yaml` | 270.8M parameters | 16.25B | about 24h on 1× H200 | Books and Wikimedia | Broad intermediate run |
| `h200-48h.yaml` | 270.8M parameters | 32.49B | about 48h on 1× H200 | Books, Wikimedia, and Stack Exchange | First template intended for basic interactive evaluation |

The byte-token budgets are `steps × batch_size × sequence_length`. Corpus
selections intentionally grow with the budget so one epoch supplies enough
source text without silently repeating a smaller dataset. WALDO still audits
and materializes the complete selected corpus before training.

The 48-hour template is large enough for meaningful pipeline and basic model
evaluation, but it is not a claim of production quality, factual reliability,
or instruction following. These are base-model composes; they contain no chat
template or supervised fine-tuning stage.
