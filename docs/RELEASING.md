# Opening WALDO for collaboration

The goal is to publish WALDO early, iterate in public, and make it easy for
other people to participate. This is not a stable-release or binary-
distribution checklist.

## Minimum publication gates

1. CI runs the real formatting, vet, unit, and portable end-to-end checks.
2. The source and Git history receive a focused review for credentials,
   private material, and files that cannot be redistributed.
3. The README describes the project as early development and directs people
   to a usable contribution path.
4. Documentation distinguishes current behavior from plans and design history.
5. A private security-reporting contact is available before inviting broad
   external testing.

## Collaboration setup

- Add GitHub issue and pull-request templates that ask for reproducible,
  focused changes without imposing a heavyweight process.
- Label good first issues and areas where design feedback is especially useful.
- State which maintainers can review and merge changes.
- Keep the roadmap short; track active work and proposals in public issues.
- Release changes frequently from source while interfaces are still evolving.

## Explicitly deferred

- Stable API or format guarantees beyond `docs/COMPATIBILITY.md`.
- Binary packaging and distribution.
- Formal release trains, long-term support commitments, and platform support
  guarantees.
- A code of conduct.

## Decisions reserved for discussion with the project owner

No legal or ownership files should be changed without explicit discussion and
approval. This includes copyright ownership and notices, licensing changes,
trademarks, governance terms with legal effect, and DCO-versus-CLA policy.

## Current status

- Present: Apache-2.0 `LICENSE`, `NOTICE`, concise project documentation,
  package tests, passing portable end-to-end tests, cross-platform CI
  configuration, and lightweight GitHub collaboration templates.
- Corrected during this preparation: the local binary ignore rule, stale BOM
  commands in E2E tests, and the obsolete CI script path.
- A preliminary source/history scan found no common credential patterns,
  private-looking URLs, user-specific paths, or historical blobs at least
  1 MiB. The owner still needs to confirm contributor email visibility and
  redistributability assumptions.
- Still needed before publication: a private security contact and an owner
  decision about the initial legal posture.
