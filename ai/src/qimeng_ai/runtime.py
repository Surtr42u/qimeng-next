"""CLI/MCP 共用执行路径：契约、许可、预算、调用、结果呈现。"""
from __future__ import annotations

import threading
import copy
import secrets
import shutil
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


def media_tools_available() -> bool:
    return bool(shutil.which("ffprobe") and shutil.which("ffmpeg"))


class Runtime:
    def __init__(self, config: Config, *, contract=None, transport=None):
        self.config = config
        self.contract = contract or load_contract()
        self.transport = transport or Transport(config, self.contract)
        self.operations = {value["tool"]: (identifier, value) for identifier, value
                           in self.contract["operations"].items() if value["tool"]}
        self.state: State | None = None
        self.base_state: State | None = None
        self.instance_id: str | None = None
        self.instance: dict | None = None
        self.session: dict | None = None
        self.delegation_policy: dict | None = None
        self.delegation_checked = False
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
        self.instance = instance
        config = self.config
        daily_control = config.task_id == "interactive"
        if daily_control:
            config = replace(config, task_id="control:" + date.today().isoformat(), max_seconds=86400)
        if self.base_state is None or self.base_state.config.task_id != config.task_id:
            self.instance_id = instance["id"]
            self.base_state = State(config, self.instance_id, self.contract["openapiSha256"], daily_control=daily_control)
            self.state = self.base_state
        return self.base_state

    def task_binding(self):
        if not self.session_checked:
            self.transport.active_token = None
            self.connect()
            session = self.transport.call("getCurrentSession", {})
            binding = BoundTask.from_session(session)
            self.session = session
            if not self.delegation_checked and "delegate" in session.get("scopes", []):
                self.delegation_checked = True
                try:
                    policy = self.transport.call("getCurrentAIDelegation", {})
                    self.delegation_policy = policy if isinstance(policy, dict) else None
                except ToolError:
                    # A traditional credential may carry delegate without a standing grant.
                    self.delegation_policy = None
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

    def _tools(self, binding, *, filtered: bool = True) -> list[dict]:
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
            if tool["name"] not in {"task.start", "task.resume", "author.merge", "library.scan"}:
                # Standing-only operations must not accept a task credential.
                tool["inputSchema"].setdefault("properties", {})["taskId"] = {"type": "string", "minLength": 8, "maxLength": 128,
                    "description": "task.start/resume返回的任务ID；可信执行器据此选择该任务的受限凭据与预算"}
            if binding:
                binding.present(tool)
        return [tool for tool in result if self._visible(tool)] if filtered else result

    def tool_definition(self, name: str):
        """Unfiltered definition, used only to explain why an action is refused."""
        return next((tool for tool in self._tools(None, filtered=False) if tool["name"] == name), None)

    def _capability(self, name: str):
        """A missing or non-boolean capability is unknown, not a proven absence."""
        if not isinstance(self.instance, dict):
            return None
        value = (self.instance.get("capabilities") or {}).get(name)
        return value if isinstance(value, bool) else None

    def _visible(self, tool: dict) -> bool:
        """Hide only tools whose prerequisites are provably absent here.

        The server still enforces its own scopes; this filter prevents giving a
        model a tool that this executor could only reject.
        """
        name, scope = tool["name"], tool["scope"]
        permissions = self.config.permissions
        if scope not in permissions:
            return False
        storages = bool(self.config.storages)
        if name in {"files.search", "upload.file", "intake.register", "research.probe"} and not storages:
            return False
        if name in {"files.search", "upload.file", "intake.register", "research.probe", "source.recycle"} and "local.read" not in permissions:
            return False
        if name == "source.recycle" and not self.config.recycle_storages:
            return False
        if name == "research.probe" and ("local.research" not in permissions or not media_tools_available()):
            return False
        if name == "platform.artifact.read" and (not self.config.allow_media_to_model or
                not ({"research", "local.research"} & permissions)):
            return False
        if name in {"backup.protect", "backup.protection.retry"} and self._capability("backupTargets") is False:
            return False
        if name in {"author.merge", "library.scan"}:
            session = self.session if isinstance(self.session, dict) else {}
            if any(session.get(key) for key in ("batchId", "authorTaskId", "assetTaskId")):
                return False
        session = self.session if isinstance(self.session, dict) else {}
        if session.get("assetTaskId"):
            scopes = set(session.get("scopes") or [])
            if name == "asset.tags.replace" and "asset.tags.replace" not in scopes:
                return False
            if name in {"asset.trash", "asset.restore", "asset.move"} and "asset.files.write" not in scopes:
                return False
            if name in {"asset.metadata.apply", "asset.rename.apply"} and "asset.metadata.write" not in scopes:
                return False
            if name == "asset.research.materials" and "research" not in scopes:
                return False
        policy = self.delegation_policy
        if isinstance(policy, dict):
            if name == "author.merge" and policy.get("canMergeAuthors") is not True:
                return False
            if name == "library.scan" and (policy.get("canOrganize") is not True or policy.get("canIngest") is not True):
                return False
            if name == "asset.tags.replace" and (policy.get("canReplaceAssetMetadata") is not True or
                    "tags" not in (policy.get("assetFields") or [])):
                return False
            if name in {"asset.trash", "asset.restore", "asset.move"} and policy.get("canManageAssetFiles") is not True:
                return False
        return True

    def task_state(self, task_id: str | None) -> State:
        binding = self.task_binding()
        state = self.connect()
        if binding:
            if task_id is not None and task_id != binding.id:
                raise ToolError("TASK_NOT_BOUND", "本凭据不能切换到另一个任务")
            task_id = binding.id if binding.delegated else None
            self.transport.active_token = None
        else:
            self.transport.active_token = state.task_credential(task_id) if task_id else None
        if task_id:
            state = State(replace(self.config, task_id=task_id), self.instance_id, self.contract["openapiSha256"])
        self.state = state
        return state

    def call(self, name: str, arguments: dict):
        # 一个执行器内部串行写与引用状态。多进程预算另外由SQLite事务保护。
        with self.lock:
            tools = self.tools(discovery=False)
            definition = next((tool for tool in tools if tool["name"] == name), None)
            if definition is None:
                raise ToolError("TOOL_UNKNOWN", "工具未注册，不允许任意 HTTP 或脚本调用")
            if definition["scope"] not in self.config.permissions:
                raise ToolError("PERMISSION_DENIED", "可信端未授予此工具权限", details={"required": definition["scope"]})
            if not isinstance(arguments, dict):
                raise ToolError("INVALID_INPUT", "工具参数应为对象")
            arguments = dict(arguments)
            binding = self.task_binding()
            if not self._visible(definition):
                # The instance capability probe runs during binding, so a tool
                # hidden by the live instance must also be refused here.
                raise ToolError("TOOL_UNKNOWN", "该工具在当前执行器配置或目标实例下不可用")
            author_write = binding is not None and binding.kind == "author" and name == "author.profile.update"
            inferred_revision = author_write and isinstance(arguments.get("body"), dict) and "expectedRevision" not in arguments["body"]
            verify_author_write = author_write and (inferred_revision or "id" not in arguments)
            if binding:
                binding.bind(self, name, arguments)
            validator = Draft202012Validator(definition["inputSchema"], format_checker=FormatChecker())
            if next(validator.iter_errors(arguments), None) is not None:
                # 不把ValidationError.message回显，避免包含完整用户输入/本机路径。
                raise ToolError("INVALID_INPUT", "参数不符合工具契约，请按describe返回的schema修正")
            task_id = arguments.pop("taskId", None)
            if binding and binding.delegated:
                task_id = binding.id
            state = self.task_state(task_id)
            if name == "platform.budget":
                return {"local": state.status(), "server": self.transport.call("getDelegatedBudget", {"id": task_id})} if task_id else state.status()
            key = None
            if not definition["readOnly"]:
                key = arguments.get("Idempotency-Key") or arguments.get("operationId")
                if not key:
                    raise ToolError("OPERATION_ID_REQUIRED", "写入必须提供稳定操作身份，重试沿用原值")

            def dispatch():
                value = self._dispatch(name, arguments, state)
                if name == "task.context":
                    from .task_actions import present_batch_actions
                    value = present_batch_actions(self, value, tools)
                return value

            value = self._metered(state, task_id, name, arguments, key, dispatch,
                                  author_task=binding.id if author_write else None, inferred_revision=inferred_revision)
            if name == "platform.result.read":
                # A cached result was already redacted. Redacting a text slice can change it again.
                return value
            visible = value.metadata if isinstance(value, ImageResult) else value
            if verify_author_write:
                visible = binding.readback(self.transport, key, arguments["body"], visible)
            try:
                presented = output.present(visible, state, self.config.output_bytes, self.config.token)
                return ImageResult(presented, value.data, value.mime_type) if isinstance(value, ImageResult) else presented
            except ToolError as exc:
                raise ToolError("RESULT_PRESENTATION_FAILED", "操作已返回，但结果无法完整呈现；按原ID回读",
                                details={"executionCompleted": True, "cause": exc.code}) from None

    def read_worker_page(self, task_id, identity, read):
        """A continuation of a worker-held Go snapshot uses the same live lease and call budgets."""
        with self.lock:
            if "read" not in self.config.permissions:
                raise ToolError("PERMISSION_DENIED", "可信端未授予读取权限")
            state = self.task_state(task_id)
            return self._metered(state, task_id, "worker.page", identity, None, read)

    def _metered(self, state, task_id, name, arguments, key, dispatch, *, author_task=None, inferred_revision=False):
        request = state.reserve(name, arguments, key, author_task=author_task, inferred_revision=inferred_revision)
        try:
            # Go reserves the task and research budgets together with the query key.
            if task_id and name not in RESEARCH_QUERY_OPERATIONS:
                self.transport.call("reserveDelegatedCall", {"id": task_id, "Idempotency-Key": "call-" + secrets.token_hex(16)})
            value = dispatch()
        except ToolError as exc:
            state.finish(request, exc.as_dict(), key, uncertain=exc.uncertain, completed=False)
            raise
        except Exception:
            state.finish(request, {"code": "EXECUTOR_ERROR"}, key, uncertain=bool(key))
            raise ToolError("EXECUTOR_ERROR", "执行器未能确认结果，请回读原操作", uncertain=bool(key)) from None
        visible = value.metadata if isinstance(value, ImageResult) else value
        pending_query = name in RESEARCH_QUERY_OPERATIONS and isinstance(visible, dict) and visible.get("state") == "pending"
        state.finish(request, visible, key, uncertain=pending_query)
        return value

    def _dispatch(self, name: str, arguments: dict, state: State):
        if name == "platform.catalog":
            tools = self.tools()
            visible = {tool["name"] for tool in tools}
            catalog = {"platformVersion": __version__, "apiVersion": self.contract["apiVersion"],
                       "discovery": "full", "permissions": sorted(self.config.permissions),
                       "tools": [{key: tool[key] for key in ("name", "description", "scope", "readOnly")}
                                 for tool in tools],
                       "workflow": ["platform.permissions", "task.start", "task.context", "intake.register", "upload.file", "research.materials", "research.submit", "naming.suggest", "naming.preview",
                                    "plan.prepare", "plan.read", "plan.commit", "task.read", "backup.protect", "backup.protection.read", "source.recycle"],
                       "existingAssetWorkflow": ["asset.query", "asset.read", "task.start", "asset.task.context",
                                                 "asset.research.materials", "platform.artifact.read", "asset.metadata.apply",
                                                 "asset.tags.replace", "naming.suggest", "naming.preview", "asset.rename.apply"],
                       "assetFileWorkflow": ["asset.query", "asset.read", "task.start", "asset.trash", "asset.restore",
                                             "asset.move", "trash.query", "asset.read"],
                       "libraryScanWorkflow": ["library.query", "library.scan", "library.scan.read"],
                       "authorCleanupWorkflow": ["author.query", "author.read", "author.merge"],
                       "authorTaskWorkflow": ["platform.session", "author.task.context", "author.profile.update", "author.read"],
                       "publicResearchWorkflow": ["research.context", "research.provider.query", "research.provider.result"],
                       "imageResearchWorkflow": ["research.context", "research.image.search", "research.provider.result"],
                       "rules": ["仅处理获授权批次；正常新增自动完成，冲突才等待用户。",
                                 "计划在Go服务端持久化；用原计划ID和fingerprint提交。",
                                 "候选与已核实身份分开；作者未知归其他搁置，不周期研究。",
                                 "文本查证只发送选定的公开标识或搜索词。单帧查证须由实例、首次授权及当前任务开启，从服务端研究材料选一张派生帧。两类线索均需核实，保留人工选定的作者、出处和角色。",
                                 "超时先按原操作键回读；预算不覆盖宿主未提供的模型费用。"]}
            for key in ("workflow", "existingAssetWorkflow", "assetFileWorkflow", "libraryScanWorkflow",
                        "authorCleanupWorkflow", "authorTaskWorkflow", "publicResearchWorkflow", "imageResearchWorkflow"):
                catalog[key] = [tool for tool in catalog[key] if tool in visible]
            return catalog
        if name == "platform.tool.describe":
            tool = next((item for item in self.tools() if item["name"] == arguments["tool"]), None)
            if tool is None:
                raise ToolError("TOOL_UNKNOWN", "不存在该工具")
            return tool
        if name == "platform.result.read":
            return output.page(state, arguments["resultRef"], arguments.get("pointer", ""),
                               arguments.get("offset", 0), arguments.get("limit", 20), max_bytes=self.config.output_bytes)
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
