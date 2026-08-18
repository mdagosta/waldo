# Reference model composes

This directory contains a small capability ladder. A compose is a candidate
until a complete real training run validates its accounting, saved artifacts,
evaluation, and generated behavior. Names describe observed or intended
milestones rather than general intelligence claims. See the
[model compose guide](../docs/MODEL-COMPOSE.md) for the complete schema and
field reference.

```bash
waldo model forecast composes/0000-canary.yaml
waldo model train canary composes/0000-canary.yaml

waldo model forecast composes/0001-babble.yaml
waldo model train babble composes/0001-babble.yaml

waldo model forecast composes/0002-conversation.yaml
waldo model train conversation composes/0002-conversation.yaml

waldo model forecast composes/0003-tool-assistant.yaml
waldo model train tool-assistant composes/0003-tool-assistant.yaml
```

| Compose | Architecture | Planned tokens | Purpose |
| --- | ---: | ---: | --- |
| `0000-canary.yaml` | 13.6M parameters | 4.1M | Release-gate the training, artifact reload, accounting, and chat paths |
| `0001-babble.yaml` | 76.4M parameters | 1.60B | Candidate compact model with coherent language and short basic interaction |
| `0002-conversation.yaml` | 336.6M parameters | 12.12B | First validated conversational model |
| `0003-tool-assistant.yaml` | 336.6M parameters | 12.57B | Extend basic conversation with assistant-only response and tool-use training |

## Canary

`0000-canary.yaml` is deliberately too small to assess language quality. It
exists to catch backend, checkpoint, evaluation, accounting, and inference
regressions cheaply.

## Babble

The original 49.9M-parameter babble run proved that the compact `r50k_base`
tokenizer and a roughly 1B-token budget could produce coherent prose. It did
not learn conversational behavior. The replacement is a new candidate, not a
claim that the earlier measurements still apply.

The candidate assigns more capacity to its transformer, expands context to
1,024 tokens, and pretrains on weighted clean books, Wikimedia main content,
and scientific prose. Every stage excludes rows assessed as repetitive or
boilerplate. Two inexpensive assistant-response stages then teach human
dialogue followed by the reviewed interaction contract and high-quality
responses. Its `user-assistant-v1` declaration makes `waldo model chat` format
and retain turns consistently.

## Basic conversation

`0002-conversation.yaml` preserves the recipe that produced WALDO's
first useful conversational result. It builds a 336.6M-parameter language
base, performs broad dialogue adaptation with OASST1/2, Aya, and Dolly, and
then applies the interaction contract and quality-gated HelpSteer2 data. Its
successful result is intentionally described as basic conversation: it can
follow the interaction form but should not be represented as a knowledgeable,
reliable general assistant.

The recipe uses causal dialogue tuning because that is the exact validated
sequence. Future experiments can compare assistant-response-only tuning
without silently changing this reference.

## Tool assistant

`0003-tool-assistant.yaml` is a standalone superset of conversation. A
new model runs the complete sequence. Running it against a compatible model
that already completed the conversation corpora skips those recorded
corpus paths and proceeds to UltraChat, curated Tulu 3, and Hermes function
calling. The final two stages apply loss only to assistant content. WALDO's
runtime remains responsible for actually executing tools.

Hardware is a deployment decision. Use `waldo model forecast` to compare a
compose against the accelerator catalog and locally observed calibration.
WALDO selects the backend from machine-local configuration; compose identity
does not change with hardware.
