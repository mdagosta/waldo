# ADR 0016: Forecast against a versioned accelerator catalog

## Context

A model builder needs to know which exact accelerator configurations can run a
compose and approximately how long the complete workload will take before
allocating hardware. Generic workstation and server labels do not answer that
question. The result also must not imply precision that the estimator does not
have.

## Decision

`waldo model forecast <compose>` performs the normal read-only compose and corpus
preflight. `waldo model forecast <index-path...>` resolves and deduplicates one
or more selections from the same checkout, recommends a model rung at roughly
20 tokens per parameter, and plans one pass over the selected tokens. Both
forms compare the workload with a versioned catalog of exact Apple, NVIDIA, and
AMD accelerators and create no model or run state.

The memory model includes sharded parameters, gradients, FP32 master weights,
Adam moments, checkpointed activations, a fixed runtime reserve, and ten
percent device headroom. Configurations that do not fit are omitted.

Runtime uses the transparent approximation:

```text
6 * approximate parameters * planned tokens / effective throughput
```

The catalog records conservative effective throughput rather than advertising
peak arithmetic. Multi-GPU entries include an explicit topology scaling
factor, and the result includes eight percent for checkpointing, evaluation,
and final artifact work. Catalog identity, effective throughput, required
memory, raw seconds, and formula remain available in JSON output.

Human output contains only manufacturer, accelerator, GPU count, memory per
GPU, and one approximate time. It is sorted from slowest to fastest. Durations
below 100 hours remain in hours; longer durations are rounded to days.

## Consequences

- A forecast is reproducible for a model compose and catalog revision.
- Unsupported or non-fitting configurations do not clutter the normal table.
- Estimates are useful planning numbers, not benchmark guarantees.
- Hardware measurements can revise the catalog without changing recipes or
  model identity; the catalog revision makes that change visible to automation.

ADR 0032 later permits verified completed local runs to calibrate an exact
accelerator topology while retaining this catalog for every unmatched row.
