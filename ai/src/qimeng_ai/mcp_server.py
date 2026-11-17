"""官方 MCP SDK stdio 适配，无私有协议或第二套业务分派。"""
from __future__ import annotations

import asyncio
import json

from mcp.server import Server, ServerRequestContext
from mcp.server.stdio import stdio_server
from mcp.types import CallToolRequestParams, CallToolResult, ListToolsResult, PaginatedRequestParams, TextContent, Tool

from .config import Config
from .errors import ToolError
from .runtime import Runtime


def create_server(runtime: Runtime) -> Server:
    async def list_tools(ctx: ServerRequestContext, params: PaginatedRequestParams | None) -> ListToolsResult:
        return ListToolsResult(tools=[Tool(name=tool["name"], description=tool["description"],
                                         input_schema=tool["inputSchema"])
                                    for tool in runtime.tools()])

    async def call_tool(ctx: ServerRequestContext, params: CallToolRequestParams) -> CallToolResult:
        try:
            value = await asyncio.to_thread(runtime.call, params.name, params.arguments or {})
            failed = False
        except ToolError as exc:
            value, failed = exc.as_dict(), True
        text = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
        return CallToolResult(content=[TextContent(type="text", text=text)],
                              structured_content=value if isinstance(value, dict) else {"result": value},
                              is_error=failed)

    return Server("qimeng-next", version="0.1.0", on_list_tools=list_tools, on_call_tool=call_tool)


async def run() -> None:
    runtime = Runtime(Config.load())
    try:
        server = create_server(runtime)
        async with stdio_server() as (incoming, outgoing):
            await server.run(incoming, outgoing, server.create_initialization_options())
    finally:
        runtime.close()
