# ADR 0042: Preserve compose and advisor history

Status: accepted

## Decision

Each model preserves every distinct compose as ordered YAML beneath
`composes/NNNN-<source-name>.yaml`. The existing `COMPOSE.json` remains the
canonical compatibility record. Reusing the exact compose during transaction
resume does not add a duplicate.

Advisor edits never silently overwrite an existing working compose. WALDO
offers a new numbered working revision or an in-place draft update. New
revision is the default when architecture or base changes, or when the current
draft is already present in archived model history. Archived files themselves
are immutable. Both working and archived revisions put the four-digit ordinal
first so files remain ordered even when contributors choose different compose
names.

`waldo model continue <name>` is authorized only by a retained durable compose
transaction. It loads the latest archived compose, with legacy `COMPOSE.json`
as a fallback, and uses the existing verified-checkpoint resume path. It does
not repeat completed or failed training.

Advisor turns and checkpoint assessments are append-only schema-1 JSONL beneath
`advisor/CHAT.jsonl`. Advisor-started training queues assessments at checkpoint
boundaries on a separate worker. Training never waits for provider latency;
completed assessments are displayed, persisted, and supplied to later chats
with compact run and compose history.

## Consequences

- Compose evolution and AI recommendations remain auditable with the model.
- A numeric prefix gives deterministic human ordering without changing model
  or run BOM identities.
- Interruption recovery remains fail-closed around the retained transaction.
- Monitoring may coalesce checkpoints when the provider is slower than
  training, and provider failure never fails the build.
