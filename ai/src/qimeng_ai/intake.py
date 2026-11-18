"""Trusted selection inventory before transfer; never infer intent from received uploads."""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path

from .errors import ToolError
from .files import guarded_path, identity, open_reference, MAX_DIRECTORY_ENTRIES


def tool_definitions() -> list[dict]:
    return [{"name": "intake.register", "scope": "ingest", "readOnly": False,
             "description": "传输前登记所选目录或完整fileRefs，最多100项。返回每项稳定inputId；有read许可时附Go认定的当前状态和原因。仅上传pending项，失败项留在原处。",
             "inputSchema": {"type": "object", "properties": {
                 "batchId": {"type": "string"},
                 "operationId": {"type": "string", "minLength": 8, "maxLength": 100},
                 "storage": {"type": "string"}, "directory": {"type": "string", "default": ""},
                 "recursive": {"type": "boolean", "default": True},
                 "fileRefs": {"type": "array", "minItems": 1, "maxItems": 100,
                              "items": {"type": "string"}}},
                 "required": ["batchId", "operationId"], "additionalProperties": False}}]


def _inventory(runtime, arguments: dict) -> list[str]:
    if "local.read" not in runtime.config.permissions:
        raise ToolError("PERMISSION_DENIED", "登记本地来源需要local.read许可")
    references = arguments.get("fileRefs")
    if references is not None:
        if any(key in arguments for key in ("storage", "directory", "recursive")):
            raise ToolError("INVALID_SELECTION", "完整文件引用与目录盘点只能选一种")
        return references
    storage = arguments.get("storage")
    if not storage:
        raise ToolError("INVALID_SELECTION", "需要明确的逻辑来源目录或完整fileRefs")
    pending = [(arguments.get("directory", ""), 0)]
    directories = []
    files = []
    visited = 0
    try:
        while pending:
            relative, depth = pending.pop()
            folder = guarded_path(runtime.config, storage, relative)
            if not folder.is_dir():
                raise ToolError("NOT_DIRECTORY", "盘点入口不是来源目录")
            if depth > 32 or len(directories) >= 256:
                raise ToolError("DIRECTORY_LIMIT", "目录层数或数量超过盘点预算；尚未登记或上传")
            directories.append((relative, identity(folder.stat())))
            with os.scandir(folder) as entries:
                for entry in entries:
                    visited += 1
                    if visited > MAX_DIRECTORY_ENTRIES:
                        raise ToolError("DIRECTORY_LIMIT", "来源目录项超过10000；尚未登记或上传")
                    child = (Path(relative) / entry.name).as_posix()
                    path = guarded_path(runtime.config, storage, child)
                    if path.is_dir():
                        if arguments.get("recursive", True):
                            pending.append((child, depth + 1))
                        continue
                    if not path.is_file():
                        raise ToolError("SOURCE_UNSUPPORTED", "来源含非普通文件；尚未登记或上传")
                    files.append((child, identity(path.stat())))
                    if len(files) > 100:
                        raise ToolError("INTAKE_LIMIT", "此选择超过单批100项；请按完整子目录或明确选择分批，不会静默截断")
        for relative, before in directories:
            if identity(guarded_path(runtime.config, storage, relative).stat()) != before:
                raise ToolError("SELECTION_CHANGED", "盘点时目录发生变化；请重新盘点，尚未登记或上传")
    except OSError:
        raise ToolError("SOURCE_UNAVAILABLE", "无法完整盘点来源，未提交不完整清单") from None
    if not files:
        raise ToolError("EMPTY_SELECTION", "所选目录没有文件")
    references = []
    for relative, before in sorted(files, key=lambda row: (row[0].casefold(), row[0])):
        ref = runtime.state.save({"kind": "file", "storage": storage, "relative": relative,
                                  "identity": before}, kind="file")
        references.append("f_" + ref[2:])
    return references


def original_input_id(batch_id: str, ordinal: int, record: dict) -> str:
    source_key = json.dumps([record["storage"], record["relative"], record["identity"]],
                            ensure_ascii=False, separators=(",", ":"))
    return hashlib.sha256((batch_id + "\0" + str(ordinal) + "\0" + source_key).encode("utf-8")).hexdigest()[:32]


def register(runtime, arguments: dict) -> dict:
    references = _inventory(runtime, arguments)
    inputs, bindings = [], []
    # Every reference is validated before the first server mutation. The actual
    # upload reopens and hashes the same captured source; no open file handles linger.
    for ordinal, reference in enumerate(references):
        with open_reference(runtime, reference) as (_, info):
            record = runtime.state.read("r_" + reference[2:], kind="file")
            # Repeated selections remain separate inputs. Go will report actual
            # duplicate content rather than silently shortening the user's list.
            identifier = original_input_id(arguments["batchId"], ordinal, record)
            item = {"id": identifier, "originalFileName": Path(record["relative"]).name,
                    "sourceRelativePath": record["relative"], "size": info.st_size}
            inputs.append(item)
            bindings.append({"inputId": identifier, "fileRef": reference,
                             "originalFileName": item["originalFileName"],
                             "sourceRelativePath": item["sourceRelativePath"], "size": info.st_size})
    # Stable expected revision belongs to the initial selection, not an evolving
    # GET response; retry of a lost seal response must replay the exact request.
    batch = runtime.transport.call("setBatchInputs", {"id": arguments["batchId"],
        "Idempotency-Key": arguments["operationId"], "body": {"expectedManifestRevision": 1, "inputs": inputs}})
    result = {"batch": batch, "inputs": bindings, "inputCount": len(inputs), "sourceRetained": True, "next": "task.context"}
    if "read" in runtime.config.permissions:
        try:
            report = runtime.transport.call("getBatch", {"id": arguments["batchId"]})
        except ToolError as exc:
            result["reportReadError"] = exc.as_dict()
            return result
        rows = report["inputs"]
        if (report["batch"]["manifestFingerprint"] != batch["manifestFingerprint"]
                or [row["id"] for row in rows] != [item["inputId"] for item in bindings]):
            result["reportReadError"] = ToolError("MANIFEST_MISMATCH", "回读的输入与封存回执不一致，请沿原任务核对").as_dict()
            return result
        for item, row in zip(bindings, rows):
            item.update({field: row[field] for field in ("state", "errorCode", "uploadId", "assetId") if field in row})
    return result
