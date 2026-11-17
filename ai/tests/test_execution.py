"""在云端验证权限、分页引用、持久预算与错误语义，全部使用临时合成数据。"""
import os
from pathlib import Path
import tempfile
import unittest

from qimeng_ai.config import Config
from qimeng_ai.errors import ToolError
from qimeng_ai.output import page, present
from qimeng_ai.runtime import Runtime
from qimeng_ai.state import State


def config(directory, **extra):
    return Config.load({"QIMENG_API_BASE": "http://127.0.0.1:18481", "QIMENG_API_TOKEN": "synthetic-only",
                        "QIMENG_AI_STATE_DIR": directory, **extra})


class BudgetAndReferences(unittest.TestCase):
    def test_reconnect_preserves_calls_and_pending_write_blocks_new_write(self):
        with tempfile.TemporaryDirectory() as directory:
            first = State(config(directory), "instance-a", "contract-a")
            request = first.reserve("upload", {"id": "a"}, "operation-a")
            first.finish(request, {"code": "NETWORK_ERROR"}, "operation-a", uncertain=True)
            second = State(config(directory), "instance-a", "contract-a")
            self.assertEqual(second.status()["calls"], 1)
            with self.assertRaises(ToolError) as raised:
                second.reserve("upload", {"id": "b"}, "operation-b")
            self.assertEqual(raised.exception.code, "WRITE_UNRESOLVED")
            second.reserve("query", {"id": "a"}, None)
            self.assertEqual(second.status()["calls"], 2)
            second.reserve("upload", {"id": "a"}, "operation-a")
            self.assertEqual(second.status()["calls"], 3)

    def test_contract_upgrade_preserves_budget_and_requires_final_upload_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            first = State(config(directory), "instance-a", "contract-a")
            request = first.reserve("upload.file", {"operationId": "original"}, "original")
            first.finish(request, {"code": "NETWORK_ERROR"}, "original", uncertain=True)
            reference = first.save({"contractSpecific": True})
            second = State(config(directory), "instance-a", "contract-b")
            self.assertEqual(second.status()["calls"], 1)
            self.assertEqual(second.receipt_key("original"), "original:complete")
            with self.assertRaises(ToolError):
                second.read(reference)
            with self.assertRaises(ToolError):
                second.reconcile("original", {"state": "committed", "idempotencyKey": "original:prepare"})
            self.assertEqual(len(second.status()["pending"]), 1)
            second.reconcile("original", {"state": "committed", "idempotencyKey": "original:complete"})
            self.assertEqual(second.status()["pending"], [])

    def test_result_ref_cannot_cross_instance_or_credential(self):
        with tempfile.TemporaryDirectory() as directory:
            first = State(config(directory), "instance-a", "contract-a")
            reference = first.save({"items": ["a", "b", "c"]})
            for other in (State(config(directory), "instance-b", "contract-a"),
                          State(config(directory, QIMENG_API_TOKEN="different-synthetic"), "instance-a", "contract-a")):
                with self.assertRaises(ToolError):
                    other.read(reference)
            self.assertEqual(page(first, reference, "/items", 1, 1)["items"], ["b"])

    def test_long_result_is_recoverable_and_token_is_removed(self):
        with tempfile.TemporaryDirectory() as directory:
            state = State(config(directory), "instance-a", "contract-a")
            result = present({"id": "plan-a", "fingerprint": "f", "token": "synthetic-only",
                              "items": [{"name": str(i), "value": "内容" * 100} for i in range(100)]},
                             state, 2048, "synthetic-only")
            self.assertTrue(result["truncated"])
            saved = state.read(result["resultRef"])
            self.assertNotIn("token", saved)
            self.assertEqual(len(saved["items"]), 100)
            self.assertEqual(saved["fingerprint"], "f")

    def test_renew_does_not_clear_consumption_and_no_progress_stops(self):
        with tempfile.TemporaryDirectory() as directory:
            state = State(config(directory), "instance-a", "contract-a")
            for _ in range(3):
                request = state.reserve("query", {}, None)
                state.finish(request, {"value": "unchanged"}, None)
            state.renew(200, 1800)
            self.assertEqual(state.status()["calls"], 3)
            with self.assertRaises(ToolError) as raised:
                state.reserve("query", {}, None)
            self.assertEqual(raised.exception.code, "NO_PROGRESS")

    def test_default_permission_is_read_only(self):
        with tempfile.TemporaryDirectory() as directory:
            self.assertEqual(config(directory).permissions, {"read"})
            for origin in ("http://example.org", "https://user:secret@example.org", "https://example.org/api"):
                with self.assertRaises(ToolError):
                    config(directory, QIMENG_API_BASE=origin)


if __name__ == "__main__":
    if os.environ.get("CI") != "true":
        raise SystemExit("All tests run in cloud CI only.")
    unittest.main()
