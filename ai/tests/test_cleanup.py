from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from qimeng_ai.cleanup import recycle
from qimeng_ai.config import Config
from qimeng_ai.errors import ToolError
from qimeng_ai.files import search
from qimeng_ai.intake import original_input_id
from qimeng_ai.state import State


class PermitServer:
    def __init__(self):
        self.entry = {}
        self.permit = None
        self.fail_receipt = False
        self.issues = 0

    def call(self, op, args):
        if op == "getBatch":
            return {"batch": {"manifestState": "sealed"}, "inputs": [self.entry]}
        if op == "getCurrentSession":
            return {"id": "credential", "batchId": "batch", "generation": 1, "scopes": ["read", "source.cleanup"]}
        if op == "issueSourceCleanupPermit":
            self.issues += 1
            assert args["body"]["expectedHash"] == self.entry["hash"]
            self.permit = {"id": "a"*32, "instanceId": "instance", "credentialId": "credential", "generation": 1,
                           "batchId": "batch", "inputId": self.entry["id"], "protectionId": "protection",
                           "size": self.entry["size"], "hash": self.entry["hash"], "proofHash": "b"*64,
                           "state": "issued", "expiresAt": (datetime.now(timezone.utc)+timedelta(minutes=10)).isoformat()}
            return self.permit
        if op == "getSourceCleanupPermit":
            return self.permit
        if op == "recordSourceCleanupReceipt":
            if self.fail_receipt:
                self.fail_receipt = False
                raise ToolError("NETWORK_ERROR", "synthetic interrupted acknowledgement", uncertain=True)
            self.permit.update(state="recorded", receipt=args["body"])
            return self.permit
        raise AssertionError(op)


class CleanupBoundary(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        self.trash = root/"xdg"/"Trash"
        context = patch.dict(os.environ, {"XDG_DATA_HOME": str(self.trash.parent)})
        context.start()
        self.addCleanup(context.stop)
        source = root/"inputs"
        source.mkdir()
        self.file = source/"chosen sample.png"
        self.file.write_bytes(b"synthetic chosen original")
        config = Config.load({"QIMENG_API_BASE": "http://127.0.0.1:18481", "QIMENG_API_TOKEN": "synthetic",
                              "QIMENG_AI_STATE_DIR": str(root/"private"), "QIMENG_AI_STORAGES": json.dumps({"input": str(source)}),
                              "QIMENG_AI_PERMISSIONS": "read,local.read,source.cleanup,local.recycle", "QIMENG_AI_RECYCLE_STORAGES": '["input"]'})
        self.server = PermitServer()
        self.runtime = SimpleNamespace(config=config, state=State(config, "instance", "contract"),
                                       transport=self.server, instance_id="instance")
        item = search(self.runtime, {"storage":"input"})["items"][0]
        record = self.runtime.state.read("r_"+item["fileRef"][2:], kind="file")
        input_id = original_input_id("batch", 0, record)
        self.server.entry = {"id":input_id, "ordinal":0, "state":"ingested", "sourceRelativePath":self.file.name,
                             "size":self.file.stat().st_size, "hash":hashlib.sha256(self.file.read_bytes()).hexdigest()}
        self.args = {"batchId":"batch", "protectionId":"protection", "inputId":input_id,
                     "fileRef":item["fileRef"], "operationId":"source-cleanup-original"}

    def test_same_bytes_at_another_path_do_not_authorize_recycling(self):
        other = self.file.with_name("other.png")
        other.write_bytes(self.file.read_bytes())
        item = search(self.runtime, {"storage":"input","query":"other.png"})["items"][0]
        with self.assertRaises(ToolError) as raised:
            recycle(self.runtime, {**self.args, "fileRef":item["fileRef"]})
        self.assertEqual(raised.exception.code, "ORIGINAL_SELECTION_MISMATCH")
        self.assertTrue(other.exists())
        self.assertEqual(self.server.issues, 0)

    @unittest.skipUnless(sys.platform=="linux", "freedesktop native provider")
    def test_lost_receipt_does_not_move_replacement_at_original_path(self):
        original = self.file.read_bytes()
        self.server.fail_receipt = True
        with self.assertRaises(ToolError) as interrupted:
            recycle(self.runtime, self.args)
        self.assertTrue(interrupted.exception.uncertain)
        self.assertFalse(self.file.exists())
        native_object, = (self.trash/"files").iterdir()
        self.assertEqual(native_object.read_bytes(), original)
        info, = (self.trash/"info").iterdir()
        self.assertIn("chosen%20sample.png", info.read_text())
        self.file.write_bytes(b"new file at old location")
        self.assertEqual(recycle(self.runtime, self.args)["outcome"], "recycled")
        self.assertEqual(self.file.read_bytes(), b"new file at old location")
        self.assertEqual(self.server.issues, 1)
        self.assertNotIn(str(self.file), json.dumps(self.server.permit["receipt"]))

    def test_missing_provider_preserves_original_and_records_reason(self):
        with patch("qimeng_ai.cleanup._provider", return_value=None):
            result = recycle(self.runtime, self.args)
        self.assertEqual(result["outcome"], "retained")
        self.assertEqual(result["code"], "provider_unavailable")
        self.assertTrue(self.file.exists())

    @unittest.skipUnless(sys.platform=="win32", "Windows Shell native provider")
    def test_windows_recycle_returns_a_native_item_or_preserves_the_original(self):
        import win32file
        self.assertEqual(win32file.GetDriveType(self.file.anchor), 3, "Windows fixture requires a local fixed volume")
        original = self.file.read_bytes()
        result = recycle(self.runtime, self.args)
        if result["outcome"] == "retained":
            self.assertEqual(self.file.read_bytes(), original)
            print("Windows runner retained the fixture:", result["code"])
            return
        self.assertEqual(result["outcome"], "recycled")
        self.assertFalse(self.file.exists())
        self.assertIn("providerReceipt", self.server.permit["receipt"])
        self.file.write_bytes(b"fresh synthetic original")
        self.assertEqual(recycle(self.runtime, self.args)["outcome"], "recycled")
        self.assertEqual(self.file.read_bytes(), b"fresh synthetic original")
        print("Windows Shell provided the verified native Recycle Bin item")
