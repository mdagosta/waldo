# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0

# `waldito list` must print bare topics, not waldito-<topic> model names --
# every other command (create/train/export/reset/delete) takes a topic, so a
# name copied from here should never need the waldito- prefix stripped or,
# worse, get it added again (waldito-waldito-<topic>). It must also keep
# showing a topic (as "empty") after `reset` deletes its trained model but
# leaves the yaml behind, rather than have the topic vanish.

import importlib.machinery
import importlib.util
import io
import os
import tempfile
import unittest
from contextlib import redirect_stdout
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


class ListDisplayTests(unittest.TestCase):
    def setUp(self):
        self.waldito = load_waldito()
        self.tempdir = tempfile.TemporaryDirectory()
        self.addCleanup(self.tempdir.cleanup)
        self.waldito.WALDITO_DIR = Path(self.tempdir.name)
        self.waldito.WALDITO_DIR.mkdir(parents=True, exist_ok=True)
        self.parser = self.waldito.build_parser()

    def run_list(self, models, *topic_arg):
        self.waldito.waldo_json = mock.MagicMock(return_value=models)
        args = self.parser.parse_args(["list", *topic_arg])
        buffer = io.StringIO()
        with redirect_stdout(buffer):
            args.func(args)
        return buffer.getvalue()

    def test_lists_bare_topic_not_prefixed_name(self):
        self.waldito.compose_path("vue").write_text("kind: waldo-model-compose\n")
        output = self.run_list([{"name": "waldito-vue", "state": "complete", "approximate_parameters": 9541632}])
        self.assertIn("vue", output)
        self.assertNotIn("waldito-vue", output)

    def test_filters_out_non_waldito_models(self):
        output = self.run_list([{"name": "some-other-model", "state": "complete", "approximate_parameters": 1234}])
        self.assertNotIn("some-other-model", output)

    def test_single_topic_lookup_also_strips_prefix(self):
        self.waldito.compose_path("vue").write_text("kind: waldo-model-compose\n")
        output = self.run_list([{"name": "waldito-vue", "state": "complete", "approximate_parameters": 9541632}], "vue")
        self.assertIn("vue", output)
        self.assertNotIn("waldito-vue", output)

    def test_yaml_only_topic_shows_as_empty(self):
        # Simulates `waldito reset vue`: the yaml is still there, waldo
        # model list no longer mentions it -- the topic must still show up.
        self.waldito.compose_path("vue").write_text("kind: waldo-model-compose\n")
        output = self.run_list([])
        self.assertIn("vue", output)
        self.assertIn("empty", output)

    def test_compose_filename_has_no_redundant_waldito_prefix(self):
        # The yaml already lives in WALDITO_DIR -- "waldito-vue.yaml" would
        # repeat what the directory itself says.
        self.assertEqual(self.waldito.compose_path("vue").name, "vue.yaml")

    def test_trained_model_without_yaml_still_shown(self):
        # A model can exist without waldito's own yaml (e.g. the yaml was
        # deleted by hand) -- it should still be listed, not silently dropped.
        output = self.run_list([{"name": "waldito-vue", "state": "complete", "approximate_parameters": 9541632}])
        self.assertIn("vue", output)
        self.assertIn("complete", output)


if __name__ == "__main__":
    unittest.main()
