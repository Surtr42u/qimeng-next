"""可信执行器派生任务凭据；模型只得到任务身份，不接触凭据材料。"""
from __future__ import annotations

import copy

from .errors import ToolError


def tool_definitions(contract):
    start = copy.deepcopy(contract["operations"]["startDelegatedTask"]["inputSchema"]["properties"]["body"])
    start["properties"].pop("credentialSecret", None)
    start["required"] = [key for key in start.get("required", []) if key != "credentialSecret"]
    start["additionalProperties"] = False
    identity = {"type": "string", "minLength": 8, "maxLength": 128}
    return [
        {"name": "task.start", "scope": "delegate", "readOnly": False,
         "description": "有 delegate 权限的长期凭据先用 platform.permissions 读授权范围，再创建任务或认领已有接收批次。维护作者先按姓名查询真实 authorId 和 revision，使用 kind=author 及获准字段。回收站、恢复或移动作品须在 kind=asset 任务传 manageFiles=true。后续调用携带返回的 taskId。若 platform.session 已有 authorTaskId，可直接读该任务上下文。",
         "inputSchema": {"type": "object", "properties": {"operationId": identity, "body": start},
                         "required": ["operationId", "body"], "additionalProperties": False}},
        {"name": "task.resume", "scope": "delegate", "readOnly": False,
         "description": "在新宿主/进程接续原任务，保留计划、预算与操作身份；轮换任务执行凭据，旧执行器停止写入。不会重新上传或重新编号。",
         "inputSchema": {"type": "object", "properties": {"operationId": identity, "id": {"type": "string"}},
                         "required": ["operationId", "id"], "additionalProperties": False}},
    ]


def dispatch(runtime, name, arguments, state):
    identity = name + ":" + arguments["operationId"] + ":" + arguments.get("id", "")
    secret = state.executor_secret(identity)
    if name == "task.start":
        operation = "startDelegatedTask"
        parameters = {"Idempotency-Key": arguments["operationId"], "body": {**arguments["body"], "credentialSecret": secret}}
    else:
        operation = "resumeDelegatedTask"
        parameters = {"id": arguments["id"], "Idempotency-Key": arguments["operationId"], "body": {"credentialSecret": secret}}
    result = runtime.transport.call(operation, parameters)
    try:
        state.attach_task_credential(result, secret)
    except ToolError:
        raise
    except Exception:
        raise ToolError("TASK_CREDENTIAL_SAVE_FAILED", "任务已创建，但本地执行凭据未确认保存；沿原operationId重试", uncertain=True) from None
    return {key: value for key, value in result.items() if key != "credential"} | {
        "next": {"intake": "携带 taskId 读取 task.context。", "author": "携带 taskId 读取 author.task.context。",
                 "asset": "携带 taskId 读取 asset.task.context。在授权范围内使用 asset.research.materials、asset.metadata.apply 和 Go 命名预览。"}[result["kind"]],
        "credentialManagedBy": "trusted-executor",
    }
