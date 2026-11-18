"""A source disposition is a local side effect, authorized separately by Go.

The model can name only a sealed input and an immutable source reference. The
permit, native provider and durable outcome are private executor details.
"""
from __future__ import annotations

from datetime import datetime, timezone
import hashlib
import json
import os
import re
import sys
import time

from .errors import ToolError
from .files import identity, open_reference
from .intake import original_input_id
from .state import encode


def tool_definitions():
    return [{
        "name": "source.recycle", "scope": "source.cleanup", "readOnly": False,
        "description": "把本批已独立备份的原始输入移入受信来源端回收站。校验原选择身份、版本、摘要、租约及本地许可，结果可沿原操作键回读。",
        "inputSchema": {"type": "object", "properties": {
            "batchId": {"type": "string"}, "inputId": {"type": "string"},
            "protectionId": {"type": "string"}, "fileRef": {"type": "string", "pattern": "^f_"},
            "operationId": {"type": "string", "minLength": 8, "maxLength": 100}},
            "required": ["batchId","inputId","protectionId","fileRef","operationId"], "additionalProperties": False},
    }]


def _journal(state, operation):
    with state.transaction() as db:
        row = db.execute("SELECT * FROM cleanup_journal WHERE operation=?", (operation,)).fetchone()
        return dict(row) if row else None


def _prepare(state, operation, permit, binding):
    with state.transaction() as db:
        existing = db.execute("SELECT operation,binding FROM cleanup_journal WHERE permit=?", (permit,)).fetchone()
        if existing:
            if existing["operation"] != operation or existing["binding"] != encode(binding):
                raise ToolError("CLEANUP_BINDING_CONFLICT", "原回收许可已绑定其他操作，请沿原操作回读")
            return
        count = db.execute("SELECT count(*) FROM cleanup_journal").fetchone()[0]
        if count >= 100_000:
            raise ToolError("CLEANUP_JOURNAL_FULL", "回收日志已达容量上限，请由本人归档已确认的记录")
        db.execute("INSERT INTO cleanup_journal VALUES(?,?,?,'prepared',NULL,?)",
                   (permit, operation, encode(binding), time.time()))


def _observe(state, permit, outcome, *, allow_armed):
    with state.transaction() as db:
        row = db.execute("SELECT stage,outcome FROM cleanup_journal WHERE permit=?", (permit,)).fetchone()
        if row["stage"] == "armed" and not allow_armed:
            raise ToolError("CLEANUP_OUTCOME_PENDING", "另一执行器已开始处理同一许可，请回读", uncertain=True)
        if row["outcome"] is not None and row["outcome"] != encode(outcome):
            raise ToolError("CLEANUP_OUTCOME_CONFLICT", "回收日志已有不同事实，请核对回收站", uncertain=True)
        db.execute("UPDATE cleanup_journal SET stage='observed',outcome=? WHERE permit=?", (encode(outcome), permit))


def _authorized_permit(runtime, permit, binding, expected_id):
    session = runtime.transport.call("getCurrentSession", {})
    if (permit.get("id") != expected_id or not re.fullmatch("[a-f0-9]{32}", expected_id)
            or permit.get("instanceId") != runtime.instance_id
            or permit.get("credentialId") != session.get("id")
            or session.get("batchId") != binding["batchId"]
            or "source.cleanup" not in session.get("scopes", [])
            or any(permit.get(k) != binding[k] for k in ("batchId", "inputId", "protectionId", "size", "hash"))
            or not re.fullmatch("[a-f0-9]{64}", permit.get("proofHash", ""))):
        raise ToolError("CLEANUP_PERMIT_MISMATCH", "服务许可、当前凭据与原始输入不一致")
    expiry = datetime.fromisoformat(permit["expiresAt"].replace("Z", "+00:00"))
    return (permit.get("state") == "issued" and permit["generation"] == session.get("generation", 0)
            and expiry > datetime.now(timezone.utc))


def _verified_source(runtime, args):
    batch_id, input_id = args["batchId"], args["inputId"]
    detail = runtime.transport.call("getBatch", {"id": batch_id})
    entry = next((item for item in detail["inputs"] if item["id"] == input_id), None)
    if detail["batch"].get("manifestState") != "sealed" or not entry or entry["state"] != "ingested":
        raise ToolError("INPUT_NOT_INGESTED", "只有本批原始清单中已入库的输入可进入回收核验")
    record = runtime.state.read("r_" + args["fileRef"][2:], kind="file")
    if record.get("storage") not in runtime.config.recycle_storages:
        raise ToolError("LOCAL_RECYCLE_DENIED", "来源存储没有独立回收授权")
    if original_input_id(batch_id, entry["ordinal"], record) != input_id or entry.get("sourceRelativePath") != record["relative"]:
        raise ToolError("ORIGINAL_SELECTION_MISMATCH", "文件引用不是登记这项输入的原文件对象")
    with open_reference(runtime, args["fileRef"]) as (source, before):
        digest = hashlib.file_digest(source, "sha256").hexdigest()
        if identity(before) != identity(os.fstat(source.fileno())):
            raise ToolError("SOURCE_CHANGED", "原文件在回收核验时发生变化")
    return {"batchId": batch_id, "inputId": input_id, "protectionId": args["protectionId"],
            "fileRef": args["fileRef"], "record": record, "size": before.st_size, "hash": digest}


def _provider():
    if sys.platform == "linux":
        from . import trash_linux
        return trash_linux
    if sys.platform == "win32":
        from . import trash_windows
        return trash_windows
    return None


def recycle(runtime, args):
    if not {"source.cleanup", "local.read", "local.recycle"} <= runtime.config.permissions:
        raise ToolError("LOCAL_RECYCLE_DENIED", "来源回收需要来源读取、本地回收和服务来源回收三项许可")
    operation = args["operationId"]
    state = runtime.state
    prior = _journal(state, operation)
    if prior:
        binding = json.loads(prior["binding"])
        if any(binding[key] != args[key] for key in ("batchId", "inputId", "protectionId", "fileRef")):
            raise ToolError("OPERATION_CONFLICT", "同一回收操作不能更换原始输入或来源")
        permit_id = prior["permit"]
    else:
        binding = _verified_source(runtime, args)
        permit = runtime.transport.call("issueSourceCleanupPermit", {
            "id": args["batchId"], "protectionId": args["protectionId"], "Idempotency-Key": operation + ":permit",
            "body": {"inputId": args["inputId"], "expectedHash": binding["hash"], "sourceSize": binding["size"]}})
        permit_id = permit["id"]
        _prepare(state, operation, permit_id, binding)
        prior = _journal(state, operation)
    permit = runtime.transport.call("getSourceCleanupPermit", {"id": permit_id})
    current = _authorized_permit(runtime, permit, binding, permit_id)
    if permit.get("state") == "recorded":
        return {"inputId": args["inputId"], "outcome": permit["receipt"]["outcome"], "recorded": True}
    provider = _provider()
    owns_native_action = False
    if prior["outcome"]:
        outcome = json.loads(prior["outcome"])
    elif prior["stage"] == "armed":
        outcome = provider.recover(runtime.config, binding, permit_id) if provider else None
        if outcome is None:
            raise ToolError("CLEANUP_OUTCOME_PENDING", "来源操作已经启动，请按原键回读或由本人核对回收站", uncertain=True)
        owns_native_action = True  # independently recovered native evidence
    elif not current:
        outcome = {"outcome": "retained", "code": "permit_no_longer_current"}
    elif binding["record"]["storage"] not in runtime.config.recycle_storages or provider is None:
        outcome = {"outcome": "retained", "code": "provider_unavailable"}
    else:
        # The only transition authorizing entry to the native provider is a
        # single conditional write, including when two MCP hosts share state.
        def arm():
            nonlocal owns_native_action
            live = runtime.transport.call("getSourceCleanupPermit", {"id": permit_id})
            if not _authorized_permit(runtime, live, binding, permit_id):
                raise ToolError("CLEANUP_PERMIT_EXPIRED", "操作前许可或执行租约已变化")
            with state.transaction() as db:
                affected = db.execute("UPDATE cleanup_journal SET stage='armed' WHERE permit=? AND stage='prepared'", (permit_id,)).rowcount
                if affected != 1:
                    raise ToolError("CLEANUP_OUTCOME_PENDING", "已有执行器开始回收，请回读实际去向", uncertain=True)
            owns_native_action = True
        # Each provider calls observe immediately after native proof and before
        # returning. A network error recording the receipt never repeats the move.
        outcome = provider.move(runtime.config, binding, permit_id, arm,
                                lambda value: _observe(state, permit_id, value, allow_armed=owns_native_action))
    _observe(state, permit_id, outcome, allow_armed=owns_native_action)
    saved = runtime.transport.call("recordSourceCleanupReceipt", {"id": permit_id, "Idempotency-Key": operation + ":receipt", "body": outcome})
    if saved.get("state") != "recorded" or saved.get("receipt", {}).get("outcome") != outcome["outcome"]:
        raise ToolError("CLEANUP_RECEIPT_UNCONFIRMED", "本地去向已保留，服务回执尚未确认", uncertain=True)
    return {"inputId": args["inputId"], "outcome": outcome["outcome"], "code": outcome.get("code"), "recorded": True}
