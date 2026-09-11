# ADR 0070: Discover Fuzzball multi-node launches

Status: accepted

## Context

WALDO's multi-host TorchTitan launcher required a hostfile and invoked OpenSSH
directly. Fuzzball's generic multi-node implementation instead publishes the
allocated topology and its managed remote-command wrapper through environment
variables. Bypassing that wrapper is not a portable way to launch within a
Fuzzball allocation.

## Decision

When `--hostfile` is absent, `MULTINODE_HOSTLIST_NOSLOTS` automatically selects
multi-host TorchTitan training. WALDO locates the current hostname in that
list as local rank 0, uses `MULTINODE_NODE_IP` as the rendezvous address when
available, and launches remote commands through `MULTINODE_SSH_WRAPPER`. The documented
`MULTINODE_RSH_WRAPPER` alias is also accepted.

An explicit hostfile remains authoritative for topology. A Fuzzball wrapper,
when present, remains authoritative for remote execution even with an explicit
hostfile. Wrapper values are absolute executable paths and are invoked using
Fuzzball's `WRAPPER HOST COMMAND` contract; WALDO does not append OpenSSH
options.

## Consequences

The same `waldo model train` command works inside a Fuzzball generic multi-node
job without synthesizing a hostfile. Existing hostfile and direct-SSH launches
retain their behavior. WALDO continues to discover GPU counts itself and does
not consume the slot-bearing `MULTINODE_HOSTLIST` value.
