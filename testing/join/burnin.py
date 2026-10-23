#!/usr/bin/env python3
# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0
"""
Burn-in for waldo model join on GitHub: runs one identity honestly through `waldo model join`
and injects protocol faults along the way, to find what the protocol does not yet handle.

  burnin.py <identity> <run-dir> random        # random faults, at most one per round
  burnin.py <identity> <run-dir> lazy close    # these faults in order, from round 1
  burnin.py report <run-dir>                   # check every injected fault's outcome

Faults (add one here whenever a run finds a new kink):
  lazy        claim a unit slot and submit the round's starting weights untrained
  copy        claim a unit slot and resubmit another identity's weights for that unit
  stale-base  claim a unit slot and submit the round's starting weights as trained from the
              round before's, as a contributor a round behind would
  silent      claim a unit slot and never submit (the claim must expire and be retaken)
  close       claim a closing slot and sign a merge that accepts every submission and
              publishes the first one's weights
  forge       propose a claim signed by a key not in the plan (the verifier must refuse)
  future      propose a claim dated an hour ahead (the verifier must refuse)

Each identity is a `waldo model join` identity in ~/.waldo/join/<identity>/ with a github-token;
RUNS_REPO names the runs repository, JOIN_EMAIL the commits' author, and WALDO the binary
(default: the one built at this checkout's root). run-identities.sh launches it.

Records are signed with the identity's own key and proposed from its own fork with its own
token, exactly as join does. Every fault is logged to <logs>/faults.jsonl, and every error state
met along the way (rate limits, failed passes, verifier refusals of honest records) to
<logs>/events.jsonl, for the report.
FAULT_RATE (default 0.15) is the chance, per pass, that a random identity injects its round's fault.
"""
import collections
import datetime
import json
import os
import pathlib
import random
import re
import subprocess
import sys
import tempfile
import time

REPO = os.environ.get("RUNS_REPO", "")
OWNER_REPO = REPO.removeprefix("https://github.com/").removesuffix(".git")
WALDO = os.environ.get("WALDO", str(pathlib.Path(__file__).resolve().parents[2] / "waldo"))
JOIN = pathlib.Path.home() / ".waldo" / "join"
CREDENTIALS = ["-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential"]
UNIT_FAULTS = ["lazy", "copy", "stale-base", "silent"]
FAULTS = UNIT_FAULTS + ["close", "forge", "future"]


def now(ahead=datetime.timedelta()):
    return (datetime.datetime.now(datetime.timezone.utc) + ahead).strftime("%Y-%m-%dT%H:%M:%S+00:00")


def stage(number):
    return "bootstrap" if number == 0 else "pretrain"


def unit_of(slot):
    return slot.rsplit("-", 1)[0]


class Run:
    """A run as main describes it, read from a clone with git, never checked out."""

    def __init__(self, clone, run_dir, env):
        self.clone, self.run_dir, self.env = clone, run_dir, env
        self.name = pathlib.PurePosixPath(run_dir).name
        self.settled = {}  # decided rounds: records are only ever added, so a decision stands

    def git(self, *args, check=True):
        return subprocess.run(["git", *args], cwd=self.clone, env=self.env, capture_output=True, text=True, check=check).stdout

    def sync(self):
        if not (self.clone / ".git").exists():
            self.clone.parent.mkdir(parents=True, exist_ok=True)
            subprocess.run(["git", "clone", "-q", REPO, str(self.clone)], env=self.env, check=True)
        self.git("fetch", "-q", "origin")

    def plan(self):
        text = self.git("show", f"origin/main:{self.run_dir}/plan.yaml")
        number = lambda key, default: int(m.group(1)) if (m := re.search(rf"^{key}: (\d+)", text, re.M)) else default
        identities = re.search(r"^identities:\n((?:  .*\n|\s*\n)*)", text, re.M).group(1)
        count = len(re.findall(r"^  [^\s#][^:]*:", identities, re.M))
        return {"units": [u.strip() for u in re.search(r"^units: \[(.*)\]", text, re.M).group(1).split(",")],
                "replicas": number("replicas", 1), "closers": min(number("closers", 3), count),
                "rounds": 1 + sum(int(r) for r in re.findall(r"^  rounds: (\d+)", text, re.M))}

    def records(self, number, kind):
        base = f"{self.run_dir}/rounds/round-{number:04d}/{kind}/"
        names = self.git("ls-tree", "--name-only", f"origin/main:{base}", check=False).split()
        return {name: json.loads(self.git("show", f"origin/main:{base}{name}")) for name in names if name.endswith(".yaml")}

    @staticmethod
    def decision(merge):
        """What closers must agree on, as join decides it: weights, units, and published provenance."""
        provenance = sorted(f"{n} {d}" for n, d in merge.get("files", {}).items() if n == "TRAINING-CLOSURE.json" or n.startswith("provenance-"))
        accepted = [f"{a['unit']} {a['identity']} {a['model_sha256']}" for a in merge.get("accepted", [])]
        return "\n".join([str(merge.get("base_sha256") or ""), str(merge.get("method")), str(merge.get("model_sha256"))] + accepted + provenance)

    def votes(self, number, plan):
        """One merge per closing slot, from its earliest holder to land one, in slot order, as join counts them."""
        claims, merges = self.records(number, "claims"), self.records(number, "merges")
        result, every = [], True
        for position in range(1, 1000):
            slot = f"close-{position}"
            attempts = sorted((c["attempt"], c["identity"]) for c in claims.values() if c.get("slot") == slot)
            if not attempts or (position > plan["closers"] and not every):
                return result
            vote = next((merges[f"{identity}.yaml"] for _, identity in attempts if f"{identity}.yaml" in merges), None)
            if vote:
                result.append(vote)
            every = every and vote is not None
        return result

    def decided(self, number, plan):
        if number < 0:
            return None
        if number not in self.settled:
            agreeing = collections.defaultdict(list)
            for vote in self.votes(number, plan):
                agreeing[self.decision(vote)].append(vote)
                if len(agreeing[self.decision(vote)]) == plan["closers"] // 2 + 1:
                    self.settled[number] = agreeing[self.decision(vote)][0]
                    break
        return self.settled.get(number)

    def current(self, plan):
        """The first undecided round, or None when the run is complete."""
        return next((n for n in range(plan["rounds"]) if not self.decided(n, plan)), None)

    def replicas(self, number):
        holders = collections.defaultdict(set)
        for claim in self.records(number, "claims").values():
            holders[unit_of(claim["slot"])].add(claim["identity"])
        result = collections.defaultdict(list)
        for submission in self.records(number, "submissions").values():
            if submission["identity"] in holders[submission["unit"]]:
                result[submission["unit"]].append(submission)
        return result


class Identity:
    def __init__(self, name, run_dir):
        self.name, self.home = name, JOIN / name
        self.email = os.environ.get("JOIN_EMAIL", f"{name}@waldo.invalid")
        self.env = {**os.environ, "GH_TOKEN": (self.home / "github-token").read_text().strip()}
        self.run = Run(self.home / "burnin" / "waldo-builds", run_dir, self.env)
        self.logs = pathlib.Path(os.environ.get("JOIN_LOGS", JOIN / "logs" / self.run.name))

    def log(self, message):
        print(f"!! {self.name}: {message}", flush=True)

    def gh(self, *args):
        """gh, waiting out the API rate limit that every identity of one GitHub account shares."""
        for attempt in range(60):
            result = subprocess.run(["gh", *args], cwd=self.run.clone, env=self.env, capture_output=True, text=True)
            if result.returncode == 0:
                return result.stdout.strip()
            if "rate limit" not in result.stderr.lower():
                break
            if attempt == 0:
                self.event("rate-limit", source="burnin", command=f"gh {args[0]} {args[1] if len(args) > 1 else ''}".strip())
            time.sleep(60)
        raise RuntimeError(f"gh {' '.join(args)}: {result.stderr.strip()}")

    def login(self):
        if not hasattr(self, "_login"):
            self._login = self.gh("api", "user", "--jq", ".login")
        return self._login

    def event(self, kind, **details):
        """Records an error state the run met, as a warning in the log and an entry for the report."""
        entry = {"time": now(), "identity": self.name, "run": self.run.name, "event": kind, **details}
        self.logs.mkdir(parents=True, exist_ok=True)
        with open(self.logs / "events.jsonl", "a") as out:
            out.write(json.dumps(entry) + "\n")
        self.log(f"WARNING {kind}: {details}")

    def note(self, fault, number, **details):
        entry = {"time": now(), "identity": self.name, "run": self.run.name, "round": number, "fault": fault, **details}
        self.logs.mkdir(parents=True, exist_ok=True)
        with open(self.logs / "faults.jsonl", "a") as out:
            out.write(json.dumps(entry) + "\n")
        self.log(f"round {number}: {fault} {details}")

    def propose(self, files, message, key=None):
        """Signs files as one record pull request from this identity's fork; returns (url, merged)."""
        run = self.run
        if "fork" not in run.git("remote").split():
            run.git("remote", "add", "fork", f"https://github.com/{self.login()}/{OWNER_REPO.split('/')[1]}.git")
        branch = f"waldo-join/{run.name}/{self.name}/burnin-{int(time.time())}"
        run.git(*CREDENTIALS, "fetch", "-q", "fork", "main")
        run.git("checkout", "-q", "-B", branch, "fork/main")
        for path, record in files.items():
            target = run.clone / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(json.dumps(record, indent=2) + "\n")
            target.with_name(target.name + ".sig").unlink(missing_ok=True)
            subprocess.run(["ssh-keygen", "-Y", "sign", "-f", str(key or self.home / "signing-key"), "-n", "waldo-join", str(target)],
                           capture_output=True, check=True)
            run.git("add", path, path + ".sig")
        run.git("-c", f"user.name={self.name}", "-c", f"user.email={self.email}", "commit", "-q", "-s", "-m", message)
        ours = {path: run.git("rev-parse", f"{branch}:{path}") for path in files}
        run.git(*CREDENTIALS, "push", "-q", "--force", "fork", branch)
        run.git("checkout", "-q", "--detach", "origin/main")
        url = self.gh("pr", "create", "--repo", OWNER_REPO, "--base", "main", "--head", f"{self.login()}:{branch}", "--title", message,
                      "--body", "A signed record from `waldo model join`; the runs repo's verifier merges it if its checks pass.")
        state, poll = "OPEN", 0
        while state == "OPEN":
            # Watch main with git, which GitHub does not rate limit; ask the API every five minutes.
            time.sleep(10)
            poll += 1
            run.git("fetch", "-q", "origin")
            landed = {path: run.git("rev-parse", f"origin/main:{path}", check=False) for path in files}
            if any(landed.values()):
                state = "MERGED" if landed == ours else "CLOSED"
            elif poll % 30 == 0:
                state = json.loads(self.gh("pr", "view", url, "--json", "state"))["state"]
        run.git(*CREDENTIALS, "push", "-q", "fork", "--delete", branch, check=False)
        run.sync()
        return url, state == "MERGED"

    def claim_record(self, number, slot, created=None):
        return {"kind": "waldo-join-claim", "schema": 1, "run": self.run.name, "round": number, "stage": stage(number),
                "slot": slot, "attempt": 1, "identity": self.name, "created_at": created or now()}

    def claim(self, number, slot):
        path = f"{self.run.run_dir}/rounds/round-{number:04d}/claims/{slot}.yaml"
        return self.propose({path: self.claim_record(number, slot)}, f"{self.run.name} round {number}: {self.name} claims {slot}")

    def free_slots(self, number, plan, units):
        claims = self.run.records(number, "claims")
        mine = {unit_of(c["slot"]) for c in claims.values() if c["identity"] == self.name}
        replicas = self.run.replicas(number)
        return [f"{u}-{n}" for u in units if u not in mine and len(replicas[u]) < plan["replicas"]
                for n in range(1, plan["replicas"] + 1) if f"{u}-{n}.yaml" not in claims]

    def submission(self, number, unit, base, weights, tokens):
        """A submission of this identity's claiming weights as its training of unit from base."""
        return {"kind": "waldo-join-submission", "schema": 1, "run": self.run.name, "round": number, "stage": stage(number),
                "unit": unit, "identity": self.name, "base": base and base["model"], "base_sha256": base and base["model_sha256"],
                "model": weights["model"], "model_sha256": weights["model_sha256"], "bom_sha256": weights["files"].get("BOM.json"),
                "files": weights["files"], "tokens": tokens, "url": weights["url"], "created_at": now()}

    def inject(self, fault, number, plan):
        """Injects one fault in round number; False if its conditions do not hold yet."""
        run = self.run
        base, before = run.decided(number - 1, plan), run.decided(number - 2, plan)
        start = base and base["model_sha256"]
        # Honest work: trained from the round's start, and not the start itself.
        honest = [s for s in run.records(number, "submissions").values() if s.get("base_sha256") == start and s["model_sha256"] != start]
        units = plan["units"] if number > 0 else ["all"]
        if fault in UNIT_FAULTS:
            if fault in ("lazy", "stale-base") and (base is None or not honest or (fault == "stale-base" and number < 2)):
                return False
            slots = self.free_slots(number, plan, units)
            if fault == "copy":
                slots = [s for s in slots if any(h["unit"] == unit_of(s) and h["identity"] != self.name for h in honest)]
            if not slots:
                return False
            slot = random.choice(slots)
            unit = unit_of(slot)
            claim, won = self.claim(number, slot)
            if not won:
                return False
            if fault == "silent":
                self.note(fault, number, slot=slot, claim=claim)
                return True
            if fault == "lazy":
                record = self.submission(number, unit, base, base, honest[0]["tokens"])
            elif fault == "copy":
                source = random.choice([h for h in honest if h["unit"] == unit and h["identity"] != self.name])
                record = self.submission(number, unit, base, source, source["tokens"])
            else:
                # The previous round's merge did train from the round before's: a submission one round behind.
                record = self.submission(number, unit, before, base, honest[0]["tokens"])
            path = f"{run.run_dir}/rounds/round-{number:04d}/submissions/{unit}-{self.name}.yaml"
            pr, merged = self.propose({path: record}, f"{run.name} round {number}: {unit} by {self.name}")
            self.note(fault, number, slot=slot, claim=claim, pr=pr, merged=merged, model_sha256=record["model_sha256"])
            return True
        if fault == "close":
            replicas = run.replicas(number)
            claims = run.records(number, "claims")
            free = [f"close-{n}" for n in range(1, plan["closers"] + 1) if f"close-{n}.yaml" not in claims]
            holding = any(c["identity"] == self.name and c["slot"].startswith("close-") for c in claims.values())
            if any(len(replicas[u]) < plan["replicas"] for u in units) or not free or holding:
                return False
            slot = free[0]
            claim, won = self.claim(number, slot)
            if not won:
                return False
            accepted = [{"unit": s["unit"], "identity": s["identity"], "model": s["model"], "model_sha256": s["model_sha256"],
                         "tokens": s["tokens"]} for u in units for s in replicas[u]]
            first = replicas[units[0]][0]
            record = {"kind": "waldo-join-merge", "schema": 1, "run": run.name, "round": number, "stage": stage(number),
                      "identity": self.name, "method": "weighted-mean-f32-v1", "base": base and base["model"], "base_sha256": start,
                      "accepted": accepted, "dropped": [], "model": first["model"], "model_sha256": first["model_sha256"],
                      "files": first["files"], "url": first["url"], "closed_at": now()}
            path = f"{run.run_dir}/rounds/round-{number:04d}/merges/{self.name}.yaml"
            pr, merged = self.propose({path: record}, f"{run.name} round {number}: merge by {self.name}")
            self.note(fault, number, slot=slot, claim=claim, pr=pr, merged=merged, model_sha256=first["model_sha256"])
            return True
        slots = self.free_slots(number, plan, units)
        if not slots:
            return False
        slot = random.choice(slots)
        path = f"{run.run_dir}/rounds/round-{number:04d}/claims/{slot}.yaml"
        message = f"{run.name} round {number}: {self.name} claims {slot}"
        if fault == "forge":
            with tempfile.TemporaryDirectory() as scratch:
                key = pathlib.Path(scratch) / "key"
                subprocess.run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(key)], check=True)
                pr, merged = self.propose({path: self.claim_record(number, slot)}, message, key=key)
        else:
            pr, merged = self.propose({path: self.claim_record(number, slot, now(datetime.timedelta(hours=1)))}, message)
        self.note(fault, number, slot=slot, pr=pr, merged=merged)
        return True

    def join_once(self):
        """One honest pass of join; returns its last line."""
        process = subprocess.Popen([WALDO, "model", "join", REPO, self.run.run_dir, "--identity", self.name, "--email", self.email, "--once"],
                                   stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        last, tail, limited = "", collections.deque(maxlen=5), False
        for line in process.stdout:
            print(line, end="", flush=True)
            last = line.strip() or last
            tail.append(line.rstrip())
            if "rate limit reached" in line and not limited:
                limited = True
                self.event("rate-limit", source="join")
            if "was closed by the verifier" in line or "did not accept" in line:
                self.event("verifier-refused", source="join", line=line.strip())
        if process.wait():
            # Recorded and retried, as an operator would restart join.
            self.event("join-failed", exit=process.returncode, tail=list(tail))
            time.sleep(60)
        return last

    def burn(self, modes):
        scripted = [m for m in modes if m != "random"]
        rate = float(os.environ.get("FAULT_RATE", "0.15"))
        faulted = set()
        while True:
            self.run.sync()
            plan = self.run.plan()
            number = self.run.current(plan)
            if number is None:
                self.log("run complete")
                return
            if scripted and number >= 1:
                if self.inject(scripted[0], number, plan):
                    scripted.pop(0)
                    continue
                if scripted[0] == "close":
                    # Wait for the round to fill rather than take a closing slot honestly first.
                    time.sleep(15)
                    continue
            elif "random" in modes and number not in faulted and random.random() < rate:
                if self.inject(random.choice(FAULTS), number, plan):
                    faulted.add(number)
                    continue
            last = self.join_once()
            if "is complete" in last:
                return
            if "nothing for" in last:
                time.sleep(30)


def report(run_dir):
    """Checks each injected fault against what the run did with it."""
    run = Run(JOIN / "burnin-report" / "waldo-builds", run_dir, dict(os.environ))
    run.sync()
    plan = run.plan()
    logs = pathlib.Path(os.environ.get("JOIN_LOGS", JOIN / "logs" / run.name))
    faults = [json.loads(line) for line in open(logs / "faults.jsonl")] if (logs / "faults.jsonl").exists() else []
    problems, kinks = [], []
    for number in range(plan["rounds"]):
        decided, merges = run.decided(number, plan), list(run.records(number, "merges").values())
        if decided is None:
            print(f"round {number}: not decided yet ({len(merges)} merges)")
            break
        dissent = sum(run.decision(m) != run.decision(decided) for m in merges)
        print(f"round {number}: decided {decided['model_sha256'][:12]}: {len(decided['accepted'])} accepted, "
              f"{len(decided['dropped'])} dropped, {dissent} dissenting")
        accepted = {(a["unit"], a["identity"]) for a in decided["accepted"]}
        for fault in (f for f in faults if f["round"] == number):
            what = f"{fault['identity']} {fault['fault']} {fault.get('slot', '')}"
            unit = unit_of(fault.get("slot", "-"))
            if fault["fault"] in ("lazy", "copy", "stale-base"):
                if not fault.get("merged"):
                    print(f"  --   {what}: verifier refused the submission")
                elif (unit, fault["identity"]) in accepted:
                    problems.append(f"round {number} {what}: ACCEPTED into the merge")
                else:
                    reasons = [d["reason"] for d in decided["dropped"] if d["unit"] == unit]
                    why = [r["reason"] for r in decided.get("rejected", []) if r["unit"] == unit and fault["identity"] in r["identities"]]
                    print(f"  ok   {what}: not accepted" + (f" ({why[0]})" if why else "") + (f"; unit dropped: {reasons[0]}" if reasons else ""))
                    if reasons:
                        kinks.append(f"round {number} {what}: one bad replica dropped the whole unit ({reasons[0]})")
            elif fault["fault"] == "close":
                if fault.get("merged") and fault["model_sha256"] == decided["model_sha256"]:
                    problems.append(f"round {number} {what}: the dishonest merge DECIDED the round")
                else:
                    print(f"  ok   {what}: outvoted")
            elif fault["fault"] in ("forge", "future"):
                if fault.get("merged"):
                    problems.append(f"round {number} {what}: the verifier MERGED it")
                else:
                    print(f"  ok   {what}: verifier refused it")
            elif fault["fault"] == "silent":
                retaken = f"{fault['slot']}.2.yaml" in run.records(number, "claims")
                print(f"  ok   {what}: " + ("retaken after expiry" if retaken else "never retaken; the round closed without it"))
    print(f"\n{len(faults)} faults injected: {len(problems)} problems, {len(kinks)} kinks")
    for line in problems + kinks:
        print("  " + line)
    events = [json.loads(line) for line in open(logs / "events.jsonl")] if (logs / "events.jsonl").exists() else []
    counts = collections.Counter((e["event"], e["identity"]) for e in events)
    print(f"\n{len(events)} error states met")
    for (kind, identity), count in sorted(counts.items()):
        print(f"  {kind:18} {identity:20} {count}")
    for e in events:
        if e["event"] == "join-failed":
            print(f"  {e['time']} {e['identity']} join failed: {(e.get('tail') or [''])[-1]}")
    return 1 if problems else 0


if __name__ == "__main__":
    if not OWNER_REPO.count("/") == 1:
        sys.exit("set RUNS_REPO to the runs repository, as in https://github.com/<owner>/<runs>")
    if sys.argv[1] == "report":
        sys.exit(report(sys.argv[2].rstrip("/")))
    Identity(sys.argv[1], sys.argv[2].rstrip("/")).burn(sys.argv[3:] or ["random"])
