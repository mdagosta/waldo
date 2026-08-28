# ADR 0017: Resolve training backends outside model composes

Status: accepted

## Decision

Model composes describe portable architecture, corpus, objective, and training
facts. They do not contain a backend field.

Before model or run state is created, the training resolver selects MLX,
PyTorch, or TorchTitan from machine-local policy. The fake backend is available
only when explicitly configured for tests. WALDO currently has no TensorFlow
backend.

The resolver returns the adapter and immutable execution facts, including the
backend revision, framework, runtime, host, accelerators, node count, and world
size. Those facts enter the model plan and run BOM.

Adapters receive a backend-neutral request and return observations and
content-addressed artifacts. They do not write lifecycle records or interpret
the index.
