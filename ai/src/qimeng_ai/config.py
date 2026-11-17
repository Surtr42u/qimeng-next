"""可信执行配置来自环境；模型参数不能修改权限、目标或资源预算。"""
from __future__ import annotations

from dataclasses import dataclass
import ipaddress
import json
import os
from pathlib import Path
from urllib.parse import urlsplit

from .errors import ToolError


def positive(env: dict, name: str, default: int, maximum: int) -> int:
    try:
        value = int(env.get(name, default))
    except (ValueError, TypeError):
        raise ToolError("CONFIG_INVALID", f"{name} 必须是整数") from None
    if not 1 <= value <= maximum:
        raise ToolError("CONFIG_INVALID", f"{name} 超出允许范围")
    return value


@dataclass(frozen=True)
class Config:
    origin: str
    token: str
    state_dir: Path
    task_id: str
    permissions: frozenset[str]
    max_calls: int
    max_seconds: int
    max_unchanged: int
    output_bytes: int
    timeout: int
    storages: dict[str, Path]
    allow_media_to_model: bool

    @classmethod
    def load(cls, env=None) -> "Config":
        env = os.environ if env is None else env
        origin = env.get("QIMENG_API_BASE", "").rstrip("/")
        parsed = urlsplit(origin)
        if (parsed.scheme not in {"http", "https"} or not parsed.hostname or parsed.username or
                parsed.password or parsed.path or parsed.query or parsed.fragment):
            raise ToolError("CONFIG_INVALID", "QIMENG_API_BASE 必须是无路径、无凭据的服务 origin")
        if parsed.scheme == "http":
            try:
                address = ipaddress.ip_address(parsed.hostname)
                local = address.is_loopback or address.is_private
            except ValueError:
                local = parsed.hostname == "localhost"
            if not local:
                raise ToolError("CONFIG_INVALID", "HTTP 仅允许回环或私有 IP；其他地址使用 HTTPS")
        token = env.get("QIMENG_API_TOKEN", "")
        if not token:
            raise ToolError("CONFIG_MISSING", "缺少 QIMENG_API_TOKEN")
        directory = Path(env.get("QIMENG_AI_STATE_DIR", ""))
        if not directory.is_absolute():
            raise ToolError("CONFIG_INVALID", "QIMENG_AI_STATE_DIR 必须是可信私有绝对目录")
        permissions = frozenset(filter(None, env.get("QIMENG_AI_PERMISSIONS", "read").split(",")))
        if not permissions <= {"read", "delegate", "ingest", "research", "organize", "author.profile.write", "local.read", "local.research"}:
            raise ToolError("CONFIG_INVALID", "QIMENG_AI_PERMISSIONS 含未知权限")
        try:
            raw_storages = json.loads(env.get("QIMENG_AI_STORAGES", "{}"))
        except ValueError:
            raise ToolError("CONFIG_INVALID", "QIMENG_AI_STORAGES 必须是逻辑名到目录的 JSON 对象") from None
        if not isinstance(raw_storages, dict):
            raise ToolError("CONFIG_INVALID", "存储配置必须是对象")
        storages = {}
        for name, value in raw_storages.items():
            if not isinstance(value, str) or not name.isidentifier() or not Path(value).is_absolute():
                raise ToolError("CONFIG_INVALID", "存储必须使用逻辑名和绝对路径")
            try:
                source_root = Path(value).resolve()
                private_root = directory.resolve()
            except OSError:
                raise ToolError("CONFIG_INVALID", "无法确认来源和私有状态目录边界") from None
            if source_root.is_relative_to(private_root) or private_root.is_relative_to(source_root):
                raise ToolError("CONFIG_INVALID", "来源存储与执行器私有状态目录必须完全分离")
            storages[name] = Path(value)
        if positive(env, "QIMENG_AI_OUTPUT_BYTES", 12288, 1048576) < 2048:
            raise ToolError("CONFIG_INVALID", "输出预算至少2048字节，以容纳执行结果和恢复引用")
        return cls(origin, token, directory, env.get("QIMENG_AI_TASK_ID", "interactive"), permissions,
                   positive(env, "QIMENG_AI_MAX_CALLS", 100, 10000),
                   positive(env, "QIMENG_AI_MAX_SECONDS", 900, 86400),
                   positive(env, "QIMENG_AI_MAX_UNCHANGED", 3, 100),
                   positive(env, "QIMENG_AI_OUTPUT_BYTES", 12288, 1048576),
                   positive(env, "QIMENG_AI_TIMEOUT", 120, 1800), storages,
                   env.get("QIMENG_AI_ALLOW_MEDIA_TO_MODEL", "false").lower() in {"1", "true"})
