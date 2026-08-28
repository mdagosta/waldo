# ADR 0004: Persist the training run state machine

Status: accepted
- Date: 2026-08-04

## Context

A training run is part of model provenance even when it fails or is
interrupted. Recording it only after an external backend exits loses the fact
that checkpoints or weights may have been produced and confuses planned corpus
totals with data actually consumed.

## Decision

Persist a run before launching its backend and move it through explicit states:
`planned`, `running`, and exactly one of `complete`, `failed`, or `interrupted`.

The run separates planned inputs and parameters from backend-observed tokens,
steps, losses, checkpoints, and output hashes. State writes are atomic.

## Consequences

- Interruption and failure remain visible in model history.
- Resume logic has durable state to inspect.
- Trainers return observations but never write the model record.
- Fake-backend tests can exhaustively exercise state transitions before a real
  ML framework is integrated.

