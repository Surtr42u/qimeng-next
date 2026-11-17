"""可信端CLI与模型工具调用共用runtime；admin操作永不注册为MCP工具。"""
from __future__ import annotations

import argparse
import asyncio
import json
import sys

from .config import Config
from .content import ImageResult
from .errors import ToolError
from .runtime import Runtime


def main() -> None:
    parser = argparse.ArgumentParser(prog="qai")
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("mcp", help="启动标准 stdio MCP 服务")
    commands.add_parser("catalog", help="查询工具目录")
    call = commands.add_parser("call", help="执行一个工具；JSON 从stdin读取")
    call.add_argument("tool")
    admin = commands.add_parser("admin", help="可信端预算状态/核销/续期")
    actions = admin.add_subparsers(dest="action", required=True)
    actions.add_parser("status")
    reconcile = actions.add_parser("reconcile")
    reconcile.add_argument("key")
    renew = actions.add_parser("renew")
    renew.add_argument("--calls", type=int, required=True)
    renew.add_argument("--seconds", type=int, required=True)
    args = parser.parse_args()
    runtime = None
    try:
        if args.command == "mcp":
            from .mcp_server import run
            asyncio.run(run())
            return
        runtime = Runtime(Config.load())
        if args.command == "catalog":
            result = runtime.call("platform.catalog", {})
        elif args.command == "call":
            raw = sys.stdin.buffer.read(1024 * 1024 + 1)
            if len(raw) > 1024 * 1024:
                raise ToolError("INPUT_LIMIT", "输入超过1MiB上限")
            try:
                payload = json.loads(raw)
            except ValueError:
                raise ToolError("INVALID_INPUT", "stdin必须是JSON对象") from None
            result = runtime.call(args.tool, payload)
        else:
            state = runtime.connect()
            if args.action == "renew":
                state.renew(args.calls, args.seconds)
                result = state.status()
            elif args.action == "reconcile":
                operation = runtime.contract["operations"].get("getOperation")
                if operation is None:
                    raise ToolError("CAPABILITY_MISSING", "服务契约未提供操作回读，保留未决状态")
                key_name = next(p["name"] for p in operation["parameters"] if p["in"] == "path")
                result = state.reconcile(args.key, runtime.transport.call("getOperation", {key_name: args.key}))
            else:
                result = state.status()
        if isinstance(result, ImageResult):
            result = {**result.metadata, "mediaContent": "Image content is delivered through MCP; CLI returns metadata only."}
        print(json.dumps(result, ensure_ascii=False, indent=2))
    except ToolError as exc:
        print(json.dumps(exc.as_dict(), ensure_ascii=False), file=sys.stderr)
        raise SystemExit(2) from None
    finally:
        if runtime is not None:
            runtime.close()


if __name__ == "__main__":
    main()
