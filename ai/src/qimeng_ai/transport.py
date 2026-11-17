"""通用 transport 只消费生成的操作契约；禁止模型提供任意 URL 或 HTTP 方法。"""
from __future__ import annotations

import importlib.resources
import json
import re
from pathlib import Path
from urllib.parse import quote

import httpx

from .config import Config
from .errors import ToolError

MAX_RESPONSE_BYTES = 32 * 1024 * 1024


def load_contract() -> dict:
    packaged = importlib.resources.files("qimeng_ai").joinpath("operations.json")
    if packaged.is_file():
        return json.loads(packaged.read_text(encoding="utf-8"))
    repository = Path(__file__).resolve().parents[3]
    path = repository / "api/generated/python/operations.json"
    if not path.is_file():
        raise ToolError("CONTRACT_MISSING", "缺少云端生成契约，请使用包含 SDK 的已验证工具包")
    return json.loads(path.read_text(encoding="utf-8"))


class Transport:
    def __init__(self, config: Config, contract: dict, *, client: httpx.Client | None = None):
        self.config, self.contract = config, contract
        self.active_token = None
        self.client = client or httpx.Client(base_url=config.origin, timeout=config.timeout,
                                            follow_redirects=False, trust_env=False,
                                            headers={"Authorization": f"Bearer {config.token}"})

    def close(self) -> None:
        self.client.close()

    def call(self, operation_id: str, arguments: dict, *, content=None):
        operation = self.contract["operations"][operation_id]
        path = operation["path"]
        params, headers = {}, {}
        if self.active_token is not None:
            headers["Authorization"] = "Bearer " + self.active_token
        for parameter in operation["parameters"]:
            name = parameter["name"]
            if name not in arguments:
                continue
            value = arguments[name]
            location = parameter["in"]
            if location == "path":
                path = path.replace("{" + name + "}", quote(str(value), safe=""))
            elif location == "query":
                params[name] = str(value).lower() if isinstance(value, bool) else value
            elif location == "header":
                headers[name] = str(value)
        if re.search(r"\{[^}]+\}", path):
            raise ToolError("INVALID_INPUT", "缺少操作路径参数")
        url = self.contract["basePath"].rstrip("/") + path
        mutation = operation["method"] not in {"GET", "HEAD"}
        kwargs = {"params": params, "headers": headers}
        if operation["body"] == "application/json":
            kwargs["json"] = arguments.get("body")
        elif content is not None:
            kwargs["content"] = content
            headers["Content-Type"] = operation["body"] or "application/octet-stream"
        try:
            with self.client.stream(operation["method"], url, **kwargs) as response:
                data = bytearray()
                for chunk in response.iter_bytes():
                    if len(data) + len(chunk) > MAX_RESPONSE_BYTES:
                        raise ToolError("RESPONSE_LIMIT", "服务响应超过执行器读取上限", uncertain=mutation)
                    data.extend(chunk)
                try:
                    result = json.loads(data) if data else None
                except ValueError:
                    raise ToolError("RESPONSE_INVALID", "服务未返回约定 JSON", status=response.status_code,
                                    uncertain=mutation) from None
                if response.is_redirect:
                    raise ToolError("REDIRECT_REJECTED", "拒绝服务端重定向", status=response.status_code)
                if response.is_error:
                    code = result.get("code", "HTTP_ERROR") if isinstance(result, dict) else "HTTP_ERROR"
                    # 服务端错误正文可能包含用户内容；这里只传稳定错误码和修订。
                    if not re.fullmatch(r"[A-Za-z0-9_.-]{1,80}", str(code)):
                        code = "HTTP_ERROR"
                    details = {}
                    if isinstance(result, dict) and isinstance(result.get("currentRevision"), int):
                        details["currentRevision"] = result["currentRevision"]
                    raise ToolError(code, "服务拒绝或未完成请求；按错误码处理并回读原操作",
                                    status=response.status_code, uncertain=mutation and response.status_code >= 500,
                                    details=details)
                return result
        except httpx.HTTPError:
            raise ToolError("NETWORK_ERROR", "网络中断或超时；保留原操作身份后回读",
                            uncertain=mutation) from None
