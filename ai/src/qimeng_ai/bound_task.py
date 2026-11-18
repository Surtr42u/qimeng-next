"""Present inputs already fixed by a direct task credential without widening its authority."""
from __future__ import annotations

from .errors import ToolError


AUTHOR_TOOLS = {"author.task.context", "author.read", "author.profile.update"}


class BoundTask:
    def __init__(self, kind, identifier):
        self.kind, self.id = kind, identifier
        self.author_id = None
        self.fields = ()

    @classmethod
    def from_session(cls, session):
        if not isinstance(session, dict) or not isinstance(session.get("scopes"), list):
            raise ToolError("SESSION_INVALID", "服务未返回当前凭据的真实权限")
        if "delegate" in session["scopes"]:
            return None
        bindings = [(kind, session[field]) for kind, field in
                    (("intake", "batchId"), ("author", "authorTaskId"), ("asset", "assetTaskId"))
                    if isinstance(session.get(field), str) and session[field]]
        if len(bindings) > 1:
            raise ToolError("SESSION_INVALID", "凭据同时指向多个任务，无法确认工作范围")
        return cls(*bindings[0]) if bindings else None

    def author_task(self, transport):
        context = transport.call("getAuthorTask", {"id": self.id})
        task = context.get("task") if isinstance(context, dict) else None
        if (not isinstance(task, dict) or task.get("id") != self.id or not isinstance(task.get("authorId"), str)
                or task["authorId"] != context.get("author", {}).get("id") or not task["authorId"]
                or type(task.get("expectedRevision")) is not int or task["expectedRevision"] < 1
                or not isinstance(task.get("allowedFields"), list)):
            raise ToolError("TASK_RESPONSE_INVALID", "服务返回的维护目标与当前作者任务不一致")
        if self.author_id is not None and self.author_id != task["authorId"]:
            raise ToolError("TASK_RESPONSE_INVALID", "已绑定的作者任务改变了目标身份")
        self.author_id = task["authorId"]
        self.fields = tuple(task["allowedFields"])
        return task

    def present(self, tool):
        schema, name = tool["inputSchema"], tool["name"]
        schema["properties"].pop("taskId", None)
        if self.kind != "author" or name not in AUTHOR_TOOLS:
            return
        schema["properties"].pop("id", None)
        schema["required"] = [field for field in schema.get("required", []) if field != "id"]
        if name == "author.task.context":
            tool["description"] = "从 Go 读取本凭据唯一绑定的作者任务、获准字段、修订和可用动作。无需输入目标。"
        elif name == "author.read":
            tool["description"] = "从 Go 回读本任务作者的当前资料和字段状态。无需输入目标。"
        else:
            body = schema["properties"]["body"]
            body["properties"] = {field: spec for field, spec in body["properties"].items() if field in self.fields}
            body["required"] = []
            body["minProperties"] = 1
            body["additionalProperties"] = False
            tool["description"] = ("修改本任务作者的获准字段。body 仅传要改的资料，稳定操作键用于重放。"
                                   "执行器从 Go 取任务目标和授权基线，提交后用 author.read 回读。")

    def bind(self, runtime, name, args):
        if "taskId" in args and args.pop("taskId") != self.id:
            raise ToolError("TASK_NOT_BOUND", "本凭据不能切换到另一个任务")
        if self.kind != "author" or name not in AUTHOR_TOOLS:
            return
        if name == "author.task.context":
            target = self.id
        else:
            if self.author_id is None:
                self.author_task(runtime.transport)
            target = self.author_id
        if "id" in args and args["id"] != target:
            raise ToolError("task_scope_denied", "该目标不属于当前作者任务", status=403)
        args["id"] = target
        if name == "author.profile.update" and isinstance(args.get("body"), dict):
            body = dict(args["body"])
            key = args.get("Idempotency-Key")
            if "expectedRevision" not in body and isinstance(key, str) and 8 <= len(key) <= 128:
                state = runtime.connect()
                baseline = state.author_write_baseline(self.id, key)
                if baseline is None:
                    baseline = self.author_task(runtime.transport)["expectedRevision"]
                body["expectedRevision"] = baseline
            args["body"] = body
