# ADR 0013: Remove lookaside objects only by explicit name

Status: Accepted

Date: 2026-08-04

## Context

An index-free garbage collector cannot know whether a published object is
still referenced by another index, a historical Git revision, or an exported
OpenWALDO BOM. Inferring reachability from incomplete inputs could permanently
delete valid training data.

## Decision

WALDO provides `waldo lookaside rm <sha256>...` instead of index-free garbage
collection. Every object must be named by its complete lowercase SHA-256. The
command operates only on the configured writable lookaside and confirms that
the entire list exists before deleting any object.

The command does not accept URLs, prefixes, globs, or inferred unreferenced
sets. Choosing the objects to remove remains an explicit caller decision.

## Consequences

- WALDO never guesses that a published object is garbage.
- A typographical error cannot cause partial deletion during preflight.
- A transport failure during deletion can still produce a partial result, and
  the command reports the object where it stopped.
- Higher-level index-aware cleanup can be designed separately if a complete,
  auditable set of protected references becomes available.
