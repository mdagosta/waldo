# ADR 0059: Separate token-budget and epoch-driven training

## Status

Accepted.

## Context

Compose authors previously had to declare optimizer `steps` even when their
intent was either a fixed pretraining token budget or complete passes over a
fine-tuning dataset. Declaring both steps and epochs duplicated facts and could
request more steps than a finite canonical stream supplied.

A full epoch over a large multi-corpus pretraining selection can also be many
times larger than the intended compute budget. Corpus weights control the
mixture while sources remain active, but do not prevent a complete epoch from
eventually consuming every selected record.

## Decision

Schema-1 composes accept three budget forms:

- `tokens` declares fixed work and cannot be combined with epochs or steps;
- `epochs` without steps consumes every filtered training record for each
  declared pass and derives exact optimizer steps during preflight; and
- `steps` retains the existing fixed-step behavior for compatibility and may
  retain an epoch limit in legacy composes.

WALDO rounds token budgets up to a complete optimizer step. Epoch-derived and
token-derived step counts are persisted in the immutable run BOM. Static
forecasts identify epoch-derived stages rather than inventing their size.

## Consequences

Reference pretraining stages use token budgets, while finite conversational
and post-training stages use epochs. Epoch stages require a complete filtered
stream scan before the learning-rate schedule and run BOM can be finalized.

Existing compose files, active processes, saved compose transactions,
checkpoints, and run BOMs that declare steps retain their original behavior
and identity.
