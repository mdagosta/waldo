#!/usr/bin/env python3
"""Score objective interaction diagnostics for the rung 0007 candidate."""

from __future__ import annotations

import json
import re
import sys
from collections import defaultdict
from pathlib import Path


def load_stream(path: Path) -> list[dict[str, object]]:
    payload = path.read_text()
    decoder = json.JSONDecoder()
    rows = []
    offset = 0
    while offset < len(payload):
        while offset < len(payload) and payload[offset].isspace():
            offset += 1
        if offset < len(payload):
            row, offset = decoder.raw_decode(payload, offset)
            rows.append(row)
    return rows


def words(value: str) -> list[str]:
    return re.findall(r"[a-z0-9']+", value.lower())


def token_f1(actual: str, expected: str) -> float:
    actual_words = words(actual)
    expected_words = words(expected)
    if not actual_words or not expected_words:
        return float(actual_words == expected_words)
    available: dict[str, int] = defaultdict(int)
    for word in expected_words:
        available[word] += 1
    overlap = 0
    for word in actual_words:
        if available[word]:
            available[word] -= 1
            overlap += 1
    precision = overlap / len(actual_words)
    recall = overlap / len(expected_words)
    return 2 * precision * recall / (precision + recall) if overlap else 0.0


def normalized(value: str) -> str:
    return " ".join(value.strip().lower().split())


def loops(value: str) -> bool:
    tokens = words(value)
    for width in range(2, 9):
        for start in range(len(tokens) - width * 3 + 1):
            phrase = tokens[start : start + width]
            if tokens[start + width : start + 2 * width] == phrase == tokens[start + 2 * width : start + 3 * width]:
                return True
    return False


def main() -> None:
    if len(sys.argv) != 3:
        raise SystemExit(f"usage: {sys.argv[0]} SUITE.json RESULTS.jsonl")
    suite = json.loads(Path(sys.argv[1]).read_text())
    results = load_stream(Path(sys.argv[2]))
    if len(suite) != 42 or len(results) != len(suite):
        raise SystemExit(f"expected 42 results, got {len(results)}")

    rows = []
    for case, result in zip(suite, results):
        generated = result["result"]["text"]
        rows.append({
            "id": case["id"],
            "category": case["category"],
            "prompt": case["prompt"],
            "expected": case["expected"],
            "actual": generated,
            "finish_reason": result["result"]["finish_reason"],
            "exact": normalized(generated) == normalized(case["expected"]),
            "token_f1": token_f1(generated, case["expected"]),
            "role_leak": bool(re.search(r"(^|\n)(User|Assistant|System|Tool):", generated)),
            "loop": loops(generated),
        })

    by_category = {}
    for category in sorted({row["category"] for row in rows}):
        selected = [row for row in rows if row["category"] == category]
        by_category[category] = {
            "exact": sum(row["exact"] for row in selected),
            "mean_token_f1": sum(row["token_f1"] for row in selected) / len(selected),
        }
    print(json.dumps({
        "responses": len(rows),
        "eos": sum(row["finish_reason"] == "eos" for row in rows),
        "exact": sum(row["exact"] for row in rows),
        "mean_token_f1": sum(row["token_f1"] for row in rows) / len(rows),
        "role_leaks": sum(row["role_leak"] for row in rows),
        "loops": sum(row["loop"] for row in rows),
        "by_category": by_category,
        "results": rows,
    }, indent=2))


if __name__ == "__main__":
    main()
