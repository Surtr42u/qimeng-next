import asyncio
import os
import sys
import tempfile
import unittest
from unittest.mock import patch

from qimeng_ai.config import Config
from qimeng_ai.errors import ToolError
from qimeng_ai.research import _process
from qimeng_ai.state import State


class ResearchBoundary(unittest.TestCase):
    def test_media_child_does_not_inherit_executor_or_cloud_credentials(self):
        with patch.dict(os.environ, {"QIMENG_API_TOKEN": "synthetic-secret", "AWS_SECRET_ACCESS_KEY": "synthetic-cloud-secret"}):
            output = asyncio.run(_process([sys.executable, "-c",
                "import os; print(any(name in os.environ for name in ['QIMENG_API_TOKEN', 'AWS_SECRET_ACCESS_KEY']))"], 128, 10))
        self.assertEqual(output.strip(), b"False")

    def test_oversized_child_output_is_killed_and_not_reported_as_success(self):
        with self.assertRaises(ToolError) as raised:
            asyncio.run(_process([sys.executable, "-c", "import sys; sys.stdout.write('x' * 1048576)"], 1024, 10))
        self.assertEqual(raised.exception.code, "RESEARCH_OUTPUT_LIMIT")

    def test_result_reader_cannot_read_file_or_image_reference(self):
        with tempfile.TemporaryDirectory() as root:
            config = Config.load({"QIMENG_API_BASE": "http://127.0.0.1:18481", "QIMENG_API_TOKEN": "synthetic-only", "QIMENG_AI_STATE_DIR": root})
            state = State(config, "synthetic-instance", "synthetic-contract")
            image = state.save({"data": "private-media-fixture"}, kind="image")
            with self.assertRaises(ToolError):
                state.read(image)
            self.assertEqual(state.read(image, kind="image")["data"], "private-media-fixture")

    def test_unchanged_file_refs_do_not_fill_result_quota(self):
        with tempfile.TemporaryDirectory() as root:
            config = Config.load({"QIMENG_API_BASE": "http://127.0.0.1:18481", "QIMENG_API_TOKEN": "synthetic-only", "QIMENG_AI_STATE_DIR": root})
            state = State(config, "synthetic-instance", "synthetic-contract")
            refs = {state.save({"storage": "input", "relative": "same.png", "identity": [1,2,3,4]}, kind="file") for _ in range(150)}
            self.assertEqual(len(refs), 1)
