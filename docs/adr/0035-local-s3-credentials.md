# ADR 0035: Store interactive S3 credentials under the WALDO home

Status: accepted

## Context

WALDO already uses `~/.waldo` for durable user-owned models and its verified
object cache. Operators also expect an interactive WALDO login to work the
same way on a laptop and a headless ingestion worker.

## Decision

`waldo lookaside login` stores the validated, bucket-scoped access and secret
keys in `~/.waldo/credentials`. The containing directory is mode `0700`; the
file is mode `0600`, installed atomically, and rejected if it is a symlink,
not a regular file, or readable by group or others. The credential document
has schema 1 and records the bucket scope so credentials are never silently
used for a different bucket. Logging into another bucket replaces the one
interactive login; changing prefixes within a bucket does not.

Credentials remain separate from WALDO configuration and must never appear in
command arguments, normal output, logs, manifests, or OpenWALDO BOMs. Status
may show the credential path, bucket scope, and redacted access-key suffix.
`waldo lookaside logout` removes the matching scoped credential file.

When the WALDO credential file is absent, the internal AWS SDK retains its
standard environment, shared-file, and workload-identity chain. S3 transport
does not invoke the AWS CLI and WALDO does not persist a session token.

## Consequences

- Interactive login behaves consistently on desktop and headless systems.
- The credential file is plaintext protected by filesystem ownership and
  permissions, like the standard AWS shared credential file. Users must
  protect and exclude their home directory from publication and backups that
  do not handle secrets safely.
- A malformed or over-permissive credential file fails closed instead of
  silently falling through to another provider.
- Failed S3 validation preserves the previously stored login because WALDO
  writes only after the probe succeeds.
