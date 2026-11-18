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
        return ListToolsResult(tools=[Tool(name=tool["name"], description=tool["description"],
                                         input_schema=tool["inputSchema"],
                                         annotations=ToolAnnotations(read_only_hint=tool["readOnly"],
                                             destructive_hint=not tool["readOnly"],
                                             idempotent_hint=tool["readOnly"] or any(key in tool["inputSchema"].get("required", []) for key in ("operationId", "Idempotency-Key")),
                                             open_world_hint=tool["name"] in RESEARCH_QUERY_OPERATIONS))
                                    for tool in sorted(runtime.tools(), key=lambda item: item["name"])])

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
                  instructions="绮梦媒体库：先读取platform.catalog与任务context。AI提供意图和证据；后端负责匹配、编号、冲突、事务与恢复。不要自行扫描最大编号，不从截断摘要重建计划。工具清单可见不代表许可；正常授权任务继续，冲突只暂停相关项。")


async def run() -> None:
    runtime = Runtime(Config.load())
    try:
        server = create_server(runtime)
        async with stdio_server() as (incoming, outgoing):
            await server.run(incoming, outgoing, server.create_initialization_options())
    finally:
        runtime.close()
