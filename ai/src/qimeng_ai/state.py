"""执行端预算及可丢弃结果缓存；业务计划和完成事实始终回读 Go。"""
from __future__ import annotations

from contextlib import contextmanager
import hashlib
import json
import secrets
import sqlite3
import time

from .config import Config
from .errors import ToolError

MAX_CACHE_BYTES = 256 * 1024 * 1024
MAX_RESULT_BYTES = 32 * 1024 * 1024
RESULT_TTL = 24 * 3600


def encode(value) -> str:
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"), allow_nan=False)


def observation(value):
    """临时引用的新随机ID不是任务进展；业务对象ID/修订与数据内容仍参与比较。"""
    if isinstance(value, dict):
        return {key: observation(item) for key, item in value.items()
                if key not in {"fileRef", "resultRef", "artifactRef"}}
    if isinstance(value, list):
        return [observation(item) for item in value]
    return value


class State:
    def __init__(self, config: Config, instance_id: str, contract_sha: str):
        self.config = config
        config.state_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
        if config.state_dir.is_symlink():
            raise ToolError("STATE_INVALID", "执行状态目录不能是符号链接")
        self.namespace = hashlib.sha256(encode([config.origin, instance_id,
            hashlib.sha256(config.token.encode()).hexdigest()]).encode()).hexdigest()
        self.contract_sha = contract_sha
        self.path = config.state_dir / (self.namespace + ".sqlite3")
        if self.path.is_symlink():
            raise ToolError("STATE_INVALID", "执行状态文件不能是符号链接")
        with self.transaction() as db:
            db.executescript("""
              CREATE TABLE IF NOT EXISTS tasks (id TEXT PRIMARY KEY, started REAL NOT NULL,
                calls INTEGER NOT NULL, max_calls INTEGER NOT NULL, max_seconds INTEGER NOT NULL);
              CREATE TABLE IF NOT EXISTS requests (task TEXT NOT NULL, request TEXT NOT NULL,
                digest TEXT NOT NULL, repeats INTEGER NOT NULL, PRIMARY KEY(task,request));
              CREATE TABLE IF NOT EXISTS pending (task TEXT NOT NULL, key TEXT NOT NULL,
                tool TEXT NOT NULL, created REAL NOT NULL, request TEXT NOT NULL, unresolved INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(task,key));
              CREATE TABLE IF NOT EXISTS results (id TEXT PRIMARY KEY, created REAL NOT NULL,
                value TEXT NOT NULL, bytes INTEGER NOT NULL, digest TEXT NOT NULL, kind TEXT NOT NULL);
              CREATE TABLE IF NOT EXISTS executor_secrets (operation TEXT PRIMARY KEY, secret TEXT NOT NULL);
              CREATE TABLE IF NOT EXISTS task_credentials (task TEXT PRIMARY KEY, secret TEXT NOT NULL,
                generation INTEGER NOT NULL, metadata TEXT NOT NULL);
              CREATE TABLE IF NOT EXISTS cleanup_journal (
                permit TEXT PRIMARY KEY, operation TEXT UNIQUE NOT NULL, binding TEXT NOT NULL,
                stage TEXT NOT NULL, outcome TEXT, created REAL NOT NULL);
            """)
            # Local execution metadata is versioned independently of generated API schemas.
            # An SDK update must not grant a fresh task budget or erase unresolved writes.
            pending_columns = {row[1] for row in db.execute("PRAGMA table_info(pending)")}
            if "request" not in pending_columns:
                db.execute("ALTER TABLE pending ADD COLUMN request TEXT NOT NULL DEFAULT ''")
            if "unresolved" not in pending_columns:
                db.execute("ALTER TABLE pending ADD COLUMN unresolved INTEGER NOT NULL DEFAULT 1")
            result_columns = {row[1] for row in db.execute("PRAGMA table_info(results)")}
            if "kind" not in result_columns:
                db.execute("ALTER TABLE results ADD COLUMN kind TEXT NOT NULL DEFAULT 'result'")
            db.execute("INSERT OR IGNORE INTO tasks VALUES(?,?,0,?,?)",
                       (config.task_id, time.time(), config.max_calls, config.max_seconds))

    @contextmanager
    def transaction(self):
        db = sqlite3.connect(self.path, timeout=10, isolation_level=None)
        db.row_factory = sqlite3.Row
        try:
            db.execute("BEGIN IMMEDIATE")
            yield db
            if db.in_transaction:
                db.commit()
        except BaseException:
            if db.in_transaction:
                db.rollback()
            raise
        finally:
            db.close()

    def reserve(self, tool: str, arguments: dict, key: str | None) -> str:
        request = hashlib.sha256(encode([tool, arguments]).encode()).hexdigest()
        task = self.config.task_id
        with self.transaction() as db:
            state = db.execute("SELECT * FROM tasks WHERE id=?", (task,)).fetchone()
            elapsed = time.time() - state["started"]
            if state["calls"] >= state["max_calls"] or elapsed >= state["max_seconds"]:
                raise ToolError("TASK_BUDGET_EXHAUSTED", "任务预算已耗尽，由可信端保留进度并决定续期",
                                details={"calls": state["calls"], "seconds": round(elapsed),
                                         "maxCalls": state["max_calls"], "maxSeconds": state["max_seconds"]})
            previous = db.execute("SELECT repeats FROM requests WHERE task=? AND request=?", (task, request)).fetchone()
            if previous and previous[0] >= self.config.max_unchanged:
                raise ToolError("NO_PROGRESS", "相同请求反复没有新结果，停止重复调用")
            pending = db.execute("SELECT key,tool,request FROM pending WHERE task=? LIMIT 1", (task,)).fetchone() if key else None
            if pending and (pending["key"] != key or pending["tool"] != tool or pending["request"] != request):
                raise ToolError("WRITE_UNRESOLVED", "存在未决写入；只允许以完全相同参数和操作键恢复，或由可信端核销")
            db.execute("UPDATE tasks SET calls=calls+1 WHERE id=?", (task,))
            if key and pending is None:
                db.execute("INSERT INTO pending(task,key,tool,created,request,unresolved) VALUES(?,?,?,?,?,0)", (task, key, tool, time.time(), request))
        return request

    def finish(self, request: str, value, key: str | None, *, uncertain: bool = False, completed: bool = True) -> None:
        digest = hashlib.sha256(encode(observation(value)).encode()).hexdigest()
        task = self.config.task_id
        with self.transaction() as db:
            previous = db.execute("SELECT digest,repeats FROM requests WHERE task=? AND request=?", (task, request)).fetchone()
            repeats = previous[1] + 1 if previous and previous[0] == digest else 1
            db.execute("INSERT OR REPLACE INTO requests VALUES(?,?,?,?)", (task, request, digest, repeats))
            if key:
                if uncertain:
                    db.execute("UPDATE pending SET unresolved=1 WHERE task=? AND key=?", (task, key))
                elif completed:
                    db.execute("DELETE FROM pending WHERE task=? AND key=?", (task, key))
                else:
                    db.execute("DELETE FROM pending WHERE task=? AND key=? AND unresolved=0", (task, key))

    def status(self) -> dict:
        with self.transaction() as db:
            row = db.execute("SELECT * FROM tasks WHERE id=?", (self.config.task_id,)).fetchone()
            pending = db.execute("SELECT key,tool,created FROM pending WHERE task=? ORDER BY created LIMIT 100",
                                 (self.config.task_id,)).fetchall()
            return {"taskId": row["id"], "calls": row["calls"], "maxCalls": row["max_calls"],
                    "seconds": round(time.time() - row["started"]), "maxSeconds": row["max_seconds"],
                    "pending": [dict(item) for item in pending]}

    def executor_secret(self, operation: str) -> str:
        # Private material has no resultRef and is never exposed through a model tool.
        with self.transaction() as db:
            row = db.execute("SELECT secret FROM executor_secrets WHERE operation=?", (operation,)).fetchone()
            if row is not None:
                return row[0]
            count = db.execute("SELECT count(*) FROM executor_secrets").fetchone()[0]
            if count >= 10000:
                raise ToolError("EXECUTOR_STATE_LIMIT", "执行器凭据记录达到上限，请在可信端归档已结束任务")
            secret = secrets.token_urlsafe(32)
            db.execute("INSERT INTO executor_secrets VALUES(?,?)", (operation, secret))
            return secret

    def attach_task_credential(self, task: dict, secret: str) -> None:
        identifier, credential = task.get("taskId"), task.get("credential")
        if not isinstance(identifier, str) or not isinstance(credential, dict):
            raise ToolError("TASK_RESPONSE_INVALID", "服务未返回可绑定的任务身份", uncertain=True)
        generation = credential.get("generation")
        if (not isinstance(generation, int) or generation < 1 or
                identifier not in (credential.get("batchId"), credential.get("authorTaskId"), credential.get("assetTaskId")) or "token" in credential):
            raise ToolError("TASK_RESPONSE_INVALID", "派生凭据未绑定正确任务", uncertain=True)
        with self.transaction() as db:
            previous = db.execute("SELECT generation FROM task_credentials WHERE task=?", (identifier,)).fetchone()
            if previous is not None and previous[0] > generation:
                raise ToolError("EXECUTOR_LEASE_STALE", "旧响应不能覆盖较新的任务执行器凭据")
            db.execute("INSERT OR REPLACE INTO task_credentials VALUES(?,?,?,?)",
                       (identifier, secret, generation, encode(credential)))

    def task_credential(self, task: str) -> str:
        with self.transaction() as db:
            row = db.execute("SELECT secret FROM task_credentials WHERE task=?", (task,)).fetchone()
        if row is None:
            raise ToolError("TASK_NOT_BOUND", "本执行器未持有该任务凭据，请使用task.resume接续原任务")
        return row[0]

    def receipt_target(self, key: str) -> tuple[str, str | None]:
        with self.transaction() as db:
            row = db.execute("SELECT tool FROM pending WHERE task=? AND key=?", (self.config.task_id, key)).fetchone()
        if row is None:
            raise ToolError("PENDING_NOT_FOUND", "没有该任务的未决操作")
        suffix = {"upload.file": ":complete", "source.recycle": ":receipt"}.get(row["tool"], "")
        operation = "queryResearchProvider" if row["tool"] == "research.provider.query" else None
        return key + suffix, operation

    def receipt_key(self, key: str) -> str:
        return self.receipt_target(key)[0]

    def reconcile(self, key: str, authoritative: dict) -> dict:
        expected_key, operation = self.receipt_target(key)
        if authoritative.get("state") != "committed" or authoritative.get("idempotencyKey") != expected_key:
            raise ToolError("STILL_UNCERTAIN", "服务没有可核验终态，保留未决写入")
        if operation:
            evidence = authoritative.get("response")
            if (authoritative.get("operation") != operation or not isinstance(evidence, dict)
                    or evidence.get("taskId") != self.config.task_id or not evidence.get("id")
                    or evidence.get("state") not in {"ok", "not_found", "blocked", "rate_limited", "unavailable", "interrupted"}):
                raise ToolError("STILL_UNCERTAIN", "原查询尚无本任务可核对的终态回执")
        with self.transaction() as db:
            db.execute("DELETE FROM pending WHERE task=? AND key=?", (self.config.task_id, key))
        return {"status": "reconciled", "key": key, "state": authoritative["state"]}

    def renew(self, calls: int, seconds: int) -> None:
        with self.transaction() as db:
            row = db.execute("SELECT max_calls,max_seconds FROM tasks WHERE id=?", (self.config.task_id,)).fetchone()
            if not row[0] <= calls <= 10000 or not row[1] <= seconds <= 86400:
                raise ToolError("INVALID_BUDGET", "续期只允许提高既有上限，不能清零调用和开始时间")
            db.execute("UPDATE tasks SET max_calls=?,max_seconds=? WHERE id=?", (calls, seconds, self.config.task_id))

    def save(self, value, *, kind: str = "result") -> str:
        data = encode(value)
        size = len(data.encode())
        if size > MAX_RESULT_BYTES:
            raise ToolError("RESULT_TOO_LARGE", "结果超出完整缓存上限，请在服务端使用分页查询")
        with self.transaction() as db:
            db.execute("DELETE FROM results WHERE created<?", (time.time() - RESULT_TTL,))
            digest = hashlib.sha256(data.encode()).hexdigest()
            # Presentation results use a contract-specific kind; local immutable file/image
            # references can survive an SDK refresh, while old API result shapes cannot.
            stored_kind = kind + ":" + self.contract_sha if kind == "result" else kind
            existing = db.execute("SELECT id FROM results WHERE kind=? AND digest=?", (stored_kind, digest)).fetchone()
            if existing is not None:
                return existing[0]
            used = db.execute("SELECT coalesce(sum(bytes),0) FROM results").fetchone()[0]
            count = db.execute("SELECT count(*) FROM results WHERE kind=?", (stored_kind,)).fetchone()[0]
            if used + size > MAX_CACHE_BYTES or count >= {"file": 10000, "image": 512}.get(kind, 128):
                raise ToolError("RESULT_CACHE_FULL", "结果缓存已满，保留已有引用；等待过期或由可信端清理")
            reference = "r_" + secrets.token_hex(16)
            db.execute("INSERT INTO results VALUES(?,?,?,?,?,?)", (reference, time.time(), data, size,
                       digest, stored_kind))
        return reference

    def read(self, reference: str, *, kind: str = "result"):
        stored_kind = kind + ":" + self.contract_sha if kind == "result" else kind
        with self.transaction() as db:
            row = db.execute("SELECT value,digest FROM results WHERE id=? AND created>=? AND kind=?",
                             (reference, time.time() - RESULT_TTL, stored_kind)).fetchone()
        if row is None:
            raise ToolError("RESULT_REF_INVALID", "引用不存在、已过期或不属于当前实例/凭据/契约")
        if hashlib.sha256(row[0].encode()).hexdigest() != row[1]:
            raise ToolError("RESULT_REF_CORRUPT", "缓存完整性校验失败，请重新读取服务端原结果")
        return json.loads(row[0])
