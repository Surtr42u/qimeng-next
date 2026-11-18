"""Original selection is fixed before any upload; private state is not an input."""
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from qimeng_ai.config import Config
from qimeng_ai.errors import ToolError
from qimeng_ai.files import search
from qimeng_ai.intake import register
from qimeng_ai.state import State


class RecordingTransport:
    def __init__(self):
        self.calls = []
        self.inputs = []

    def call(self, operation, arguments):
        self.calls.append((operation, arguments))
        if operation == "setBatchInputs":
            self.inputs = arguments["body"]["inputs"]
            return {"id": arguments["id"], "manifestState": "sealed", "manifestFingerprint": "fixture",
                    "expectedInputs": len(self.inputs)}
        if operation == "getBatch":
            return {"batch": {"manifestFingerprint": "fixture"},
                    "inputs": [{"id": row["id"], "state": "pending"} for row in self.inputs]}
        raise AssertionError(operation)


def runtime(root: Path):
    source = root / "source"
    source.mkdir()
    config = Config.load({"QIMENG_API_BASE": "http://127.0.0.1:18481", "QIMENG_API_TOKEN": "synthetic-only",
                          "QIMENG_AI_STATE_DIR": str(root / "private"),
                          "QIMENG_AI_STORAGES": json.dumps({"input": str(source)}),
                          "QIMENG_AI_PERMISSIONS": "read,ingest,local.read"})
    return SimpleNamespace(config=config, state=State(config, "synthetic-instance", "synthetic-contract"),
                           transport=RecordingTransport()), source


class OriginalInputManifest(unittest.TestCase):
    def test_directory_registration_covers_complete_ordered_selection_and_stable_retry(self):
        with tempfile.TemporaryDirectory() as directory:
            value, source = runtime(Path(directory))
            (source / "nested").mkdir()
            (source / "b.png").write_bytes(b"synthetic b")
            (source / "nested/a.png").write_bytes(b"synthetic a")
            arguments = {"storage": "input", "batchId": "batch-synthetic", "operationId": "selection-operation"}
            first = register(value, arguments)
            second = register(value, arguments)
            self.assertEqual(first, second)
            self.assertEqual(first["inputCount"], 2)
            self.assertEqual([item["sourceRelativePath"] for item in first["inputs"]], ["b.png", "nested/a.png"])
            self.assertEqual(value.transport.calls[:2], value.transport.calls[2:])
            self.assertEqual(value.transport.calls[0][0], "setBatchInputs")
            self.assertEqual(value.transport.calls[0][1]["body"]["expectedManifestRevision"], 1)

    def test_oversized_selection_is_not_truncated_or_sent(self):
        with tempfile.TemporaryDirectory() as directory:
            value, source = runtime(Path(directory))
            for index in range(101):
                (source / f"{index}.png").write_bytes(b"synthetic")
            with self.assertRaises(ToolError) as error:
                register(value, {"storage": "input", "batchId": "batch-synthetic", "operationId": "too-many-inputs"})
            self.assertEqual(error.exception.code, "INTAKE_LIMIT")
            self.assertEqual(value.transport.calls, [])

    def test_successful_seal_preserves_its_receipt_when_status_cannot_be_read(self):
        with tempfile.TemporaryDirectory() as directory:
            value, source = runtime(Path(directory))
            (source / "selected.txt").write_bytes(b"synthetic text")
            original_call = value.transport.call

            for error in (ToolError("NETWORK_ERROR", "response interrupted"), ToolError("scope_denied", "read scope unavailable", status=403)):
                with self.subTest(code=error.code):
                    def call(operation, arguments):
                        if operation == "getBatch":
                            raise error
                        return original_call(operation, arguments)

                    with patch.object(value.transport, "call", call):
                        result = register(value, {"storage": "input", "batchId": "batch-synthetic", "operationId": "known-seal-result"})
                    self.assertEqual(result["batch"]["manifestFingerprint"], "fixture")
                    self.assertEqual(result["reportReadError"]["code"], error.code)
                    self.assertNotIn("state", result["inputs"][0])

    def test_changed_source_reference_cannot_redefine_the_selection(self):
        with tempfile.TemporaryDirectory() as directory:
            value, source = runtime(Path(directory))
            path = source / "a.png"
            path.write_bytes(b"original synthetic")
            ref = search(value, {"storage": "input"})["items"][0]["fileRef"]
            path.write_bytes(b"different synthetic content")
            with self.assertRaises(ToolError) as error:
                register(value, {"fileRefs": [ref], "batchId": "batch-synthetic", "operationId": "changed-input"})
            self.assertEqual(error.exception.code, "FILE_REF_STALE")
            self.assertEqual(value.transport.calls, [])

    def test_explicit_repeated_selection_is_not_silently_deduplicated(self):
        with tempfile.TemporaryDirectory() as directory:
            value, source = runtime(Path(directory))
            (source / "same.png").write_bytes(b"synthetic")
            ref = search(value, {"storage": "input"})["items"][0]["fileRef"]
            result = register(value, {"fileRefs": [ref, ref], "batchId": "batch-synthetic", "operationId": "repeated-selection"})
            self.assertEqual(result["inputCount"], 2)
            self.assertNotEqual(result["inputs"][0]["inputId"], result["inputs"][1]["inputId"])

    def test_private_executor_state_cannot_be_made_a_source(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for source, private in ((root, root / "state"), (root / "media", root)):
                with self.assertRaises(ToolError) as error:
                    Config.load({"QIMENG_API_BASE": "http://127.0.0.1:18481", "QIMENG_API_TOKEN": "synthetic-only",
                                 "QIMENG_AI_STATE_DIR": str(private), "QIMENG_AI_STORAGES": json.dumps({"input": str(source)})})
                self.assertEqual(error.exception.code, "CONFIG_INVALID")
