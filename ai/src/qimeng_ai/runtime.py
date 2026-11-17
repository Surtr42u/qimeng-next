"""CLI/MCP 共用执行路径：契约、许可、预算、调用、结果呈现。"""
from __future__ import annotations

import threading
from jsonschema import Draft202012Validator, FormatChecker

from . import __version__, output
from .config import Config
from .content import ImageResult
from .errors import ToolError
from .state import State
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
        self.config = config
        self.contract = contract or load_contract()
        self.transport = transport or Transport(config, self.contract)
        self.operations = {value["tool"]: (identifier, value) for identifier, value
                           in self.contract["operations"].items() if value["tool"]}
        self.state: State | None = None
        self.instance_id: str | None = None
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
        if self.state is None:
            self.instance_id = instance["id"]
            self.state = State(self.config, self.instance_id, self.contract["openapiSha256"])
        return self.state

    def tools(self) -> list[dict]:
        result = [{"name": name, "description": value["description"],
                   "inputSchema": value["inputSchema"], "scope": value["scope"],
                   "readOnly": value["method"] in {"GET", "HEAD"}}
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
        from .research import tool_definitions as research_definitions
        result.extend(research_definitions())
        return result

    def call(self, name: str, arguments: dict):
        # 一个执行器内部串行写与引用状态。多进程预算另外由SQLite事务保护。
        with self.lock:
            definition = next((tool for tool in self.tools() if tool["name"] == name), None)
            if definition is None:
                raise ToolError("TOOL_UNKNOWN", "工具未注册，不允许任意 HTTP 或脚本调用")
            if definition["scope"] not in self.config.permissions:
                raise ToolError("PERMISSION_DENIED", "可信端未授予此工具权限", details={"required": definition["scope"]})
            validator = Draft202012Validator(definition["inputSchema"], format_checker=FormatChecker())
            if next(validator.iter_errors(arguments), None) is not None:
                # 不把ValidationError.message回显，避免包含完整用户输入/本机路径。
                raise ToolError("INVALID_INPUT", "参数不符合工具契约，请按describe返回的schema修正")
            state = self.connect()
            if name == "platform.budget":
                return state.status()
            key = None
            if not definition["readOnly"]:
                key = arguments.get("Idempotency-Key") or arguments.get("operationId")
                if not key:
                    raise ToolError("OPERATION_ID_REQUIRED", "写入必须提供稳定操作身份，重试沿用原值")
            request = state.reserve(name, arguments, key)
            try:
                value = self._dispatch(name, arguments, state)
            except ToolError as exc:
                state.finish(request, exc.as_dict(), key, uncertain=exc.uncertain)
                raise
            except Exception:
                state.finish(request, {"code": "EXECUTOR_ERROR"}, key, uncertain=bool(key))
                raise ToolError("EXECUTOR_ERROR", "执行器未能确认结果，请回读原操作", uncertain=bool(key)) from None
            visible = value.metadata if isinstance(value, ImageResult) else value
            state.finish(request, visible, key)
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
                    "workflow": ["task.context", "files.search", "upload.file", "research.submit",
                                 "plan.prepare", "plan.read", "plan.commit", "task.read"],
                    "rules": ["仅处理获授权批次；正常新增自动完成，冲突才等待用户。",
                              "计划在Go服务端持久化；用原计划ID和fingerprint提交。",
                              "候选与已核实身份分开；作者未知归其他搁置，不周期研究。",
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
        if name == "research.probe":
            from .research import probe
            return probe(self, arguments)
        if name == "platform.artifact.read":
            from .research import read_artifact
            return read_artifact(self, arguments["artifactRef"])
        from .files import dispatch
        return dispatch(self, name, arguments)
