"""逻辑存储与流式传输；不直接改正式库、不向模型暴露物理根。"""
from __future__ import annotations

from contextlib import contextmanager
from datetime import datetime, timezone
import hashlib
import mimetypes
import os
from pathlib import Path
import stat

from .errors import ToolError

CHUNK_BYTES = 4 * 1024 * 1024
MAX_DIRECTORY_ENTRIES = 10000


def tool_definitions() -> list[dict]:
    return [
        {"name": "files.storages", "description": "列出可信端配置的逻辑存储名，不显示物理目录。",
         "scope": "local.read", "readOnly": True,
         "inputSchema": {"type": "object", "properties": {}, "additionalProperties": False}},
        {"name": "files.search", "description": "有界列出一个逻辑存储子目录，按相对名筛选并返回fileRef；目录项可继续进入。",
         "scope": "local.read", "readOnly": True,
         "inputSchema": {"type": "object", "properties": {
             "storage": {"type": "string"}, "directory": {"type": "string", "default": ""},
             "query": {"type": "string", "maxLength": 200, "default": ""},
             "offset": {"type": "integer", "minimum": 0, "default": 0},
             "limit": {"type": "integer", "minimum": 1, "maximum": 100, "default": 20}},
             "required": ["storage"], "additionalProperties": False}},
        {"name": "upload.file", "description": "把fileRef流式分片提交到已授权批次。operationId首次生成后保持；重试先读取服务端offset。不会删来源。",
         "scope": "ingest", "readOnly": False,
         "inputSchema": {"type": "object", "properties": {
             "fileRef": {"type": "string"}, "operationId": {"type": "string", "minLength": 8, "maxLength": 100},
             "libraryId": {"type": "string"}, "batchId": {"type": "string"},
             "fileName": {"type": "string", "minLength": 1, "maxLength": 240},
             "mode": {"type": "string", "enum": ["ordinary", "intelligent"]},
             "authorId": {"type": "string"}, "collectionId": {"type": "string"},
             "note": {"type": "string", "maxLength": 10000}},
             "required": ["fileRef", "operationId", "libraryId", "batchId", "fileName", "mode"],
             "additionalProperties": False}},
    ]


def guarded_path(config, storage: str, relative: str) -> Path:
    root = config.storages.get(storage)
    if root is None or Path(relative).is_absolute() or ".." in Path(relative).parts or "\x00" in relative:
        raise ToolError("PATH_DENIED", "存储或相对路径未获授权")
    if ":" in relative or relative.startswith(("/", "\\")):
        raise ToolError("PATH_DENIED", "拒绝绝对路径及替代数据流")
    path = root / relative
    try:
        resolved_root = root.resolve(strict=True)
        resolved = path.resolve(strict=True)
        if not resolved.is_relative_to(resolved_root):
            raise ToolError("PATH_DENIED", "路径超出授权存储")
        for ancestor in (path, *path.parents):
            info = ancestor.lstat()
            if stat.S_ISLNK(info.st_mode) or getattr(info, "st_file_attributes", 0) & getattr(stat, "FILE_ATTRIBUTE_REPARSE_POINT", 1024):
                raise ToolError("PATH_DENIED", "拒绝链接或重解析路径")
            if ancestor == root:
                break
    except OSError:
        raise ToolError("FILE_UNAVAILABLE", "来源不存在或不可读取") from None
    return path


def identity(info) -> list[int]:
    return [info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns]


def search(runtime, arguments: dict) -> dict:
    storage = arguments["storage"]
    directory = arguments.get("directory", "")
    path = guarded_path(runtime.config, storage, directory)
    if not path.is_dir():
        raise ToolError("NOT_DIRECTORY", "所选引用不是目录")
    matches = []
    query = arguments.get("query", "").casefold()
    try:
        with os.scandir(path) as entries:
            for index, item in enumerate(entries):
                if index >= MAX_DIRECTORY_ENTRIES:
                    raise ToolError("DIRECTORY_LIMIT", "单目录超过10000项，请由可信端缩小存储范围")
                if not item.is_symlink() and query in item.name.casefold():
                    matches.append(item.name)
    except OSError:
        raise ToolError("DIRECTORY_UNAVAILABLE", "目录无法枚举") from None
    matches.sort(key=lambda name: (name.casefold(), name))
    offset, limit = arguments.get("offset", 0), arguments.get("limit", 20)
    output = []
    for name in matches[offset:offset + limit]:
        relative = (Path(directory) / name).as_posix()
        item = guarded_path(runtime.config, storage, relative)
        info = item.stat()
        if item.is_dir():
            output.append({"name": name, "directory": relative, "kind": "directory"})
        elif item.is_file():
            reference = runtime.state.save({"kind": "file", "storage": storage, "relative": relative,
                                            "identity": identity(info)})
            output.append({"name": name, "relativePath": relative, "kind": "file", "size": info.st_size,
                           "fileRef": "f_" + reference[2:]})
    next_offset = offset + limit
    return {"items": output, "totalItems": len(matches),
            "nextOffset": next_offset if next_offset < len(matches) else None,
            "snapshot": "目录变化时重新枚举；fileRef执行时复核源身份"}


@contextmanager
def open_reference(runtime, reference: str):
    if "local.read" not in runtime.config.permissions or not reference.startswith("f_"):
        raise ToolError("FILE_REF_DENIED", "需要local.read权限和有效fileRef")
    record = runtime.state.read("r_" + reference[2:])
    if not isinstance(record, dict) or record.get("kind") != "file":
        raise ToolError("FILE_REF_INVALID", "引用不是源文件")
    path = guarded_path(runtime.config, record["storage"], record["relative"])
    descriptor = None
    try:
        descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_BINARY", 0))
        with os.fdopen(descriptor, "rb") as source:
            descriptor = None
            before = os.fstat(source.fileno())
            if not stat.S_ISREG(before.st_mode) or identity(before) != record["identity"]:
                raise ToolError("FILE_REF_STALE", "来源已变化，请重新发现")
            # 打开后再比对授权路径与fd，避免最后一级被替换而继续读取其他对象。
            current = guarded_path(runtime.config, record["storage"], record["relative"])
            if identity(current.stat()) != identity(before):
                raise ToolError("FILE_REF_STALE", "打开期间来源身份发生变化")
            yield source, before
    except OSError:
        raise ToolError("FILE_UNAVAILABLE", "无法安全读取来源") from None
    finally:
        if descriptor is not None:
            os.close(descriptor)


def upload(runtime, arguments: dict) -> dict:
    operation = arguments["operationId"]
    with open_reference(runtime, arguments["fileRef"]) as (source, before):
        digest = hashlib.sha256()
        for chunk in iter(lambda: source.read(CHUNK_BYTES), b""):
            digest.update(chunk)
        expected_hash = digest.hexdigest()
        if identity(os.fstat(source.fileno())) != identity(before):
            raise ToolError("FILE_REF_STALE", "哈希过程中来源变化，未上传")
        body = {key: arguments[key] for key in ("libraryId", "batchId", "fileName", "mode", "authorId", "collectionId", "note")
                if key in arguments}
        body.update(size=before.st_size, mimeType=mimetypes.guess_type(arguments["fileName"])[0] or "application/octet-stream",
                    sourceMtime=datetime.fromtimestamp(before.st_mtime, timezone.utc).isoformat(), expectedHash=expected_hash)
        created = runtime.transport.call("createUpload", {"body": body, "Idempotency-Key": operation + ":prepare"})
        upload_id = created["id"]
        current = runtime.transport.call("getUpload", {"id": upload_id})
        if current["size"] != before.st_size:
            raise ToolError("UPLOAD_CONFLICT", "原上传大小与当前来源不一致", uncertain=True)
        if current["state"] in {"cancelled", "failed", "duplicate"}:
            return {"status": current["state"], "upload": current, "sourceRetained": True}
        offset = current["offset"]
        if not 0 <= offset <= before.st_size:
            raise ToolError("UPLOAD_INVALID", "服务端offset超出来源范围", uncertain=True)
        source.seek(offset)
        while offset < before.st_size:
            chunk = source.read(min(CHUNK_BYTES, before.st_size - offset))
            if not chunk:
                raise ToolError("SOURCE_CHANGED", "来源在传输中变短", uncertain=True)
            current = runtime.transport.call("appendUpload", {"id": upload_id, "Upload-Offset": offset}, content=chunk)
            if current["offset"] != offset + len(chunk):
                raise ToolError("UPLOAD_OFFSET", "分片回读offset不符合本次传输", uncertain=True)
            offset = current["offset"]
        if identity(os.fstat(source.fileno())) != identity(before):
            raise ToolError("SOURCE_CHANGED", "来源在传输期间变化，保留会话不提交", uncertain=True)
        result = runtime.transport.call("completeUpload", {"id": upload_id, "Idempotency-Key": operation + ":complete"})
        verified = runtime.transport.call("getUpload", {"id": upload_id})
        if verified.get("hash") != expected_hash:
            raise ToolError("UPLOAD_HASH_MISMATCH", "服务端内容摘要与来源不一致，请停止批次并核对", uncertain=True)
        return {"status": verified["state"], "upload": verified, "asset": result.get("asset"),
                "sha256": expected_hash, "sourceRetained": True,
                "next": "task.context" if arguments["mode"] == "intelligent" else "asset.read"}


def dispatch(runtime, name: str, arguments: dict):
    if name == "files.storages":
        return {"items": [{"id": key} for key in sorted(runtime.config.storages)]}
    if name == "files.search":
        return search(runtime, arguments)
    if name == "upload.file":
        return upload(runtime, arguments)
    raise ToolError("TOOL_UNKNOWN", "本地工具未注册")
