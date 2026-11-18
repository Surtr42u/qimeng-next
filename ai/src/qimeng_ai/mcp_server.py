"""官方 MCP SDK stdio 适配，无私有协议或第二套业务分派。"""
from __future__ import annotations

import asyncio
import base64
import json

from mcp.server import Server, ServerRequestContext
from mcp.server.stdio import stdio_server
from mcp.types import CallToolRequestParams, CallToolResult, ImageContent, ListToolsResult, PaginatedRequestParams, TextContent, Tool, ToolAnnotations

from .config import Config
from .content import ImageResult
from .errors import ToolError
from .runtime import Runtime
from .state import RESEARCH_QUERY_OPERATIONS


def create_server(runtime: Runtime) -> Server:
    async def list_tools(ctx: ServerRequestContext, params: PaginatedRequestParams | None) -> ListToolsResult:
        tools = await asyncio.to_thread(runtime.tools)
        return ListToolsResult(tools=[Tool(name=tool["name"], description=tool["description"],
                                         input_schema=tool["inputSchema"],
                                         annotations=ToolAnnotations(read_only_hint=tool["readOnly"],
                                             destructive_hint=not tool["readOnly"],
                                             idempotent_hint=tool["readOnly"] or any(key in tool["inputSchema"].get("required", []) for key in ("operationId", "Idempotency-Key")),
                                             open_world_hint=tool["name"] in RESEARCH_QUERY_OPERATIONS))
                                    for tool in sorted(tools, key=lambda item: item["name"])])

    async def call_tool(ctx: ServerRequestContext, params: CallToolRequestParams) -> CallToolResult:
        try:
            value = await asyncio.to_thread(runtime.call, params.name, params.arguments or {})
            failed = False
        except ToolError as exc:
            value, failed = exc.as_dict(), True
        picture = value if isinstance(value, ImageResult) else None
        if picture is not None:
            value = picture.metadata
        text = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
        blocks = [TextContent(type="text", text=text)]
        if picture is not None:
            blocks.append(ImageContent(type="image", data=base64.b64encode(picture.data).decode(), mime_type=picture.mime_type))
        return CallToolResult(content=blocks,
                              structured_content=value if isinstance(value, dict) else {"result": value},
                              is_error=failed)

    return Server("qimeng-next", version="0.1.0", on_list_tools=list_tools, on_call_tool=call_tool,
                  instructions="绮梦媒体库。只读检索可直接查询。单任务凭据已经绑定工作对象，按工具列出的参数调用。作者维护可直接提交本次获准修改的资料，用 author.read 回读。长期凭据用 platform.permissions 确认范围，再由 task.start 创建或认领任务。身份、修订、证据与执行结果从实际记录读取。")


async def run() -> None:
    runtime = Runtime(Config.load())
    try:
        server = create_server(runtime)
        async with stdio_server() as (incoming, outgoing):
            await server.run(incoming, outgoing, server.create_initialization_options())
    finally:
        runtime.close()
