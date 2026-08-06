# ADR 0032: Calibrate exact forecast topologies from observed runs

- Status: accepted
- Date: 2026-08-05

## Context

The versioned hardware catalog makes forecasts reproducible but cannot capture
the user's precise runtime, framework build, thermal behavior, or workload.
WALDO already records immutable execution topology, consumed tokens, model
size, attempts, and timestamps for completed training runs.

## Decision

Before forecasting, WALDO verifies locally managed models and selects complete,
non-simulated accelerator runs with positive consumed-token and active-time
measurements. Training work is estimated using the same six-FLOPs-per-parameter
per-token formula as the catalog. Active time is the sum of bounded execution
attempts, excluding downtime between interrupted and resumed attempts.

Evidence is aggregated by exact accelerator family and GPU count. Aggregate
FLOPs divided by aggregate active seconds becomes measured effective
throughput for only the matching forecast row. WALDO does not extrapolate a
one-GPU observation to four or eight GPUs. Unmatched rows retain the dated
catalog throughput, scaling, and overhead assumptions.

JSON reports the estimate source, run count, aggregate FLOPs, aggregate active
seconds, effective throughput, and a SHA-256 over the ordered contributing run
identities and measurements. Human output states when local observations were
applied.

## Consequences

- Repeated use improves estimates for hardware the operator actually runs.
- Simulations, incomplete runs, CPU runs, malformed timings, and mixed-device
  topologies cannot silently influence forecasts.
- Catalog identity remains present and reproducible for unmeasured hardware.
- Observed estimates are historical measurements, not benchmark guarantees.

