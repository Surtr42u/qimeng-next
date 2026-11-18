"""同提交 Go 测试提供独立服务与合成 TLS 上游。此进程只获得首次授权凭据。"""
import asyncio
import os
from pathlib import Path
import sys
import tempfile

from mcp import Client, StdioServerParameters


async def main():
    with tempfile.TemporaryDirectory() as temporary:
        params = StdioServerParameters(command=sys.executable, args=["-m", "qimeng_ai.cli", "mcp"], env={
            "PYTHONPATH": str(Path(__file__).resolve().parents[1] / "src"),
            "QIMENG_API_BASE": os.environ["QIMENG_TEST_BASE"],
            "QIMENG_API_TOKEN": os.environ["QIMENG_PUBLIC_TEST_GRANT"],
            "QIMENG_AI_STATE_DIR": str(Path(temporary) / "state"),
            "QIMENG_AI_TASK_ID": "public-provider-stdio",
            "QIMENG_AI_PERMISSIONS": "read,delegate,research",
        })
        async with Client(params) as client:
            async def call(name, arguments):
                response = await client.call_tool(name, arguments)
                if response.is_error:
                    raise AssertionError(f"{name}: {response.content}")
                return response.structured_content

            names = {tool.name for tool in (await client.list_tools()).tools}
            assert {"research.context", "research.provider.query", "research.provider.result"} <= names
            policy = await call("research.context", {})
            assert policy["enabled"] and policy["allowedProviders"] == ["fxtwitter"]
            started = await call("task.start", {"operationId": "stdio-public-start", "body": {
                "kind": "author", "label": "Synthetic public author lookup",
                "authorId": os.environ["QIMENG_PUBLIC_TEST_AUTHOR"], "expectedRevision": 1,
                "candidateBlocks": ["basic"], "researchProviders": ["fxtwitter"],
            }})
            task_id = started["taskId"]
            assert "credential" not in started
            args = {"taskId": task_id, "provider": "fxtwitter", "Idempotency-Key": "stdio-stable-query",
                    "body": {"operation": "author", "target": "@fixture"}}
            result = await call("research.provider.query", args)
            assert result["state"] == "ok" and result["items"][0]["url"] == "https://x.com/fixture"
            assert result["subjectId"] == os.environ["QIMENG_PUBLIC_TEST_AUTHOR"]
            replay = await call("research.provider.query", args)
            assert replay == result
            budget = await call("platform.budget", {"taskId": task_id})
            assert budget["server"]["calls"] == 1
            receipt = await call("research.provider.result", {"taskId": task_id, "id": result["id"]})
            assert receipt == result
            context = await call("research.context", {"taskId": task_id})
            assert context["remainingResearchCalls"] == 0
            assert context["queries"][0]["id"] == result["id"]
            exhausted = await client.call_tool("research.provider.query", {
                **args, "Idempotency-Key": "no-second-query"})
            assert exhausted.is_error


if __name__ == "__main__":
    asyncio.run(main())
