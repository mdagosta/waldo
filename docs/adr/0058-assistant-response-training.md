# 0058: Mask non-assistant dialogue targets during SFT

## Status

Accepted.

## Decision

WALDO accepts a `chat-messages` ingestion profile that deterministically renders
ordered system, user, assistant, and tool messages into canonical text. Training
stages may select `assistant-response-modeling`; the tokenizer then emits an
additive `loss_mask` in worker protocol schema 1 and backends compute loss only
for assistant content and a final assistant EOS.

The canonical shard remains the existing text schema. Tool definitions, calls,
and results are preserved as dialogue content; this decision teaches the model
their textual interaction pattern and does not add or replace runtime tool
execution.

## Consequences

Existing causal stages and workers remain readable because `loss_mask` is
optional. Backend revisions change because target accounting and loss behavior
change. Role-formatted dialogue is required for the masked objective and fails
closed when it contains no assistant response.
