#!/usr/bin/env python3

"""Build the deterministic, narrow supervised corpus for ladder rung 0003."""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path


NAMES = (
    "Alice", "Ben", "Clara", "Daniel", "Emma", "Felix", "Grace", "Henry",
    "Iris", "Jack", "Kira", "Leo", "Maya", "Noah", "Olive", "Peter",
    "Ruby", "Sam", "Tina", "Victor", "Wendy", "Yara", "Zoe", "Liam",
)
OBJECTS = (
    "ball", "book", "box", "cup", "doll", "flower", "hat", "kite",
    "key", "lamp", "map", "pencil", "rock", "shell", "shoe", "spoon",
    "star", "toy", "train", "umbrella",
)
PLACES = (
    "basket", "bedroom", "bench", "cabinet", "drawer", "garden", "kitchen",
    "porch", "shelf", "table", "tree", "wagon", "yard", "closet", "desk",
    "playroom",
)
COLORS = (
    "blue", "brown", "gold", "green", "orange", "pink", "purple", "red",
    "silver", "white", "yellow", "black",
)
CAUSES = (
    ("went inside", "it started to rain"),
    ("opened the window", "the room was warm"),
    ("shared the snack", "her friend was hungry"),
    ("picked up the toy", "it was on the floor"),
    ("wore a coat", "the wind was cold"),
    ("turned on the lamp", "the room was dark"),
    ("filled the cup", "he was thirsty"),
    ("closed the gate", "the puppy was outside"),
)


def pick(values: tuple[str, ...], index: int, stride: int) -> str:
    return values[(index * stride + index // len(values)) % len(values)]


def example(index: int) -> dict[str, object]:
    name = pick(NAMES, index, 5)
    other = pick(NAMES, index + 7, 11)
    if other == name:
        other = NAMES[(NAMES.index(name) + 1) % len(NAMES)]
    obj = pick(OBJECTS, index, 7)
    place = pick(PLACES, index, 9)
    color = pick(COLORS, index, 5)
    task = index % 5

    if task == 0:
        prompt = (
            f"Read this tiny story. {name} put the {color} {obj} in the {place}. "
            f"Where is the {obj}? Answer with only the place."
        )
        response = f"The {place}."
        category = "place-extraction"
    elif task == 1:
        prompt = (
            f"Read this tiny story. {name} gave {other} the {color} {obj} because "
            f"{other} needed it. Who received the {obj}? Answer with only the name."
        )
        response = f"{other}."
        category = "recipient-extraction"
    elif task == 2:
        asked_color = color if (index // 5) % 2 == 0 else COLORS[(COLORS.index(color) + 3) % len(COLORS)]
        prompt = (
            f"Read this tiny story. {name} carried a {color} {obj} to the {place}. "
            f"Was the {obj} {asked_color}? Answer yes or no."
        )
        response = "Yes." if asked_color == color else "No."
        category = "yes-no"
    elif task == 3:
        action, reason = CAUSES[(index // 5) % len(CAUSES)]
        prompt = (
            f"Read this tiny story. {name} {action} because {reason}. "
            f"Why did {name} {action}? Answer with only the reason."
        )
        response = f"Because {reason}."
        category = "explicit-cause"
    else:
        sentence = f"{name} carried the {color} {obj} to the {place}."
        prompt = f'Rewrite this sentence by replacing {name} with {other}: "{sentence}"'
        response = f"{other} carried the {color} {obj} to the {place}."
        category = "one-name-rewrite"

    return {
        "id": f"{category}-{index:06d}",
        "task": category,
        "messages": [
            {"role": "user", "content": prompt},
            {"role": "assistant", "content": response},
        ],
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("output", type=Path)
    parser.add_argument("--records", type=int, default=80_000)
    args = parser.parse_args()
    if args.records < 1:
        raise SystemExit("--records must be positive")

    raw = args.output / "raw"
    raw.mkdir(parents=True, exist_ok=True)
    data = raw / "training.jsonl"
    with data.open("w", encoding="utf-8", newline="\n") as stream:
        for index in range(args.records):
            stream.write(json.dumps(example(index), ensure_ascii=False, separators=(",", ":")))
            stream.write("\n")

    payload = data.read_bytes()
    digest = hashlib.sha256(payload).hexdigest()
    evidence = f"{digest}\t{len(payload)}\ttraining.jsonl\n".encode()
    tree_digest = hashlib.sha256(evidence).hexdigest()
    manifest = {
        "kind": "waldo-source-directory",
        "schema": 1,
        "retrieved_at": "2026-10-15T00:00:00Z",
        "corpus": {
            "id": "tiny-grounded-responses-v1",
            "title": "Tiny Grounded Responses v1",
            "description": "Deterministic TinyStories-level grounded response and rewriting examples for ladder rung 0003.",
        },
        "sources": [{
            "id": "tiny-grounded-responses-v1",
            "path": "",
            "license": "Apache-2.0",
            "source": {
                "name": "OpenWALDO Tiny Grounded Responses v1",
                "version": "generator-v1",
                "url": "https://github.com/openwaldo/waldo",
                "category": "public-dataset",
                "content": {"languages": ["en"]},
                "license_evidence": {"declaration": "Apache-2.0"},
            },
            "input": {
                "format": "jsonl",
                "type": "chat-messages",
                "fields": {"id": "id", "meta": {"task": "task"}},
                "messages": {"role": "messages[].role", "content": "messages[].content"},
            },
            "artifacts": [],
        }],
        "fetcher": {"name": "rung-0003-deterministic-generator"},
        "raw": {
            "path": "raw",
            "file_count": 1,
            "byte_count": len(payload),
            "tree_sha256": tree_digest,
        },
    }
    (args.output / "manifest.json").write_text(
        json.dumps(manifest, indent=2, ensure_ascii=False) + "\n", encoding="utf-8"
    )
    print(f"wrote {args.records} records ({len(payload)} bytes) to {args.output}")


if __name__ == "__main__":
    main()
