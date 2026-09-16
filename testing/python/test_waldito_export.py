# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0

# waldito has no .py suffix and only stdlib dependencies, so it loads
# directly via SourceFileLoader rather than the ast-extraction dance the
# mlx/pytorch worker tests need for their hardware-only imports.

import importlib.machinery
import importlib.util
import os
import unittest

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
WALDITO_PATH = os.path.join(REPO_ROOT, "waldito")


def load_waldito():
    loader = importlib.machinery.SourceFileLoader("waldito", WALDITO_PATH)
    spec = importlib.util.spec_from_loader("waldito", loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


class ExportFormatTests(unittest.TestCase):
    def setUp(self):
        self.waldito = load_waldito()
        self.parser = self.waldito.build_parser()

    def test_format_defaults_to_ollama(self):
        args = self.parser.parse_args(["export", "vue"])
        self.assertEqual(args.format, "ollama")

    def test_format_accepts_positional_choice(self):
        for fmt in self.waldito.EXPORT_FORMATS:
            args = self.parser.parse_args(["export", "vue", fmt])
            self.assertEqual(args.format, fmt)

    def test_format_rejects_unknown_choice(self):
        with self.assertRaises(SystemExit):
            self.parser.parse_args(["export", "vue", "bogus"])

    def test_export_dir_is_per_topic_then_format(self):
        for fmt in self.waldito.EXPORT_FORMATS:
            path = self.waldito.export_dir("vue", fmt)
            self.assertEqual(path.name, fmt)
            self.assertEqual(path.parent.name, "vue")
            self.assertEqual(path.parent, self.waldito.export_root("vue"))

    def test_export_home_is_separate_from_waldito_dir(self):
        # A yaml is tiny and worth backing up/syncing; exports are large
        # binary weights -- they must never share a tree, or a sweep of one
        # topic's yaml would drag gigabytes of exported weights along.
        self.assertNotEqual(self.waldito.EXPORT_HOME, self.waldito.WALDITO_DIR)
        self.assertFalse(str(self.waldito.export_root("vue")).startswith(str(self.waldito.WALDITO_DIR)))


if __name__ == "__main__":
    unittest.main()
