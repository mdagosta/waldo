# ADR 0043: Unify model training entry points

Status: accepted
- Date: 2026-08-09

## Context

`model train` accepted index selections for an existing architecture, while
`model compose` accepted a strict file that could also create the architecture
and execute multiple stages. Both commands ultimately performed the same user
intent: train a named model. A separate command forced users to understand an
implementation distinction and made continued training look like replacement.

## Decision

`waldo model train <name>` is the only training entry point. With one input
whose strict document identity is `waldo-model-compose`, WALDO performs
compose-driven training. Otherwise the inputs are index selections for direct
training; omission selects the entire resolved index. WALDO does not use a file
extension or permissive YAML decoding to infer a compose.

A compose creates the named model when absent. When the model exists, WALDO
compares the complete canonical architecture hash, including tokenizer
identity. A match appends the compose stages to the existing history. A
mismatch fails and recommends a new model name; training never replaces the
model. The durable transaction pins the existing model ID and starting run
ordinal so interruption resumes the same stage without replaying earlier runs.

Direct index training still requires an existing architecture. `--epochs`
applies only to direct training because compose stage budgets are declared in
the file. `--audit` applies to both forms. `model continue` remains the explicit
name-only convenience for a retained interrupted compose transaction.

## Consequences

- Users choose inputs, not separate training commands.
- Existing compatible models can receive ordered follow-up composes without
  changing model identity.
- Architecture changes require a new name and cannot erase model history.
- Compose validation, archival, multi-stage execution, and checkpoint resume
  remain model-domain behavior rather than a separate CLI lifecycle.
