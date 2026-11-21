"""默认结果总额有界；完整业务计划保留服务端 ID，不从展示摘要执行。"""
from __future__ import annotations

from itertools import islice
import re

from .errors import ToolError
from .state import State, encode

SECRET_FIELDS = {"token", "authorization", "password", "secret", "leaseToken", "credentialSecret"}
PRIVATE_FIELDS = {"root", "absolutePath", "localPath", "dataDir", "mediaRoot"}
HIDDEN_FIELDS = SECRET_FIELDS | PRIVATE_FIELDS


def public(value, token: str):
    if isinstance(value, dict):
        return {key: public(item, token) for key, item in value.items()
                if key not in HIDDEN_FIELDS}
    if isinstance(value, list):
        return [public(item, token) for item in value]
    if isinstance(value, str):
        value = value.replace(token, "[credential]") if token else value
        return re.sub(r"(?i)Bearer\s+[^\s\"']+", "Bearer [credential]", value)
    return value


def present(value, state: State, limit: int, token: str):
    value = public(value, token)
    encoded = encode(value)
    raw = encoded.encode()
    if len(raw) <= limit:
        return value
    reference = state.save_encoded(encoded, raw, kind="result")
    summary = {"truncated": True, "resultRef": reference, "totalBytes": len(raw),
               "readWith": "platform.result.read", "omitted": "完整结果保存在受控引用；按 JSON Pointer 和 offset 续读"}
    if isinstance(value, dict):
        for key in ("id", "batchId", "state", "status", "revision", "fingerprint", "nextCursor", "operationKey"):
            if key in value and isinstance(value[key], (str, int, bool, type(None))):
                summary[key] = value[key]
        summary["fields"] = list(value)[:80]
    elif isinstance(value, list):
        summary["totalItems"] = len(value)
    if len(encode(summary).encode()) > limit:
        raise ToolError("OUTPUT_BUDGET_TOO_SMALL", "输出预算不足以承载可续读摘要")
    return summary


def page(state: State, reference: str, pointer: str = "", offset: int = 0, limit: int = 20, *, max_bytes: int) -> dict:
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
        total = len(value)
        fields = islice(value.items(), min(offset, total), min(offset + limit, total))
        items = [{"key": key, "value": item} for key, item in fields]
    elif isinstance(value, list):
        total = len(value)
        items = value[offset:offset + limit]
    elif isinstance(value, str):
        total = len(value)
        items = value[offset:offset + 1024]
    else:
        total = 1
        items = [value][offset:offset + limit]

    def response(count):
        end = offset + count
        return {"resultRef": reference, "pointer": pointer, "items": items[:count],
                "totalItems": total, "nextOffset": end if end < total else None}

    low, high = 0, len(items)
    # The last page can use a shorter null nextOffset. Check it before searching other prefixes.
    full = response(high)
    if len(encode(full).encode()) <= max_bytes:
        return full
    if len(encode(response(0)).encode()) > max_bytes:
        raise ToolError("OUTPUT_BUDGET_TOO_SMALL", "输出预算不足以承载此续读指针")
    while low + 1 < high:
        count = (low + high) // 2
        if len(encode(response(count)).encode()) <= max_bytes:
            low = count
        else:
            high = count
    if not low:
        raise ToolError("FIELD_TOO_LARGE", "单项超出当前输出预算，请使用更深的 JSON Pointer 或由可信端调整预算")
    return response(low)
