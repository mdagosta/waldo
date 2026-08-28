# 0063: Forecast the current host by default

Status: accepted

## Decision

`waldo model forecast` reports whether its resolved workload is ready to train
on the current host. It selects MLX, PyTorch, or TorchTitan through the same
environment resolver used by training, compares the resulting execution
topology with the existing memory forecast, and uses exact observed-run or
catalog evidence only when the accelerator topology matches.

The versioned hardware catalog remains available through the explicit
`--compare-hosts` option. A missing local backend or unknown local performance
does not prevent WALDO from producing the portable workload forecast.

`waldo status` owns workload-independent readiness. It reports host resources,
index and lookaside configuration, and whether the production backend resolver
can select a real training harness. A negative readiness result always carries
a reason.

## Consequences

- Forecast output answers the common local question without presenting an
  unrelated hardware grid by default.
- Backend selection cannot drift between status, forecast, and training.
- CPU training may be ready without a duration when no trustworthy throughput
  evidence exists.
- Host comparison remains explicit and reproducible against the versioned
  catalog.
