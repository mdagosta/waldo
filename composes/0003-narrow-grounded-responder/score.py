#!/usr/bin/env python3

"""Score the immutable rung-0003 grounded-response suite."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path


EXPECTED = (
    "the cupboard.", "the bucket.", "the attic.", "the pantry.", "the garage.",
    "owen.", "quinn.", "sven.", "wade.", "yuri.",
    "yes.", "no.", "yes.", "no.", "yes.",
    "owen carried the teal marble to the cupboard.",
    "quinn carried the gray ribbon to the bucket.",
    "sven carried the tan coin to the attic.",
    "wade carried the violet brush to the pantry.",
    "yuri carried the cream bell to the garage.",
)


def normalize(text: str) -> str:
    return " ".join(text.strip().lower().split())


def loops(text: str) -> bool:
    words = re.findall(r"[a-z0-9']+", text.lower())
    for width in range(2, 9):
        for start in range(0, len(words) - width * 3 + 1):
            phrase = words[start:start + width]
            if words[start + width:start + 2 * width] == phrase and words[start + 2 * width:start + 3 * width] == phrase:
                return True
    return False


def load_concatenated_json(path: Path) -> list[dict[str, object]]:
    payload = path.read_text()
    decoder = json.JSONDecoder()
    rows = []
    offset = 0
    while offset < len(payload):
        while offset < len(payload) and payload[offset].isspace():
            offset += 1
        if offset == len(payload):
            break
        row, offset = decoder.raw_decode(payload, offset)
        rows.append(row)
    return rows


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit(f"usage: {sys.argv[0]} OUTPUT.jsonl")
    rows = load_concatenated_json(Path(sys.argv[1]))
    if len(rows) != len(EXPECTED):
        raise SystemExit(f"expected {len(EXPECTED)} responses, found {len(rows)}")

    eos = 0
    relevant = 0
    grounded = 0
    loop_count = 0
    results = []
    for index, (row, expected) in enumerate(zip(rows, EXPECTED)):
        result = row["result"]
        text = normalize(result["text"])
        expected_text = normalize(expected)
        stopped = result["finish_reason"] == "eos" and result["tokens"] <= 128
        starts = text.startswith(expected_text)
        exact = text.rstrip(' "') == expected_text
        repeated = loops(text)
        eos += int(stopped)
        relevant += int(starts)
        if index < 15:
            grounded += int(exact)
        loop_count += int(repeated)
        results.append({
            "index": index + 1,
            "expected": expected,
            "text": result["text"],
            "finish_reason": result["finish_reason"],
            "tokens": result["tokens"],
            "boundary_eos": stopped,
            "relevant_start": starts,
            "grounded_exact": exact if index < 15 else None,
            "loop": repeated,
        })

    summary = {
        "responses": len(rows),
        "boundary_eos": eos,
        "relevant_start": relevant,
        "grounded_exact": grounded,
        "loops": loop_count,
        "gates": {
            "boundary_eos": eos >= 18,
            "relevant_start": relevant >= 14,
            "grounded_exact": grounded >= 12,
            "loops": loop_count <= 1,
        },
        "results": results,
    }
    summary["passed"] = all(summary["gates"].values())
    print(json.dumps(summary, indent=2))


if __name__ == "__main__":
    main()
