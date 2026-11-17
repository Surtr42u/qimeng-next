"""默认结果总额有界；完整业务计划保留服务端 ID，不从展示摘要执行。"""
from __future__ import annotations

import re

from .errors import ToolError
from .state import State, encode

SECRET_FIELDS = {"token", "authorization", "password", "secret", "leaseToken"}
PRIVATE_FIELDS = {"root", "absolutePath", "localPath", "dataDir", "mediaRoot"}


def public(value, token: str):
    if isinstance(value, dict):
        return {key: public(item, token) for key, item in value.items()
                if key not in SECRET_FIELDS | PRIVATE_FIELDS}
    if isinstance(value, list):
        return [public(item, token) for item in value]
    if isinstance(value, str):
        value = value.replace(token, "[credential]") if token else value
        return re.sub(r"(?i)Bearer\s+[^\s\"']+", "Bearer [credential]", value)
    return value


def present(value, state: State, limit: int, token: str):
    value = public(value, token)
    encoded = encode(value)
    if len(encoded.encode()) <= limit:
        return value
    reference = state.save(value)
    summary = {"truncated": True, "resultRef": reference, "totalBytes": len(encoded.encode()),
               "readWith": "platform.result.read", "omitted": "完整结果保存在受控引用；按 JSON Pointer 和 offset 续读"}
    if isinstance(value, dict):
        for key in ("id", "batchId", "state", "status", "revision", "fingerprint", "nextCursor"):
            if key in value and isinstance(value[key], (str, int, bool, type(None))):
                summary[key] = value[key]
        summary["fields"] = list(value)[:80]
    elif isinstance(value, list):
        summary["totalItems"] = len(value)
    if len(encode(summary).encode()) > limit:
        raise ToolError("OUTPUT_BUDGET_TOO_SMALL", "输出预算不足以承载可续读摘要")
    return summary


def page(state: State, reference: str, pointer: str = "", offset: int = 0, limit: int = 20) -> dict:
    if offset < 0 or not 1 <= limit <= 100 or (pointer and not pointer.startswith("/")):
        raise ToolError("INVALID_INPUT", "续读指针/偏移/条数无效")
    value = state.read(reference)
    try:
        for part in pointer.split("/")[1:] if pointer else []:
            key = part.replace("~1", "/").replace("~0", "~")
            value = value[int(key)] if isinstance(value, list) else value[key]
    except (KeyError, IndexError, TypeError, ValueError):
        raise ToolError("POINTER_INVALID", "JSON Pointer 未命中结果字段") from None
    if isinstance(value, dict):
        values = [{"key": key, "value": item} for key, item in value.items()]
    elif isinstance(value, list):
        values = value
    elif isinstance(value, str):
        # 字符串按字符续读，仍以UTF-8字节限制每页。
        values = list(value)
        limit = min(4096, max(limit, 1024))
    else:
        values = [value]
    output = []
    for item in values[offset:offset + limit]:
        candidate = output + [item]
        if len(encode(candidate).encode()) > 11000:
            if not output:
                raise ToolError("FIELD_TOO_LARGE", "单项过大，请使用更深的 JSON Pointer 读取子字段")
            break
        output = candidate
    next_offset = offset + len(output)
    return {"resultRef": reference, "pointer": pointer,
            "items": "".join(output) if isinstance(value, str) else output,
            "totalItems": len(values), "nextOffset": next_offset if next_offset < len(values) else None}
