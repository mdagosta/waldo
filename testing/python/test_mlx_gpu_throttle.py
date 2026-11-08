# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0

# internal/training/workers/mlx.py cannot be imported directly: it depends on
# mlx (Apple Silicon + Metal only) and its module-level run() blocks on
# stdin. read_gpu_throttle() has neither dependency, so this pulls just that
# function (plus the emit() and PROTOCOL_SCHEMA it uses) out of the real
# source via ast and executes it in isolation, keeping this test runnable
# anywhere with only the standard library.

import ast
import io
import json
import math
import os
import unittest
from contextlib import redirect_stdout

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
MLX_WORKER_PATH = os.path.join(REPO_ROOT, "internal", "training", "workers", "mlx.py")

WANTED_NAMES = {"PROTOCOL_SCHEMA", "emit", "read_gpu_throttle"}


def load_gpu_throttle_module():
    with open(MLX_WORKER_PATH, encoding="utf-8") as source_file:
        tree = ast.parse(source_file.read(), filename=MLX_WORKER_PATH)

    extracted = [
        node for node in tree.body
        if (isinstance(node, ast.FunctionDef) and node.name in WANTED_NAMES)
        or (isinstance(node, ast.Assign) and any(
            isinstance(target, ast.Name) and target.id in WANTED_NAMES for target in node.targets
        ))
    ]
    found = {node.name if isinstance(node, ast.FunctionDef) else node.targets[0].id for node in extracted}
    if found != WANTED_NAMES:
        raise AssertionError(f"expected to extract {WANTED_NAMES} from {MLX_WORKER_PATH}, found {found}")

    module_ast = ast.Module(body=extracted, type_ignores=[])
    ast.fix_missing_locations(module_ast)
    namespace = {"math": math, "os": os, "json": json}
    exec(compile(module_ast, MLX_WORKER_PATH, "exec"), namespace)
    return namespace


class ReadGPUThrottleTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.namespace = load_gpu_throttle_module()

    def setUp(self):
        self.addCleanup(os.environ.pop, "WALDO_GPU_THROTTLE", None)

    def set_env(self, value):
        if value is None:
            os.environ.pop("WALDO_GPU_THROTTLE", None)
        else:
            os.environ["WALDO_GPU_THROTTLE"] = value

    def read_gpu_throttle(self):
        buffer = io.StringIO()
        with redirect_stdout(buffer):
            value = self.namespace["read_gpu_throttle"]()
        return value, buffer.getvalue()

    def test_unset(self):
        self.set_env(None)
        value, emitted = self.read_gpu_throttle()
        self.assertEqual(value, 1)
        self.assertEqual(emitted, "")

    def test_empty(self):
        # An env var can't be null, only unset (test_unset) or empty. "" is
        # falsy, so `or 1` catches it before the try/except even runs --
        # a different code path than unset, same as no code path exists for
        # None here at all.
        self.set_env("")
        value, emitted = self.read_gpu_throttle()
        self.assertEqual(value, 1)
        self.assertEqual(emitted, "")

    def test_text(self):
        self.set_env("non-numeric text")
        value, emitted = self.read_gpu_throttle()
        self.assertEqual(value, 1)
        self.assertIn("WALDO_GPU_THROTTLE must be a decimal", emitted)

    def test_valid(self):
        self.set_env("0.25")
        value, emitted = self.read_gpu_throttle()
        self.assertEqual(value, 0.25)
        self.assertEqual(emitted, "")

    def test_min(self):
        self.set_env("0.01")
        value, emitted = self.read_gpu_throttle()
        self.assertEqual(value, 0.01)
        self.assertEqual(emitted, "")

    def test_max(self):
        self.set_env("1.0")
        value, emitted = self.read_gpu_throttle()
        self.assertEqual(value, 1.0)
        self.assertEqual(emitted, "")

    def test_zero(self):
        self.set_env("0")
        value, emitted = self.read_gpu_throttle()
        self.assertEqual(value, 1)
        self.assertIn("WALDO_GPU_THROTTLE must be a decimal", emitted)

    def test_negative(self):
        self.set_env("-0.5")
        value, emitted = self.read_gpu_throttle()
        self.assertEqual(value, 1)
        self.assertIn("WALDO_GPU_THROTTLE must be a decimal", emitted)

    def test_nan(self):
        # float("nan") parses without raising, so this exercises the range
        # check, not the except ValueError branch -- any comparison with NaN
        # is False, so it falls through the same as an out-of-range number.
        self.set_env("nan")
        value, emitted = self.read_gpu_throttle()
        self.assertEqual(value, 1)
        self.assertIn("WALDO_GPU_THROTTLE must be a decimal", emitted)

    def test_inf(self):
        # Also parses without raising; rejected by the upper bound.
        self.set_env("inf")
        value, emitted = self.read_gpu_throttle()
        self.assertEqual(value, 1)
        self.assertIn("WALDO_GPU_THROTTLE must be a decimal", emitted)

    def test_malformed(self):
        self.set_env("not-a-number")
        value, emitted = self.read_gpu_throttle()
        self.assertEqual(value, 1)
        self.assertIn("WALDO_GPU_THROTTLE must be a decimal", emitted)

    def test_too_high(self):
        self.set_env("1.5")
        value, emitted = self.read_gpu_throttle()
        self.assertEqual(value, 1)
        self.assertIn("WALDO_GPU_THROTTLE must be a decimal", emitted)

    def test_tiny(self):
        # Below the 0.01 floor, 1 / GPU_THROTTLE - 1 would otherwise blow up
        # into an effectively infinite per-step sleep.
        self.set_env("1e-300")
        value, emitted = self.read_gpu_throttle()
        self.assertEqual(value, 1)
        self.assertIn("WALDO_GPU_THROTTLE must be a decimal", emitted)


if __name__ == "__main__":
    unittest.main()


class LiveGPUThrottleTest(unittest.TestCase):
    """`waldo config set gpu.throttle` moves the budget under WALDO_GPU_THROTTLE while a worker runs."""

    def load(self, ceiling, configured):
        with open(MLX_WORKER_PATH, encoding="utf-8") as source_file:
            tree = ast.parse(source_file.read(), filename=MLX_WORKER_PATH)
        wanted = {"config_path", "live_config", "live_setting", "live_gpu_throttle", "idle_after", "checkpoint_due"}
        body = [node for node in tree.body
                if (isinstance(node, ast.FunctionDef) and node.name in wanted)
                or (isinstance(node, ast.Assign) and isinstance(node.targets[0], ast.Name) and node.targets[0].id in wanted)]
        module_ast = ast.Module(body=body, type_ignores=[])
        ast.fix_missing_locations(module_ast)
        directory = __import__("tempfile").mkdtemp()
        self.addCleanup(__import__("shutil").rmtree, directory)
        path = os.path.join(directory, "config.json")
        self.addCleanup(os.environ.pop, "WALDO_CONFIG", None)
        os.environ["WALDO_CONFIG"] = path
        clock = {"now": 1000.0, "slept": []}

        class Clock:
            @staticmethod
            def monotonic():
                return clock["now"]

            @staticmethod
            def perf_counter():
                return clock["now"]

            @staticmethod
            def sleep(seconds):
                clock["slept"].append(seconds)
                clock["now"] += seconds
                if clock.get("resume") and len(clock["slept"]) == clock["resume"][0]:
                    self.write(path, clock["resume"][1])

        namespace = {"math": math, "os": os, "json": json, "sys": __import__("sys"), "time": Clock, "GPU_THROTTLE": ceiling}
        exec(compile(module_ast, MLX_WORKER_PATH, "exec"), namespace)
        if configured is not None:
            self.write(path, configured)
        return namespace, clock, path

    @staticmethod
    def write(path, throttle):
        with open(path, "w", encoding="utf-8") as file:
            json.dump({"schema": 1, "gpu": {"throttle": throttle}}, file)

    def test_config_moves_the_budget_under_the_ceiling(self):
        for ceiling, configured, want in ((0.25, 0.1, 0.1), (0.25, 0.5, 0.25), (0.25, None, 0.25), (1, 0.2, 0.2)):
            namespace, _, _ = self.load(ceiling, configured)
            self.assertEqual(namespace["live_gpu_throttle"](), want, (ceiling, configured))

    def test_zero_pauses_until_the_budget_comes_back(self):
        namespace, clock, path = self.load(0.5, 0)
        clock["resume"] = (2, 0.25)  # the second 30s nap finds the budget raised again
        started = clock["now"] - 1.0  # one second of GPU work
        namespace["idle_after"](started)
        naps, idle = clock["slept"][:-1], clock["slept"][-1]
        self.assertEqual(naps, [30, 30])
        # The work's second is idled at the new budget; the minute paused isn't multiplied.
        self.assertAlmostEqual(idle, 1.0 * (1 / 0.25 - 1))

    def test_a_checkpoint_comes_by_steps_or_by_minutes(self):
        namespace, _, _ = self.load(0.25, None)
        due = namespace["checkpoint_due"]
        self.assertTrue(due(150, 150, 0, 60, None))            # every checkpoint_every steps
        self.assertFalse(due(151, 150, 0, 60, None))           # no minutes set: steps only
        self.assertFalse(due(151, 150, 0, 29 * 60, 30))        # 29 minutes since the last save
        self.assertTrue(due(151, 150, 0, 30 * 60, 30))         # 30 minutes: save now
        self.assertFalse(due(151, 0, 0, 99 * 60, 30))          # a plan without checkpoints stays without
