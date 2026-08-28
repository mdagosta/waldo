# ADR 0016: Forecast against a versioned accelerator catalog

Status: accepted

## Decision

`waldo model forecast` always builds a portable workload forecast. By default
it evaluates that workload against the current host and the backend selected by
the training resolver.

Passing `--compare-hosts` also compares the workload with the versioned Apple,
NVIDIA, and AMD accelerator catalog. The catalog records exact topology,
memory, effective throughput, scaling, and identity. Results use the declared
memory model and the formula recorded in structured output.

Observed complete real runs may replace catalog throughput only for an exact
matching accelerator family and GPU count. Forecasting is read-only and does
not create model or run state.
