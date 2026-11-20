"""在云端验证权限、分页引用、持久预算与错误语义，全部使用临时合成数据。"""
import os
from pathlib import Path
import tempfile
import unittest

import httpx

from qimeng_ai.config import Config
from qimeng_ai.errors import ToolError
from qimeng_ai.output import page, present
from qimeng_ai.runtime import Runtime
from qimeng_ai.state import State
from qimeng_ai.transport import Transport, load_contract


def config(directory, **extra):
    return Config.load({"QIMENG_API_BASE": "http://127.0.0.1:18481", "QIMENG_API_TOKEN": "synthetic-only",
                        "QIMENG_AI_STATE_DIR": directory, **extra})


class BudgetAndReferences(unittest.TestCase):
    def test_lost_frame_lookup_needs_the_image_operation_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            cfg = config(directory, QIMENG_AI_TASK_ID="task-for-frame")
            state = State(cfg, "instance-a", "contract-a")
            key = "lost-frame-response"
            request = state.reserve("research.image.search", {"Idempotency-Key": key}, key)
            state.finish(request, {"code": "NETWORK_ERROR"}, key, uncertain=True)
            self.assertEqual(state.receipt_target(key), (key, "lookupResearchImage"))
            receipt = {"state": "committed", "idempotencyKey": key, "operation": "lookupResearchImage",
                       "response": {"id": "frame-query", "taskId": cfg.task_id, "state": "interrupted"}}
            with self.assertRaises(ToolError):
                state.reconcile(key, {**receipt, "operation": "queryResearchProvider"})
            self.assertEqual([row["key"] for row in state.status()["pending"]], [key])
            state.reconcile(key, receipt)
            self.assertEqual(state.status()["pending"], [])

    def test_disabled_lookup_leaves_local_writes_available_but_timeout_needs_its_own_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            cfg = config(directory, QIMENG_AI_TASK_ID="task-for-research")
            state = State(cfg, "instance-a", "contract-a")
            contract = load_contract()
            arguments = {"provider": "fxtwitter", "Idempotency-Key": "original-query",
                         "body": {"operation": "author", "target": "fixture"}}
            tool = "research.provider.query"

            def provider(request):
                if request.headers["Idempotency-Key"] == "original-query":
                    return httpx.Response(403, json={"code": "research_provider_disabled"})
                raise httpx.ReadTimeout("fixture connection interrupted", request=request)

            with httpx.Client(base_url=cfg.origin, transport=httpx.MockTransport(provider)) as client:
                transport = Transport(cfg, contract, client=client)
                for key, uncertain in (("original-query", False), ("lost-response", True)):
                    arguments["Idempotency-Key"] = key
                    request = state.reserve(tool, arguments, key)
                    with self.assertRaises(ToolError) as refused:
                        transport.call("queryResearchProvider", arguments)
                    self.assertEqual(refused.exception.uncertain, uncertain)
                    state.finish(request, refused.exception.as_dict(), key, uncertain=uncertain, completed=False)
                    if not uncertain:
                        local = state.reserve("research.submit", {"outcome": "unknown"}, "local-evidence")
                        state.finish(local, {"status": "saved"}, "local-evidence")

            self.assertEqual(state.receipt_target("lost-response"), ("lost-response", "queryResearchProvider"))
            with self.assertRaises(ToolError):
                state.reserve("research.submit", {}, "next-evidence")
            terminal = {"state": "committed", "operation": "queryResearchProvider", "idempotencyKey": "lost-response",
                        "response": {"id": "query-original", "taskId": cfg.task_id, "state": "interrupted"}}
            for wrong in ({"operation": "different-operation"}, {"idempotencyKey": "another-key"},
                          {"response": {**terminal["response"], "taskId": "another-task"}},
                          {"response": {**terminal["response"], "state": "pending"}}):
                with self.assertRaises(ToolError):
                    state.reconcile("lost-response", {**terminal, **wrong})
            state.reconcile("lost-response", terminal)
            self.assertFalse(state.status()["pending"])

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

    def test_failed_resume_does_not_clear_an_earlier_uncertain_write(self):
        with tempfile.TemporaryDirectory() as directory:
            state = State(config(directory), "instance-a", "contract-a")
            request = state.reserve("upload.file", {"operationId": "original"}, "original")
            state.finish(request, {"code": "NETWORK_ERROR"}, "original", uncertain=True, completed=False)
            state.reserve("upload.file", {"operationId": "original"}, "original")
            state.finish(request, {"code": "task_budget_exhausted"}, "original", completed=False)
            self.assertEqual(len(state.status()["pending"]), 1)
            state.finish(request, {"state": "ingested"}, "original")
            self.assertEqual(state.status()["pending"], [])

    def test_executor_secret_is_not_a_result_reference_and_old_lease_cannot_replace_new(self):
        with tempfile.TemporaryDirectory() as directory:
            state = State(config(directory), "instance-a", "contract-a")
            secret = state.executor_secret("task.start:stable-id")
            self.assertEqual(secret, state.executor_secret("task.start:stable-id"))
            with self.assertRaises(ToolError):
                state.read(secret)
            state.attach_task_credential({"taskId": "task-a", "credential": {"batchId": "task-a", "generation": 2}}, secret)
            with self.assertRaises(ToolError):
                state.attach_task_credential({"taskId": "task-a", "credential": {"batchId": "task-a", "generation": 1}}, "old")
            self.assertEqual(state.task_credential("task-a"), secret)

    def test_result_ref_cannot_cross_instance_or_credential(self):
        with tempfile.TemporaryDirectory() as directory:
            first = State(config(directory), "instance-a", "contract-a")
            reference = first.save({"items": ["a", "b", "c"]})
            for other in (State(config(directory), "instance-b", "contract-a"),
                          State(config(directory, QIMENG_API_TOKEN="different-synthetic"), "instance-a", "contract-a")):
                with self.assertRaises(ToolError):
                    other.read(reference)
            self.assertEqual(page(first, reference, "/items", 1, 1, max_bytes=2048)["items"], ["b"])

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

    def test_large_call_budget_has_no_artificial_ceiling_and_rejects_invalid_values(self):
        with tempfile.TemporaryDirectory() as directory:
            state = State(config(directory, QIMENG_AI_TASK_ID="task-large-budget",
                                 QIMENG_AI_MAX_CALLS="1000000000"), "instance-a", "contract-a")
            self.assertEqual(state.status()["maxCalls"], 1_000_000_000)
            state.renew(10_000_000_000, 1800)
            self.assertEqual(state.status()["maxCalls"], 10_000_000_000)
            for calls in (9_999_999_999, 20_000.5, "20000", 2**63):
                with self.assertRaises(ToolError) as raised:
                    state.renew(calls, 1800)
                self.assertEqual(raised.exception.code, "INVALID_BUDGET")
            for value in ("0", "-1", "1.5", "many", str(2**63), 10_000.5):
                with self.assertRaises(ToolError) as raised:
                    config(directory, QIMENG_AI_MAX_CALLS=value)
                self.assertEqual(raised.exception.code, "CONFIG_INVALID")

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
