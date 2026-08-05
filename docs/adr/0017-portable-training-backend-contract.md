# ADR 0017: Resolve training backends outside portable model recipes

## Context

The first real trainer will use MLX, but model composition must not inherit MLX
types or require a framework choice. The same lifecycle must later support
PyTorch, TensorFlow, and distributed engines such as TorchTitan without
duplicating plans, run state, provenance, or CLI workflows.

## Decision

Schema-1 model recipes describe the model, verified corpus stages, and portable
training parameters. They do not contain a backend field.

Before any model state is written, the model domain asks a training resolver to
select an adapter for the current environment. The resolver returns:

- a backend implementing the narrow execution interface; and
- immutable execution facts including adapter identity, framework, runtime,
  host operating system and architecture, accelerators, node count, and world
  size.

Those resolved facts enter the immutable model plan and every run OpenWALDO
BOM. A resolver result is rejected if its execution identity differs from the
adapter descriptor, required host facts are absent, or the adapter does not
declare support for every planned objective.

The backend-neutral request contains the architecture's canonical JSON and
hash, stage and objective, verified corpus BOM and local shard inputs, portable
training parameters, a private artifact directory, and a progress-event sink.
The adapter returns observations and content-addressed artifact descriptions.
It never writes model plans, run state, or OpenWALDO BOMs.

MLX, PyTorch, and TensorFlow are peer adapters. TorchTitan is a distributed
adapter in the PyTorch ecosystem, not a separate model lifecycle. Framework
specific configuration is resolved inside an adapter from the portable plan;
it is not added to the shared recipe merely because the first implementation
needs it.

## Consequences

- One recipe can run on multiple supported environments.
- Framework selection and machine facts remain auditable without polluting the
  portable source configuration.
- Adding TensorFlow or another backend does not change lifecycle persistence or
  command organization.
- Backend capability mismatches fail during preflight, before a model directory
  or paid training run exists.
- Until a real adapter lands, the default development resolver selects the
  deterministic fake backend through the same contract.
