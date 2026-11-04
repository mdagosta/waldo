#!/usr/bin/env python3
# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0
"""
Every training under ~/.waldo, from each run's RUN.json and TELEMETRY.csv, grouped by experiment:

  timings.py [prefix...]          # per experiment: runs, elapsed and wall hours, training hours, tokens
  timings.py --csv [prefix...]    # one row per training
Prefixes keep only the experiments that start with one, as in `timings.py python-local`. Elapsed is
first start to last finish; wall counts the time any training ran, each moment once.
"""
import collections
import csv
import datetime
import json
import pathlib
import re
import sys

ROOT = pathlib.Path.home() / ".waldo"


def when(text):
    # Python 3.9 only parses 3 or 6 fractional digits; seconds are precise enough.
    return datetime.datetime.strptime(text[:19], "%Y-%m-%dT%H:%M:%S").replace(tzinfo=datetime.timezone.utc)


def group(model):
    if "probe" in model or model.endswith("-eval"):
        return "(evaluation probes)"
    for pattern in (r"^waldo-(.+?)-r\d{4}-", r"^(waldito-.+?)-(?:pret|post)-r\d+", r"^(.+?)-r\d+-[abc]-\d+$"):
        match = re.match(pattern, model)
        if match:
            return match.group(1)
    return model


prefixes = tuple(arg for arg in sys.argv[1:] if not arg.startswith("-"))
rows = []
for path in ROOT.rglob("runs/*/RUN.json"):
    if path.parents[2].parent.name != "models":
        continue
    run = json.loads(path.read_text())
    if not run.get("started") or not run.get("finished"):
        continue
    tokens = flops = 0.0
    steps = 0
    telemetry = path.with_name("TELEMETRY.csv")
    if telemetry.exists():
        with telemetry.open() as stream:
            for row in csv.DictReader(stream):
                if row.get("event") == "progress" and row.get("tokens"):
                    tokens, steps = float(row["tokens"]), int(row["step"])
                    flops = float(row.get("training_flops") or 0) or flops
    model = path.parents[2].name
    if prefixes and not group(model).startswith(prefixes):
        continue
    rows.append({"model": model, "group": group(model), "host": path.parents[3].parent.name if "join" in path.parts else "single",
                 "state": run["state"], "started": when(run["started"]), "finished": when(run["finished"]),
                 "tokens": tokens, "steps": steps, "flops": flops})

if "--csv" in sys.argv:
    out = csv.writer(sys.stdout)
    out.writerow(["group", "model", "host", "state", "started", "finished", "seconds", "steps", "tokens", "training_flops"])
    for row in sorted(rows, key=lambda row: row["started"]):
        out.writerow([row["group"], row["model"], row["host"], row["state"], row["started"].isoformat(), row["finished"].isoformat(),
                      round((row["finished"] - row["started"]).total_seconds()), row["steps"], int(row["tokens"]), row["flops"]])
    sys.exit()

groups = collections.defaultdict(list)
for row in rows:
    groups[row["group"]].append(row)


def hours(seconds):
    return f"{seconds / 3600:6.2f}"


print(f"{'experiment':42} {'runs':>4} {'first start (UTC)':17} {'elapsed h':>9} {'wall h':>6} {'train h':>7} {'Mtokens':>8}")
total_wall = total_train = total_tokens = 0.0
for name, items in sorted(groups.items(), key=lambda item: min(row["started"] for row in item[1])):
    start, end = min(row["started"] for row in items), max(row["finished"] for row in items)
    train = sum((row["finished"] - row["started"]).total_seconds() for row in items)
    # Wall time counts each moment once even when several bots trained at the same time.
    spans, wall = sorted((row["started"], row["finished"]) for row in items), 0.0
    current = None
    for begin, finish in spans:
        if current and begin <= current[1]:
            current[1] = max(current[1], finish)
        else:
            if current:
                wall += (current[1] - current[0]).total_seconds()
            current = [begin, finish]
    wall += (current[1] - current[0]).total_seconds()
    tokens = sum(row["tokens"] for row in items)
    total_wall, total_train, total_tokens = total_wall + wall, total_train + train, total_tokens + tokens
    print(f"{name[:42]:42} {len(items):4} {start:%Y-%m-%d %H:%M} {hours((end - start).total_seconds()):>9} {hours(wall)} {hours(train):>7} {tokens / 1e6:8.1f}")
print(f"{'total':42} {len(rows):4} {'':17} {'':>9} {hours(total_wall)} {hours(total_train):>7} {total_tokens / 1e6:8.1f}")
