# ADR 0012: Store interactive S3 credentials in the OS keychain

Status: Accepted

Date: 2026-08-04

## Context

S3 credentials passed as command arguments can be exposed through shell
history and process inspection. Persisting them in WALDO's configuration would
leave long-lived plaintext secrets on disk.

WALDO also needs to work unattended on machines that receive credentials from
environment variables or workload identities.

## Decision

`waldo lookaside login` prompts for an S3 access key and a non-echoed S3 secret
key. It stores both in the operating system's native credential vault, scoped
to the configured bucket. WALDO does not request or persist a session token.

Full credentials must not appear in WALDO configuration, command output, logs,
index manifests, or OpenWALDO BOMs. Status output may show only a redacted
access-key suffix. WALDO provides no plaintext credential-store fallback.

When no WALDO keychain login is present, the internal AWS SDK uses its standard
environment and workload-role credential chain. S3 transport does not invoke
the AWS CLI.

## Consequences

- Interactive credentials use macOS Keychain, Windows Credential Manager, or
  the Linux Secret Service through the system keyring implementation.
- `waldo lookaside logout` removes credentials for the configured bucket.
- Different prefixes in one bucket share a login; a different bucket requires
  a separate login.
- Headless deployments can continue to use short-lived environment or role
  credentials without an interactive login.
