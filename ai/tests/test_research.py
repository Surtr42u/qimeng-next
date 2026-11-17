import asyncio
import sys
import tempfile
import unittest

from qimeng_ai.config import Config
from qimeng_ai.errors import ToolError
from qimeng_ai.research import _process
from qimeng_ai.state import State


class ResearchBoundary(unittest.TestCase):
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
