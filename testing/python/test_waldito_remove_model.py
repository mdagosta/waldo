# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0

# remove_model() must check whether a model was ever trained before calling
# `waldo model rm` -- otherwise every `rm`/`delete` of a yaml-only topic
# (never trained, or already removed) prints waldo's raw
# MODEL.json-not-found error as if something went wrong.

import importlib.machinery
import importlib.util
import io
import os
import unittest
from contextlib import redirect_stderr, redirect_stdout
from unittest import mock

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
WALDITO_PATH = os.path.join(REPO_ROOT, "waldito")

# Unlikely to collide with any real staged compose transaction that
# remove_model() also scans for under the real ~/.waldo/models/.waldo-compose.
FIXTURE_NAME = "waldito-remove-model-test-fixture-6f1e9c"


def load_waldito():
    loader = importlib.machinery.SourceFileLoader("waldito", WALDITO_PATH)
    spec = importlib.util.spec_from_loader("waldito", loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


class RemoveModelTests(unittest.TestCase):
    def setUp(self):
        self.waldito = load_waldito()

    def test_skips_waldo_rm_when_never_trained(self):
        self.waldito.waldo_json = mock.MagicMock(return_value=[])
        with mock.patch.object(self.waldito, "subprocess") as fake_subprocess:
            buffer = io.StringIO()
            with redirect_stdout(buffer):
                self.waldito.remove_model(FIXTURE_NAME)
            fake_subprocess.run.assert_not_called()
        self.assertIn("nothing to remove", buffer.getvalue())

    def test_calls_waldo_rm_when_trained(self):
        self.waldito.waldo_json = mock.MagicMock(return_value=[{"name": FIXTURE_NAME}])
        with mock.patch.object(self.waldito, "subprocess") as fake_subprocess:
            fake_subprocess.run.return_value = mock.MagicMock(
                returncode=0, stdout=f"removed model {FIXTURE_NAME}\n", stderr="")
            buffer = io.StringIO()
            with redirect_stdout(buffer):
                self.waldito.remove_model(FIXTURE_NAME)
            fake_subprocess.run.assert_called_once()
        self.assertIn(f"removed model {FIXTURE_NAME}", buffer.getvalue())

    def test_surfaces_real_failure_when_trained_but_rm_errors(self):
        # Trained per waldo_json, but the rm subprocess itself fails (a race,
        # a permissions issue, ...) -- must not be swallowed.
        self.waldito.waldo_json = mock.MagicMock(return_value=[{"name": FIXTURE_NAME}])
        with mock.patch.object(self.waldito, "subprocess") as fake_subprocess:
            fake_subprocess.run.return_value = mock.MagicMock(
                returncode=1, stdout="", stderr="Error: permission denied")
            buffer = io.StringIO()
            with redirect_stdout(buffer), redirect_stderr(buffer):
                self.waldito.remove_model(FIXTURE_NAME)
        self.assertIn("permission denied", buffer.getvalue())


if __name__ == "__main__":
    unittest.main()
