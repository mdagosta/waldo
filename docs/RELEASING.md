# Open-source release plan

WALDO has an Apache-2.0 license and public-oriented source structure, but it is
not ready for a supported public release. Complete these gates in order.

## 1. Ownership and project policy

- Confirm that CtrlIQ and the named contributors authorize publication under
  Apache-2.0 and that `NOTICE` is complete.
- Decide project governance, maintainer roles, trademark usage, and whether
  contributions use DCO sign-off or a CLA.
- Publish a code of conduct, contribution policy, support expectations, and a
  private security-reporting channel.

## 2. Repository hygiene and security

- Review the full Git history for credentials, private URLs, proprietary
  material, large binaries, and personal email addresses before publication.
- Keep local build products such as `/waldo` ignored.
- Add automated secret scanning, dependency review, vulnerability scanning,
  and dependency-update automation.
- Confirm that examples and tests contain only redistributable fixtures.

## 3. Documentation

- Treat `waldo <command> --help` as the command source of truth.
- Keep the root README short and maintain user, contributor, testing, and
  release documentation under `docs/`.
- Reconcile the website and related `waldo-index` and `waldo-fetchers`
  repositories with the same terminology and support status.
- Add migration guidance only for compatibility promises the project intends
  to support.

## 4. Quality gates

- Fix `.github/workflows/ci.yml`; it currently invokes the nonexistent
  `./scripts/e2e/ingest-smoke.sh` path.
- Replace stale top-level `waldo bom` calls in end-to-end tests with the
  supported `waldo index` or `waldo model` command.
- Require formatting, vet, unit tests, and portable end-to-end tests on Linux
  and macOS. Keep accelerator and live-service tests as explicit qualified
  gates.
- Add a supported Go vulnerability scan and document the response process.

## 5. Distribution

- Define versioning, release branches, changelog policy, and supported Go and
  operating-system versions.
- Produce reproducible checksummed binaries for supported platforms and an
  SBOM/provenance record for each release.
- Sign tags and release artifacts, test installation from a clean machine,
  and publish rollback instructions.

## 6. Launch

- Enable branch protection, required reviews, required CI, issue templates,
  pull-request templates, and release permissions.
- Create a release candidate, run the complete matrix, and perform an external
  documentation and security review.
- Publish the first supported version only after every gate above has an owner
  and recorded result.

## Current audit findings

- Present: Apache-2.0 `LICENSE`, `NOTICE`, Go module metadata, tests, and ADRs.
- Fixed in this cleanup: local `/waldo` binary ignore rule and consolidated
  documentation layout.
- Blocking: broken CI path, stale BOM calls in end-to-end tests, no tagged
  releases, and no public contribution, conduct, security, or support policy.
- Needs an owner decision: copyright authority, governance, DCO/CLA policy,
  supported platforms, and release signing identity.
