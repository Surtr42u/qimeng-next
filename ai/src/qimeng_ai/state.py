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


class State:
    def __init__(self, config: Config, instance_id: str, contract_sha: str):
        self.config = config
        config.state_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
        if config.state_dir.is_symlink():
            raise ToolError("STATE_INVALID", "执行状态目录不能是符号链接")
        self.namespace = hashlib.sha256(encode([config.origin, instance_id,
            hashlib.sha256(config.token.encode()).hexdigest(), contract_sha]).encode()).hexdigest()
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
                tool TEXT NOT NULL, created REAL NOT NULL, PRIMARY KEY(task,key));
              CREATE TABLE IF NOT EXISTS results (id TEXT PRIMARY KEY, created REAL NOT NULL,
                value TEXT NOT NULL, bytes INTEGER NOT NULL, digest TEXT NOT NULL);
            """)
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
            if key and db.execute("SELECT 1 FROM pending WHERE task=? LIMIT 1", (task,)).fetchone():
                raise ToolError("WRITE_UNRESOLVED", "存在未决写入，先按原操作键回读并由可信端核销")
            db.execute("UPDATE tasks SET calls=calls+1 WHERE id=?", (task,))
            if key:
                db.execute("INSERT INTO pending VALUES(?,?,?,?)", (task, key, tool, time.time()))
        return request

    def finish(self, request: str, value, key: str | None, *, uncertain: bool = False) -> None:
        digest = hashlib.sha256(encode(value).encode()).hexdigest()
        task = self.config.task_id
        with self.transaction() as db:
            previous = db.execute("SELECT digest,repeats FROM requests WHERE task=? AND request=?", (task, request)).fetchone()
            repeats = previous[1] + 1 if previous and previous[0] == digest else 1
            db.execute("INSERT OR REPLACE INTO requests VALUES(?,?,?,?)", (task, request, digest, repeats))
            if key and not uncertain:
                db.execute("DELETE FROM pending WHERE task=? AND key=?", (task, key))

    def status(self) -> dict:
        with self.transaction() as db:
            row = db.execute("SELECT * FROM tasks WHERE id=?", (self.config.task_id,)).fetchone()
            pending = db.execute("SELECT key,tool,created FROM pending WHERE task=? ORDER BY created LIMIT 100",
                                 (self.config.task_id,)).fetchall()
            return {"taskId": row["id"], "calls": row["calls"], "maxCalls": row["max_calls"],
                    "seconds": round(time.time() - row["started"]), "maxSeconds": row["max_seconds"],
                    "pending": [dict(item) for item in pending]}

    def reconcile(self, key: str, authoritative: dict) -> dict:
        if authoritative.get("state") not in {"completed", "not_applied", "failed"}:
            raise ToolError("STILL_UNCERTAIN", "服务没有可核验终态，保留未决写入")
        with self.transaction() as db:
            db.execute("DELETE FROM pending WHERE task=? AND key=?", (self.config.task_id, key))
        return {"status": "reconciled", "key": key, "state": authoritative["state"]}

    def renew(self, calls: int, seconds: int) -> None:
        with self.transaction() as db:
            row = db.execute("SELECT max_calls,max_seconds FROM tasks WHERE id=?", (self.config.task_id,)).fetchone()
            if not row[0] <= calls <= 10000 or not row[1] <= seconds <= 86400:
                raise ToolError("INVALID_BUDGET", "续期只允许提高既有上限，不能清零调用和开始时间")
            db.execute("UPDATE tasks SET max_calls=?,max_seconds=? WHERE id=?", (calls, seconds, self.config.task_id))

    def save(self, value) -> str:
        data = encode(value)
        size = len(data.encode())
        if size > MAX_RESULT_BYTES:
            raise ToolError("RESULT_TOO_LARGE", "结果超出完整缓存上限，请在服务端使用分页查询")
        with self.transaction() as db:
            db.execute("DELETE FROM results WHERE created<?", (time.time() - RESULT_TTL,))
            used, count = db.execute("SELECT coalesce(sum(bytes),0),count(*) FROM results").fetchone()
            if used + size > MAX_CACHE_BYTES or count >= 128:
                raise ToolError("RESULT_CACHE_FULL", "结果缓存已满，保留已有引用；等待过期或由可信端清理")
            reference = "r_" + secrets.token_hex(16)
            db.execute("INSERT INTO results VALUES(?,?,?,?,?)", (reference, time.time(), data, size,
                       hashlib.sha256(data.encode()).hexdigest()))
        return reference

    def read(self, reference: str):
        with self.transaction() as db:
            row = db.execute("SELECT value,digest FROM results WHERE id=? AND created>=?",
                             (reference, time.time() - RESULT_TTL)).fetchone()
        if row is None:
            raise ToolError("RESULT_REF_INVALID", "引用不存在、已过期或不属于当前实例/凭据/契约")
        if hashlib.sha256(row[0].encode()).hexdigest() != row[1]:
            raise ToolError("RESULT_REF_CORRUPT", "缓存完整性校验失败，请重新读取服务端原结果")
        return json.loads(row[0])
