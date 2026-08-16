# 0056: Persist a versioned model interaction contract

## Status

Accepted.

## Decision

A model compose may declare `interaction.template: user-assistant-v1`. WALDO
persists this declaration in the immutable model plan, model record, and model
BOM. The chat command uses it to render alternating `User:` and `Assistant:`
turns, retain bounded history, and stop before a generated next-user turn.

The zero value remains raw causal continuation. WALDO does not infer an
interaction format from corpus paths, stage names, or model names, and schema
1 does not accept arbitrary template expressions.

## Consequences

The interaction declaration changes model identity without changing its
architecture or parameter count. Existing models remain valid and retain raw
continuation behavior. A conversational model must be built with the declared
contract for automatic chat formatting.
