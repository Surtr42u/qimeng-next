from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest

from qimeng_ai.config import Config
from qimeng_ai.runtime import Runtime
from qimeng_ai.task_actions import present_batch_actions
from qimeng_ai.transport import load_contract


class TaskActions(unittest.TestCase):
    def test_human_duplicate_decision_and_local_denial_leave_independent_authorized_work(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Config.load({"QIMENG_API_BASE": "http://127.0.0.1:18481", "QIMENG_API_TOKEN": "synthetic-fixture",
                                  "QIMENG_AI_STATE_DIR": str(Path(directory)), "QIMENG_AI_PERMISSIONS": "read,organize"})

            def session(operation, arguments):
                self.assertEqual((operation, arguments), ("getCurrentSession", {}))
                return {"batchId": "fixture-batch", "scopes": ["read", "research", "organize"]}

            executor = Runtime(config, contract=load_contract(), transport=SimpleNamespace(call=session))
            context = {"batch": {"id": "fixture-batch"}, "inputs": [{"id": "duplicate-file", "state": "duplicate"},
                       {"id": "new-file", "state": "staged"}],
                       "nextActions": ["awaitUserDuplicateDecision", "preparePlan", "submitResearch", "unknownFutureOperation"]}
            presented = present_batch_actions(executor, context, executor.tools())
            self.assertEqual(presented["nextActions"], ["plan.prepare"])
            self.assertEqual(presented["actionWaits"], [
                {"reason": "user_duplicate_decision", "inputIds": ["duplicate-file"]},
                {"reason": "permission_required", "tool": "research.submit", "missingLocalScopes": ["research"],
                 "missingServerScopes": [], "requiresBatchBinding": False},
                {"reason": "unavailable_tool", "operation": "unknownFutureOperation"},
            ])
