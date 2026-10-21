#!/usr/bin/env python3
"""Compare rung-0004 likelihood evidence with paired uncertainty."""

from __future__ import annotations

import json
import math
import random
import sys
from pathlib import Path


def load(path: str) -> dict[str, object]:
    return json.loads(Path(path).read_text())


def bpb(rows: list[dict[str, object]]) -> float:
    return sum(row["nll"] for row in rows) / math.log(2) / sum(row["target_bytes"] for row in rows)


def main() -> None:
    if len(sys.argv) != 7:
        raise SystemExit(
            f"usage: {sys.argv[0]} CANDIDATE_MATCHED CANDIDATE_MISMATCHED "
            "BASE_MATCHED BASE_MISMATCHED UNTRAINED_MATCHED UNTRAINED_MISMATCHED"
        )
    candidate, candidate_wrong, base, base_wrong, untrained, untrained_wrong = map(load, sys.argv[1:])
    candidate_rows = candidate["results"]
    base_rows = base["results"]
    if len(candidate_rows) != len(base_rows):
        raise SystemExit("candidate and baseline suites differ in length")

    rng = random.Random(20261016)
    paired = []
    for _ in range(10000):
        indices = [rng.randrange(len(candidate_rows)) for _ in candidate_rows]
        paired.append(
            bpb([candidate_rows[index] for index in indices])
            - bpb([base_rows[index] for index in indices])
        )
    paired.sort()
    delta = candidate["bits_per_byte"] - base["bits_per_byte"]
    candidate_gap = candidate_wrong["bits_per_byte"] - candidate["bits_per_byte"]
    base_gap = base_wrong["bits_per_byte"] - base["bits_per_byte"]
    untrained_gap = untrained_wrong["bits_per_byte"] - untrained["bits_per_byte"]
    form_regressions = {
        form: candidate["forms"][form]["bits_per_byte"] - base["forms"][form]["bits_per_byte"]
        for form in candidate["forms"]
    }
    explanation_gain = 1 - (
        candidate["forms"]["explanation"]["bits_per_byte"]
        / base["forms"]["explanation"]["bits_per_byte"]
    )
    gates = {
        "paired_bpb_improvement": paired[9749] < 0,
        "context_coherence": candidate_gap >= 0.25,
        "untrained_control": abs(untrained_gap) <= 0.05,
        "explanation_improvement": explanation_gain >= 0.25,
        "form_regression_bound": max(form_regressions.values()) <= 0.15,
    }
    print(json.dumps({
        "candidate_bpb": candidate["bits_per_byte"],
        "baseline_bpb": base["bits_per_byte"],
        "paired_delta_bpb": delta,
        "paired_bootstrap_95": [paired[249], paired[9749]],
        "probability_candidate_better": sum(value < 0 for value in paired) / len(paired),
        "coherence_advantage": {
            "candidate": candidate_gap,
            "baseline": base_gap,
            "untrained": untrained_gap,
        },
        "explanation_fractional_improvement": explanation_gain,
        "form_delta_bpb": form_regressions,
        "gates": gates,
        "passed": all(gates.values()),
    }, indent=2))


if __name__ == "__main__":
    main()
