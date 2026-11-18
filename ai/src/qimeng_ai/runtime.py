"""CLI/MCP 共用执行路径：契约、许可、预算、调用、结果呈现。"""
from __future__ import annotations

import threading
import copy
import secrets
from dataclasses import replace
from datetime import date
from jsonschema import Draft202012Validator, FormatChecker

from . import __version__, output
from .bound_task import BoundTask
from .config import Config
from .content import ImageResult
from .errors import ToolError
from .state import RESEARCH_QUERY_OPERATIONS, State
from .transport import Transport, load_contract

RESULT_SCHEMA = {"type": "object", "properties": {
    "resultRef": {"type": "string"}, "pointer": {"type": "string", "default": ""},
    "offset": {"type": "integer", "minimum": 0, "default": 0},
    "limit": {"type": "integer", "minimum": 1, "maximum": 100, "default": 20}},
    "required": ["resultRef"], "additionalProperties": False}
EMPTY_SCHEMA = {"type": "object", "properties": {}, "additionalProperties": False}
DESCRIPTION_SCHEMA = {"type": "object", "properties": {"tool": {"type": "string"}},
                      "required": ["tool"], "additionalProperties": False}


class Runtime:
    def __init__(self, config: Config, *, contract=None, transport=None):
        # Daily control-plane budget is distinct from each persistent business task.
        self.config = replace(config, task_id="control:" + date.today().isoformat(), max_seconds=86400) if config.task_id == "interactive" else config
        self.task_max_seconds = config.max_seconds
        self.contract = contract or load_contract()
        self.transport = transport or Transport(config, self.contract)
        self.operations = {value["tool"]: (identifier, value) for identifier, value
                           in self.contract["operations"].items() if value["tool"]}
        self.state: State | None = None
        self.base_state: State | None = None
        self.instance_id: str | None = None
        self.bound_task = None
        self.session_checked = False
        self.lock = threading.RLock()

    def close(self) -> None:
        self.transport.close()

    def connect(self) -> State:
        instance = self.transport.call("getInstance", {})
        if not isinstance(instance, dict) or not isinstance(instance.get("id"), str):
            raise ToolError("INSTANCE_INVALID", "目标服务没有有效实例身份")
        if instance.get("apiVersion") != self.contract["apiVersion"]:
            raise ToolError("API_INCOMPATIBLE", "目标服务 API 版本与工具包不兼容")
        if self.instance_id is not None and instance["id"] != self.instance_id:
            raise ToolError("INSTANCE_CHANGED", "同地址实例已变化，请由可信端重新绑定")
        if self.base_state is None:
            self.instance_id = instance["id"]
            self.base_state = State(self.config, self.instance_id, self.contract["openapiSha256"])
            self.state = self.base_state
        return self.base_state

    def task_binding(self):
        if not self.session_checked:
            self.transport.active_token = None
            self.connect()
            binding = BoundTask.from_session(self.transport.call("getCurrentSession", {}))
            if binding and binding.kind == "author":
                binding.author_task(self.transport)
            self.bound_task = binding
            self.session_checked = True
        return self.bound_task

    def tools(self, *, discovery=True) -> list[dict]:
        with self.lock:
            binding = None
            if discovery:
                try:
                    binding = self.task_binding()
                except ToolError:
                    # Discovery also works offline. Execution independently checks the live instance and credential.
                    pass
            return self._tools(binding)

    def _tools(self, binding) -> list[dict]:
        result = [{"name": name, "description": value["description"],
                   "inputSchema": value["inputSchema"], "scope": value["scope"],
                   "readOnly": value["readOnly"]}
                  for name, (_, value) in self.operations.items()]
        result.extend([
            {"name": "platform.catalog", "description": "精简工具目录、权限与版本；目录可见不表示有执行许可。",
             "inputSchema": EMPTY_SCHEMA, "scope": "read", "readOnly": True},
            {"name": "platform.tool.describe", "description": "按需读取一个工具的完整契约。",
             "inputSchema": DESCRIPTION_SCHEMA, "scope": "read", "readOnly": True},
            {"name": "platform.result.read", "description": "按受控引用与JSON Pointer续读完整结果；不从截断预览重建计划。",
             "inputSchema": RESULT_SCHEMA, "scope": "read", "readOnly": True},
            {"name": "platform.budget", "description": "查看当前任务预算与未决操作，不续期、不扩权。",
             "inputSchema": EMPTY_SCHEMA, "scope": "read", "readOnly": True},
        ])
        from .files import tool_definitions
        result.extend(tool_definitions())
        from .intake import tool_definitions as intake_definitions
        result.extend(intake_definitions())
        from .cleanup import tool_definitions as cleanup_definitions
        result.extend(cleanup_definitions())
        from .research import tool_definitions as research_definitions
        result.extend(research_definitions())
        from .delegation import tool_definitions as delegation_definitions
        result.extend(delegation_definitions(self.contract))
        for tool in result:
            tool["inputSchema"] = copy.deepcopy(tool["inputSchema"])
            if tool["name"] not in {"task.start", "task.resume"}:
                tool["inputSchema"].setdefault("properties", {})["taskId"] = {"type": "string", "minLength": 8, "maxLength": 128,
                    "description": "task.start/resume返回的任务ID；可信执行器据此选择该任务的受限凭据与预算"}
            if binding:
                binding.present(tool)
        return result

    def task_state(self, task_id: str | None) -> State:
        state = self.connect()
        self.transport.active_token = state.task_credential(task_id) if task_id else None
        if task_id:
            state = State(replace(self.config, task_id=task_id, max_seconds=self.task_max_seconds), self.instance_id, self.contract["openapiSha256"])
        self.state = state
        return state

    def call(self, name: str, arguments: dict):
        # 一个执行器内部串行写与引用状态。多进程预算另外由SQLite事务保护。
        with self.lock:
            definition = next((tool for tool in self.tools(discovery=False) if tool["name"] == name), None)
            if definition is None:
                raise ToolError("TOOL_UNKNOWN", "工具未注册，不允许任意 HTTP 或脚本调用")
            if definition["scope"] not in self.config.permissions:
                raise ToolError("PERMISSION_DENIED", "可信端未授予此工具权限", details={"required": definition["scope"]})
            if not isinstance(arguments, dict):
                raise ToolError("INVALID_INPUT", "工具参数应为对象")
            arguments = dict(arguments)
            binding = self.task_binding()
            author_write = binding is not None and binding.kind == "author" and name == "author.profile.update"
            inferred_revision = author_write and isinstance(arguments.get("body"), dict) and "expectedRevision" not in arguments["body"]
            if binding:
                binding.bind(self, name, arguments)
            validator = Draft202012Validator(definition["inputSchema"], format_checker=FormatChecker())
            if next(validator.iter_errors(arguments), None) is not None:
                # 不把ValidationError.message回显，避免包含完整用户输入/本机路径。
                raise ToolError("INVALID_INPUT", "参数不符合工具契约，请按describe返回的schema修正")
            task_id = arguments.pop("taskId", None)
            state = self.task_state(task_id)
            if name == "platform.budget":
                return {"local": state.status(), "server": self.transport.call("getDelegatedBudget", {"id": task_id})} if task_id else state.status()
            key = None
            if not definition["readOnly"]:
                key = arguments.get("Idempotency-Key") or arguments.get("operationId")
                if not key:
                    raise ToolError("OPERATION_ID_REQUIRED", "写入必须提供稳定操作身份，重试沿用原值")
            request = state.reserve(name, arguments, key, author_task=binding.id if author_write else None,
                                    inferred_revision=inferred_revision)
            try:
                # Go reserves the task and research budgets together with the query key.
                if task_id and name not in RESEARCH_QUERY_OPERATIONS:
                    self.transport.call("reserveDelegatedCall", {"id": task_id, "Idempotency-Key": "call-" + secrets.token_hex(16)})
                value = self._dispatch(name, arguments, state)
            except ToolError as exc:
                state.finish(request, exc.as_dict(), key, uncertain=exc.uncertain, completed=False)
                raise
            except Exception:
                state.finish(request, {"code": "EXECUTOR_ERROR"}, key, uncertain=bool(key))
                raise ToolError("EXECUTOR_ERROR", "执行器未能确认结果，请回读原操作", uncertain=bool(key)) from None
            visible = value.metadata if isinstance(value, ImageResult) else value
            pending_query = name in RESEARCH_QUERY_OPERATIONS and isinstance(visible, dict) and visible.get("state") == "pending"
            state.finish(request, visible, key, uncertain=pending_query)
            try:
                presented = output.present(visible, state, self.config.output_bytes, self.config.token)
                return ImageResult(presented, value.data, value.mime_type) if isinstance(value, ImageResult) else presented
            except ToolError as exc:
                raise ToolError("RESULT_PRESENTATION_FAILED", "操作已返回，但结果无法完整呈现；按原ID回读",
                                details={"executionCompleted": True, "cause": exc.code}) from None

    def _dispatch(self, name: str, arguments: dict, state: State):
        if name == "platform.catalog":
            return {"platformVersion": __version__, "apiVersion": self.contract["apiVersion"],
                    "discovery": "full", "permissions": sorted(self.config.permissions),
                    "tools": [{key: tool[key] for key in ("name", "description", "scope", "readOnly")}
                              for tool in self.tools()],
                    "workflow": ["platform.permissions", "task.start", "task.context", "intake.register", "upload.file", "research.materials", "research.submit", "naming.suggest", "naming.preview",
                                 "plan.prepare", "plan.read", "plan.commit", "task.read", "backup.protect", "backup.protection.read", "source.recycle"],
                    "existingAssetWorkflow": ["asset.query", "asset.read", "task.start", "asset.task.context",
                                              "asset.research.materials", "platform.artifact.read", "asset.metadata.apply",
                                              "naming.suggest", "naming.preview", "asset.rename.apply"],
                    "authorTaskWorkflow": ["platform.session", "author.task.context", "author.profile.update", "author.read"],
                    "publicResearchWorkflow": ["research.context", "research.provider.query", "research.provider.result"],
                    "imageResearchWorkflow": ["research.context", "research.image.search", "research.provider.result"],
                    "rules": ["仅处理获授权批次；正常新增自动完成，冲突才等待用户。",
                              "计划在Go服务端持久化；用原计划ID和fingerprint提交。",
                              "候选与已核实身份分开；作者未知归其他搁置，不周期研究。",
                              "文本查证只发送选定的公开标识或搜索词。单帧查证须由实例、首次授权及当前任务开启，从服务端研究材料选一张派生帧。两类线索均需核实，保留人工选定的作者、出处和角色。",
                              "超时先按原操作键回读；预算不覆盖宿主未提供的模型费用。"]}
        if name == "platform.tool.describe":
            tool = next((item for item in self.tools() if item["name"] == arguments["tool"]), None)
            if tool is None:
                raise ToolError("TOOL_UNKNOWN", "不存在该工具")
            return tool
        if name == "platform.result.read":
            return output.page(state, arguments["resultRef"], arguments.get("pointer", ""),
                               arguments.get("offset", 0), arguments.get("limit", 20))
        if name in self.operations:
            return self.transport.call(self.operations[name][0], arguments)
        if name in {"task.start", "task.resume"}:
            from .delegation import dispatch
            return dispatch(self, name, arguments, state)
        if name == "research.probe":
            from .research import probe
            return probe(self, arguments)
        if name == "intake.register":
            from .intake import register
            return register(self, arguments)
        if name == "source.recycle":
            from .cleanup import recycle
            return recycle(self, arguments)
        if name == "platform.artifact.read":
            from .research import read_artifact
            return read_artifact(self, arguments["artifactRef"])
        from .files import dispatch
        return dispatch(self, name, arguments)
