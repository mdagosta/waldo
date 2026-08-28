# ADR 0056: Persist a versioned model interaction contract

Status: accepted

## Decision

A schema-1 compose may declare `interaction.template` as
`user-assistant-v1` or `chatml-v1`. The declaration is part of the immutable
model plan, model record, and model BOM. The zero value remains raw causal
continuation.

Conversation training must use the same template as model interaction.
`interaction.tools: true` requires assistant-response training that supervises
assistant messages. Tool definitions, calls, arguments, and results are
rendered deterministically by the selected template.

Chat uses the stored interaction contract and bounded history. WALDO never
infers a template or tool capability from corpus names or model names.
