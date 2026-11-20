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

# Stable business codes mapped to one-line, actionable hints. Hints never echo
# server messages, field values, paths or identifiers supplied by the user.
BUSINESS_HINTS = {
    "asset_field_occupied": "该字段已有内容；本任务只补全空字段。需要整体替换时，请本人在 AI 授权中另开「替换已有作品标签」，并在任务中选择标签字段。",
    "asset_scope_denied": "此作品任务超出首次授权的字段或文件范围；先读 platform.permissions，再按 assetFields 或 manageFiles 请求。",
    "asset_task_scope_denied": "当前凭据未绑定这个作品任务；用 platform.session 核对 taskId。",
    "asset_unavailable": "库已停用或作品已在回收站；先回读库与作品状态，不要换个任务ID重试。",
    "asset_write_denied": "此凭据未获作品资料写入权限；由本人签发含 assetFields 的作品任务。",
    "author_task_stale": "作者任务已完成、取消或到期；按当前作者修订建立新任务。",
    "update_budget_denied": "本次请求超出任务的更新次数或授权上限；不要换键重试，先核对平台预算。",
    "asset_tags_required": "替换标签须至少保留一个标签；清空标签请由本人在影库中操作。",
    "human_field_protected": "该字段有本人修改或清空记录，工具不能覆盖；只有首次授权开启替换且任务声明该字段后才可改写。",
    "asset_evidence_required": "每个要改的字段都要附自己的出处与观察；补齐 evidence 后沿原操作键重试。",
    "asset_updates_exhausted": "此任务的修改次数已用完；由本人核对后建立新任务，不要换键重试。",
    "asset_task_expired": "作品任务已到期；按当前修订与内容版本重新建立任务。",
    "asset_task_revoked": "此作品任务的授权已撤销；历史证据仍可读，需本人重新授权。",
    "asset_version_changed": "作品内容或元数据已变化；重新读取作品，再按新修订建立任务。",
    "asset_files_denied": "此任务未获文件管理许可；由本人开启「回收站与移动」并在任务中申请文件管理。",
    "asset_protection_pending": "该作品的独立备份核验尚未完成；先读取保护记录，完成后再处理回收站。",
    "asset_not_trashed": "作品不在回收站，无需恢复；先读取作品当前状态。",
    "library_scan_denied": "首次授权未包含整理所选媒体库；由本人调整授权后再触发扫描。",
    "library_scope_denied": "目标不在首次授权的范围内；不要改用其他库或作者重试。",
    "scan_already_queued": "该库已有扫描在执行；读取扫描状态，等完成或取消后再触发。",
    "author_merge_denied": "合并作者需要首次配置单独授权，且只能由长期凭据发起。",
    "author_scope_denied": "源或目标作者不在授权范围内；不要改用其他作者重试。",
    "same_author": "源与目标必须是不同作者；重新选择后再提交。",
    "merge_regular_only": "只有明确的常规作者身份可以合并；占位或 COS 身份保留原样。",
    "revision_conflict": "对象已被修改；重新读取当前修订后再提交，不要复用旧修订。",
    "delegation_inactive": "长期授权已撤销或到期；请本人在 AI 接入中重新授权。",
    "idempotency_conflict": "同一操作键已用于不同输入；沿用原参数回读，或换新键重新开始。",
    "naming_namespace_changed": "目录命名已变化；重新读取 Go 命名预览后再提交。",
    "asset_rename_scope": "改名只能作用于当前作品与目录；跨目录请使用移动工具。",
    "asset_rename_intent": "现有单项改名请选择规范续号或精确名称。",
}


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
                                            verify=config.tls_ca or (False if config.tls_insecure_loopback else True),
                                            headers={"Authorization": f"Bearer {config.token}"})

    def close(self) -> None:
        self.client.close()

    def _request(self, operation_id: str, arguments: dict, content=None):
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
        mutation = not operation["readOnly"]
        kwargs = {"params": params, "headers": headers}
        if operation["body"] == "application/json":
            kwargs["json"] = arguments.get("body")
        elif content is not None:
            kwargs["content"] = content
            headers["Content-Type"] = operation["body"] or "application/octet-stream"
        return operation, url, kwargs, mutation

    def call(self, operation_id: str, arguments: dict, *, content=None):
        operation, url, kwargs, mutation = self._request(operation_id, arguments, content)
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
                    message = BUSINESS_HINTS.get(code, "服务拒绝或未完成请求；按错误码处理并回读原操作")
                    if operation_id == "updateAuthor" and code in {"scope_denied", "field_scope_denied", "clear_not_authorized", "author_task_stale", "revision_conflict"}:
                        details["nextTool"] = "platform.session"
                        if code == "scope_denied" and "acquisitionSources" in (arguments.get("body") or {}):
                            message = "作者维护凭据不能写 acquisitionSources。请省略该字段，用 platform.session 的 authorTaskId 读取 author.task.context，再按 allowedFields 提交。"
                        else:
                              message = "用 platform.session 核对范围。单一作者任务可直接用 author.task.context 回读获准字段及 task.expectedRevision。人工修改后须复核原任务。清空需要 allowClear。长期 delegate 凭据可用 platform.permissions 和 task.start 建立任务。"
                    raise ToolError(code, message,
                                    status=response.status_code, uncertain=mutation and response.status_code >= 500,
                                    details=details)
                return result
        except httpx.HTTPError:
            raise ToolError("NETWORK_ERROR", "网络中断或超时；保留原操作身份后回读",
                            uncertain=mutation) from None

    def binary(self, operation_id: str, arguments: dict, *, maximum: int):
        operation, url, kwargs, mutation = self._request(operation_id, arguments)
        if mutation:
            raise ToolError("BINARY_READ_ONLY", "二进制读取通道仅支持只读操作")
        try:
            with self.client.stream(operation["method"], url, **kwargs) as response:
                if response.is_redirect:
                    raise ToolError("REDIRECT_REJECTED", "研究内容拒绝重定向")
                if response.is_error:
                    raise ToolError("ARTIFACT_UNAVAILABLE", "研究内容不可读、授权失效或引用已过期", status=response.status_code)
                result = bytearray()
                for chunk in response.iter_bytes():
                    if len(result) + len(chunk) > maximum:
                        raise ToolError("ARTIFACT_LIMIT", "研究内容超过读取预算")
                    result.extend(chunk)
                return bytes(result), response.headers.get("content-type", "").split(";", 1)[0]
        except httpx.HTTPError:
            raise ToolError("NETWORK_ERROR", "研究内容读取中断，请按原引用重试") from None
