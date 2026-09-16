# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0

# Exercises `waldito reset` (model only) and `waldito delete` (yaml + model +
# every export) against throwaway WALDITO_DIR/EXPORT_HOME trees, with
# remove_model() stubbed out so no real `waldo`/subprocess call happens.

import importlib.machinery
import importlib.util
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
WALDITO_PATH = os.path.join(REPO_ROOT, "waldito")


def load_waldito():
    loader = importlib.machinery.SourceFileLoader("waldito", WALDITO_PATH)
    spec = importlib.util.spec_from_loader("waldito", loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


class TeardownTests(unittest.TestCase):
    def setUp(self):
        self.waldito = load_waldito()
        self.tempdir = tempfile.TemporaryDirectory()
        self.addCleanup(self.tempdir.cleanup)
        root = Path(self.tempdir.name)
        self.waldito.WALDITO_DIR = root / "waldito"
        self.waldito.WALDITO_DIR.mkdir(parents=True, exist_ok=True)
        self.waldito.EXPORT_HOME = root / "export"
        self.waldito.EXPORT_HOME.mkdir(parents=True, exist_ok=True)
        self.parser = self.waldito.build_parser()
        self.remove_model = mock.MagicMock()
        self.waldito.remove_model = self.remove_model

    def answer(self, reply):
        self.waldito.input = lambda *_a, **_k: reply

    def run_command(self, *argv):
        args = self.parser.parse_args(list(argv))
        args.func(args)

    def test_reset_removes_only_the_model(self):
        self.answer("y")
        compose = self.waldito.compose_path("demo")
        compose.write_text("kind: waldo-model-compose\n")
        self.run_command("reset", "demo")
        self.remove_model.assert_called_once_with("waldito-demo")
        self.assertTrue(compose.exists())  # reset keeps the yaml

    def test_reset_cancelled_skips_removal(self):
        self.answer("n")
        self.run_command("reset", "demo")
        self.remove_model.assert_not_called()

    def test_delete_removes_yaml_and_every_export_format(self):
        self.answer("y")
        compose = self.waldito.compose_path("demo")
        compose.write_text("kind: waldo-model-compose\n")
        export_dirs = []
        for fmt in self.waldito.EXPORT_FORMATS:
            out = self.waldito.export_dir("demo", fmt)
            out.mkdir(parents=True)
            export_dirs.append(out)
        self.run_command("delete", "demo")
        self.remove_model.assert_called_once_with("waldito-demo")
        self.assertFalse(compose.exists())
        self.assertFalse(self.waldito.export_root("demo").exists())
        for out in export_dirs:
            self.assertFalse(out.exists())

    def test_delete_does_not_touch_a_different_topics_export(self):
        # Sibling directories under EXPORT_HOME, one per topic -- deleting
        # "demo" must not take "demo-2" down with it.
        self.answer("y")
        other = self.waldito.export_dir("demo-2", "ollama")
        other.mkdir(parents=True)
        self.run_command("delete", "demo")
        self.assertTrue(other.exists())

    def test_delete_cancelled_touches_nothing(self):
        self.answer("n")
        compose = self.waldito.compose_path("demo")
        compose.write_text("kind: waldo-model-compose\n")
        self.run_command("delete", "demo")
        self.remove_model.assert_not_called()
        self.assertTrue(compose.exists())


if __name__ == "__main__":
    unittest.main()
