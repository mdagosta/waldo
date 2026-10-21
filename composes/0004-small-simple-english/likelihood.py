#!/usr/bin/env python3
"""Measure exact conditional bits per byte for a WALDO PyTorch artifact."""

from __future__ import annotations

import argparse
import json
import math
import random
from pathlib import Path

import torch
from torch.nn import functional


def load_runtime(path: Path) -> dict[str, object]:
    source = path.read_text()
    prefix = source.split("\ndef run():", 1)[0]
    namespace = {"__name__": "waldo_likelihood_runtime"}
    exec(compile(prefix, str(path), "exec"), namespace)
    return namespace


def initialize(model, architecture: dict[str, object], nn) -> None:
    torch.manual_seed(42)
    for module in model.modules():
        if isinstance(module, (nn.Embedding, nn.Linear)):
            nn.init.normal_(module.weight, mean=0.0, std=0.02)
        elif module.__class__.__name__ == "RMSNorm":
            nn.init.ones_(module.weight)
    if architecture.get("initialization", "normal") == "depth-scaled":
        residual_std = 0.02 / math.sqrt(2 * len(model.layers))
        for layer in model.layers:
            nn.init.normal_(layer.attention.o_proj.weight, mean=0.0, std=residual_std)
            nn.init.normal_(layer.feed_forward.down.weight, mean=0.0, std=residual_std)


def ratio(rows: list[dict[str, object]]) -> float:
    return sum(row["nll"] for row in rows) / math.log(2) / sum(row["target_bytes"] for row in rows)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--worker", required=True)
    parser.add_argument("--config", required=True)
    parser.add_argument("--weights")
    parser.add_argument("--encoded", required=True)
    parser.add_argument("--untrained", action="store_true")
    parser.add_argument("--device", default="cuda")
    args = parser.parse_args()
    if not args.untrained and not args.weights:
        parser.error("--weights is required unless --untrained is set")

    runtime = load_runtime(Path(args.worker))
    config = json.loads(Path(args.config).read_text())
    architecture = config["architecture"]
    device = torch.device(args.device)
    model = runtime["DecoderLM"](architecture)
    if args.untrained:
        initialize(model, architecture, runtime["nn"])
    else:
        missing, unexpected = model.load_state_dict(runtime["load_safetensors"](args.weights), strict=False)
        if missing or unexpected:
            raise SystemExit(f"weights mismatch: missing={missing}, unexpected={unexpected}")
    model.to(device).eval()

    items = json.loads(Path(args.encoded).read_text())
    rows = []
    with torch.inference_mode():
        for index, item in enumerate(items, 1):
            prompt = item["prompt_tokens"]
            target = item["target_tokens"]
            tokens = prompt + target
            inputs = torch.tensor([tokens[:-1]], dtype=torch.long, device=device)
            logits = model(inputs)[0, len(prompt) - 1 :]
            labels = torch.tensor(target, dtype=torch.long, device=device)
            nll = float(functional.cross_entropy(logits.float(), labels, reduction="sum").cpu())
            rows.append({
                "index": index, "form": item["form"], "nll": nll,
                "target_tokens": len(target), "target_bytes": item["target_bytes"],
            })

    forms = {}
    for form in sorted({row["form"] for row in rows}):
        selected = [row for row in rows if row["form"] == form]
        forms[form] = {"items": len(selected), "bits_per_byte": ratio(selected)}
    rng = random.Random(20261016)
    draws = []
    for _ in range(10000):
        draws.append(ratio([rows[rng.randrange(len(rows))] for _ in rows]))
    draws.sort()
    print(json.dumps({
        "control": "untrained" if args.untrained else "trained",
        "items": len(rows),
        "bits_per_byte": ratio(rows),
        "bootstrap_95": [draws[249], draws[9749]],
        "forms": forms,
        "results": [
            {**row, "bits_per_byte": row["nll"] / math.log(2) / row["target_bytes"]}
            for row in rows
        ],
    }, indent=2))


if __name__ == "__main__":
    main()
