# ADR 0020: Select training backends from host policy

Status: accepted
- Date: 2026-08-05

## Context

Portable model composes intentionally do not name a machine framework. A bare
`auto` setting therefore needs one predictable interpretation on every host,
and a missing dependency must not create durable run state or silently enter
simulation. Framework installation and WALDO adapter availability are separate
facts: finding a Python distribution does not prove that WALDO can execute it.

## Decision

`model.backend` remains `auto` by default. On macOS, automatic selection always
chooses MLX; MLX itself requires Apple Silicon and a usable native Python
runtime. On Linux, WALDO probes for TorchTitan first and PyTorch second. It does
not fall back from a detected but broken preferred installation, because doing
so would hide an operator problem. Explicit `mlx`, `torchtitan`, and `pytorch`
settings bypass host preference but still require platform compatibility,
installation, and an included WALDO execution adapter. `fake` remains explicit
and is never selected automatically.

Host selection is a resolver above the backend-neutral training contract.
Framework adapters register beneath it. Resolution happens before a run ID or
run directory is created. Failures write a warning and return an error with an
official installation location. A framework that is installed but lacks a
WALDO adapter is reported separately from a missing framework.

## Consequences

- A default command has deterministic behavior without putting machine policy
  into a portable compose.
- TorchTitan wins on Linux whenever both TorchTitan and PyTorch are installed.
- Missing dependencies and missing WALDO adapters cannot be mistaken for a
  completed or planned training run.
- Adding PyTorch, TorchTitan, or another framework does not change model-domain
  persistence; it registers another resolver and backend adapter.
