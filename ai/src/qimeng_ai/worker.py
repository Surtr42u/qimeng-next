"""Claim App-submitted intakes and drive the existing scoped executor."""
from __future__ import annotations

from contextlib import contextmanager
import json
import os
import secrets
import signal
import threading
import time

from . import output
from .config import positive
from .errors import ToolError
from .state import encode
from .worker_host import ModelUsage, WorkerHost, work_with_host
from .worker_status import WorkerStatus, pause_reason


class ClaimJournal:
    # Only transport intents survive here. Task status, plans and receipts are read from Go.
    def __init__(self, state):
        self.state = state
        with state.transaction() as db:
            db.execute("""CREATE TABLE IF NOT EXISTS worker_claims (
                batch TEXT PRIMARY KEY, claim_key TEXT NOT NULL, body TEXT NOT NULL,
                action TEXT, args TEXT, next_attempt REAL NOT NULL DEFAULT 0,
                paused TEXT, revision INTEGER)""")

    def remember(self, candidate, allow_image_lookup):
        body = {"kind": "intake", "mode": "intelligent", "label": "App 智能整理",
                "batchId": candidate["batchId"], "libraryId": candidate["libraryId"],
                "readyManifestFingerprint": candidate["manifestFingerprint"]}
        if allow_image_lookup and candidate["maxResearchCalls"] > 0:
            body["allowImageLookup"] = True
        with self.state.transaction() as db:
            db.execute("INSERT OR IGNORE INTO worker_claims(batch,claim_key,body) VALUES(?,?,?)",
                       (body["batchId"], "claim-" + secrets.token_urlsafe(24), encode(body)))

    def due(self):
        with self.state.transaction() as db:
            now = time.time()
            active = db.execute("SELECT * FROM worker_claims WHERE paused IS NULL AND next_attempt<=? "
                                "ORDER BY next_attempt,rowid LIMIT 16", (now,)).fetchall()
            parked = db.execute("SELECT * FROM worker_claims WHERE paused IS NOT NULL AND revision IS NOT NULL "
                                "AND next_attempt<=? ORDER BY next_attempt,rowid LIMIT 8", (now,)).fetchall()
            return active + parked

    def before_call(self, batch_id, name, args):
        with self.state.transaction() as db:
            db.execute("UPDATE worker_claims SET action=?,args=? WHERE batch=?", (name, encode(args), batch_id))

    def after_call(self, batch_id):
        with self.state.transaction() as db:
            db.execute("UPDATE worker_claims SET action=NULL,args=NULL WHERE batch=?", (batch_id,))

    def defer(self, batch_id, seconds):
        with self.state.transaction() as db:
            db.execute("UPDATE worker_claims SET next_attempt=? WHERE batch=?", (time.time() + seconds, batch_id))

    def park(self, batch_id, reason, revision, seconds):
        with self.state.transaction() as db:
            db.execute("UPDATE worker_claims SET paused=?,revision=?,next_attempt=? WHERE batch=?",
                       (reason, revision, time.time() + seconds, batch_id))

    def resume(self, batch_id):
        with self.state.transaction() as db:
            change = db.execute("UPDATE worker_claims SET paused=NULL,revision=NULL,next_attempt=0 WHERE batch=?",
                                (batch_id,))
            if not change.rowcount:
                raise ToolError("WORKER_CLAIM_NOT_FOUND", "此执行器没有该批的领取记录")

    def forget(self, batch_id):
        with self.state.transaction() as db:
            db.execute("DELETE FROM worker_claims WHERE batch=?", (batch_id,))


@contextmanager
def exclusive_worker(state):
    # Separate processes using the same private credentials cannot interleave a host turn.
    location = state.path.with_suffix(".worker-lock")
    if location.is_symlink():
        raise ToolError("STATE_INVALID", "执行器锁路径不能是符号链接")
    with location.open("a+b") as handle:
        try:
            if os.name == "nt":
                import msvcrt
                if handle.seek(0, os.SEEK_END) == 0:
                    handle.write(b"\0")
                    handle.flush()
                handle.seek(0)
                msvcrt.locking(handle.fileno(), msvcrt.LK_NBLCK, 1)
            else:
                import fcntl
                fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError:
            raise ToolError("WORKER_ALREADY_RUNNING", "这个私有执行器已有进程在处理队列") from None
        yield


def current_report(runtime, batch):
    state = runtime.task_state(batch)
    report = runtime.transport.call("getBatch", {"id": batch})
    if report.get("batch", {}).get("id") != batch:
        raise ToolError("TASK_RESPONSE_INVALID", "不能确认接收批次身份")
    visible = output.present(report, state, runtime.config.output_bytes, runtime.config.token)
    return report, visible


def intake_state(report):
    state = report["batch"]["state"]
    if state in {"completed", "cancelled"}:
        return state
    if any(item["state"] == "staged" for item in report["inputs"]):
        return "staged"
    if state == "partial":
        return "partial"
    return "waiting"


def drive_claim(runtime, host, journal, row, stop, usage, status):
    batch = row["batch"]
    body = json.loads(row["body"])
    try:
        journal.state.task_credential(batch)
    except ToolError as exc:
        if exc.code != "TASK_NOT_BOUND":
            raise
        started = runtime.call("task.start", {"operationId": row["claim_key"], "body": body})
        if started.get("taskId") != batch:
            raise ToolError("TASK_RESPONSE_INVALID", "签发响应与原应用批次不符", uncertain=True)
    report, visible = current_report(runtime, batch)
    status.observe(report)
    if row["paused"]:
        if report["batch"]["revision"] == row["revision"]:
            return "unchanged", row["revision"]
        journal.resume(batch)
    if row["action"]:
        args = json.loads(row["args"])
        value = runtime.call(row["action"], args)
        if isinstance(value, dict) and value.get("state") == "pending":
            return "waiting_receipt", None
        journal.after_call(batch)
        report, visible = current_report(runtime, batch)
        status.observe(report)
    before = intake_state(report)
    if before != "staged" or stop.is_set():
        return ("stopped" if stop.is_set() else before), report["batch"]["revision"]
    outcome = work_with_host(runtime, host, journal, batch, visible, stop, body.get("allowImageLookup", False),
                             usage, lambda: status.transition(batch, "claimed"))
    report, _ = current_report(runtime, batch)
    status.observe(report)
    after = intake_state(report)
    if after == "staged":
        return ("stopped" if outcome == "stopped" else "needs_attention"), report["batch"]["revision"]
    return after, report["batch"]["revision"]


def worker_pass(runtime, host, stop, allow_image_lookup, poll_seconds, retry_batch=None):
    runtime.transport.active_token = None
    state = runtime.task_state(None)
    with exclusive_worker(state):
        journal = ClaimJournal(state)
        status = WorkerStatus(runtime, state)
        status.flush()
        runtime.transport.active_token = None
        if retry_batch:
            journal.resume(retry_batch)
        admission = None
        try:
            ready = runtime.transport.call("listReadyIntakeBatches", {})["items"]
        except ToolError as exc:
            if exc.code != "delegation_daily_limit":
                raise
            # The daily quota admits new tasks. Already claimed batches keep their lease and budget.
            ready, admission = [], exc.code
        if ready and allow_image_lookup:
            policy = runtime.transport.call("getCurrentAIDelegation", {})
            allow_image_lookup = policy.get("canLookupImages") is True
        for candidate in ready:
            journal.remember(candidate, allow_image_lookup)
        outcomes = []
        for row in journal.due():
            if stop.is_set():
                break
            batch = row["batch"]
            usage = ModelUsage()
            revision = None
            try:
                outcome, revision = drive_claim(runtime, host, journal, row, stop, usage, status)
            except ToolError as exc:
                outcome = exc.code
            if outcome == "unchanged":
                journal.defer(batch, poll_seconds)
                continue
            if outcome in {"completed", "partial", "cancelled", "batch_not_claimable", "task_already_claimed"}:
                journal.forget(batch)
            elif outcome in {"waiting_receipt", "WORKER_QUERY_PENDING", "NETWORK_ERROR", "WORKER_HOST_UNAVAILABLE", "stopped"}:
                journal.defer(batch, poll_seconds)
            else:
                journal.park(batch, outcome, revision, poll_seconds)
            if outcome not in {"completed", "partial", "cancelled", "stopped", "batch_not_claimable", "task_already_claimed"}:
                status.transition(batch, "paused", pause_reason(outcome))
            outcomes.append({"batchId": batch, "state": outcome, "modelUsage": usage.summary()})
            if outcome == "WORKER_HOST_UNAVAILABLE":
                break
        report = {"state": "processed" if outcomes else "idle", "batches": outcomes}
        if admission:
            report["admission"] = admission
        return report


def run_worker(runtime, *, once=False, retry_batch=None, env=None):
    env = os.environ if env is None else env
    if not {"read", "delegate", "organize"} <= runtime.config.permissions:
        raise ToolError("PERMISSION_DENIED", "自动领取需要可信端明确授予 read、delegate 和 organize")
    timeout = positive(env, "QIMENG_AI_WORKER_POLL_SECONDS", 30, 3600)
    setting = env.get("QIMENG_AI_WORKER_IMAGE_LOOKUP", "false").lower()
    if setting not in {"true", "false"} or setting == "true" and "research" not in runtime.config.permissions:
        raise ToolError("WORKER_HOST_CONFIG", "单帧查证须明确配置布尔值并有 research 权限")
    host = WorkerHost(env, runtime.config.timeout)
    stop = threading.Event()
    previous = {}
    def interrupt(_signal, _frame):
        stop.set()
        raise KeyboardInterrupt

    try:
        for sig in (signal.SIGINT, signal.SIGTERM):
            previous[sig] = signal.signal(sig, interrupt)
        while not stop.is_set():
            try:
                report = worker_pass(runtime, host, stop, setting == "true", timeout, retry_batch)
                retry_batch = None
            except ToolError as exc:
                if once or exc.code not in {"NETWORK_ERROR", "WORKER_ALREADY_RUNNING"} and exc.status not in {502, 503, 504}:
                    raise
                report = {"state": exc.code, "batches": []}
            if once:
                return report
            if report["state"] != "idle":
                print(encode(report), flush=True)
            stop.wait(timeout)
        return {"state": "stopped", "batches": []}
    except KeyboardInterrupt:
        return {"state": "stopped", "batches": []}
    finally:
        for sig, original in previous.items():
            signal.signal(sig, original)
        host.close()
