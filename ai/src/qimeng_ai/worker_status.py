"""Relay an observed worker transition to the bound Go task without heartbeats."""
import hashlib
import json

from .errors import ToolError
from .state import encode


def pause_reason(outcome):
    if outcome == "WORKER_HOST_UNAVAILABLE":
        return "host_unavailable"
    if outcome in {"PERMISSION_DENIED", "MEDIA_TRANSFER_NOT_AUTHORIZED", "WORKER_HOST_AUTH"}:
        return "permission_required"
    if outcome in {"TASK_BUDGET_EXHAUSTED", "research_budget_exhausted"}:
        return "budget_exhausted"
    if outcome in {"waiting_receipt", "WORKER_QUERY_PENDING", "NETWORK_ERROR", "WRITE_UNRESOLVED",
                   "STILL_UNCERTAIN", "RESULT_PRESENTATION_FAILED", "EXECUTOR_ERROR"}:
        return "operation_uncertain"
    return "needs_attention"


class WorkerStatus:
    def __init__(self, runtime, state):
        self.runtime, self.state = runtime, state
        self.observed = {}
        self.blocked = set()
        with state.transaction() as db:
            db.execute("""CREATE TABLE IF NOT EXISTS worker_status_outbox (
                batch TEXT PRIMARY KEY, request_key TEXT NOT NULL, body TEXT NOT NULL)""")

    def observe(self, report):
        batch = report["batch"]["id"]
        self.observed[batch] = report.get("executorStatus")

    def flush(self):
        with self.state.transaction() as db:
            rows = db.execute("SELECT * FROM worker_status_outbox ORDER BY rowid LIMIT 32").fetchall()
        for row in rows:
            self.send(row["batch"], row["request_key"], json.loads(row["body"]))

    def send(self, batch, key, body):
        try:
            self.runtime.task_state(batch)
            response = self.runtime.transport.call("reportIntakeExecutorStatus",
                                                   {"id": batch, "Idempotency-Key": key, "body": body})
        except ToolError as exc:
            if exc.uncertain or exc.code == "NETWORK_ERROR" or exc.status == 429:
                self.blocked.add(batch)
                return
            if exc.status not in {401, 403, 404, 409} and exc.code != "TASK_NOT_BOUND":
                raise
            # The observation belonged to an older lease or snapshot. Do not relabel the new one.
            self.observed.pop(batch, None)
        else:
            self.observed[batch] = response
        with self.state.transaction() as db:
            db.execute("DELETE FROM worker_status_outbox WHERE batch=? AND request_key=?", (batch, key))

    def transition(self, batch, state, reason=None):
        current = self.observed.get(batch)
        if not current or not current.get("generation") or batch in self.blocked:
            return
        if current["state"] == state and current.get("reason") == reason:
            return
        if state == "claimed" and current["state"] != "paused":
            return
        body = {"state": state, "generation": current["generation"], "batchRevision": current["batchRevision"],
                "expectedStatusRevision": current["revision"]}
        if reason is not None:
            body["reason"] = reason
        key = "observation-" + hashlib.sha256(encode([batch, body]).encode()).hexdigest()
        with self.state.transaction() as db:
            # A lost acknowledgement keeps its original payload until Go returns the receipt.
            db.execute("INSERT OR IGNORE INTO worker_status_outbox(batch,request_key,body) VALUES(?,?,?)",
                       (batch, key, encode(body)))
            row = db.execute("SELECT request_key,body FROM worker_status_outbox WHERE batch=?", (batch,)).fetchone()
        self.send(batch, row["request_key"], json.loads(row["body"]))
