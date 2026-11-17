"""云端同提交Go服务+真正stdio MCP子进程，只有显式隔离地址才执行。"""
import asyncio
import base64
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
import urllib.request

from mcp import Client, StdioServerParameters


@unittest.skipUnless(os.environ.get("QIMENG_TEST_BASE") == "http://127.0.0.1:18481", "requires cloud isolated Go service")
class LiveMCP(unittest.IsolatedAsyncioTestCase):
    def request(self, method, path, body=None, *, key=None):
        headers = {"Authorization": "Bearer " + os.environ["QIMENG_E2E_TOKEN"]}
        data = None
        if body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        if key:
            headers["Idempotency-Key"] = key
        request = urllib.request.Request(os.environ["QIMENG_TEST_BASE"] + "/api/v1" + path, data=data, headers=headers, method=method)
        with urllib.request.urlopen(request, timeout=30) as response:
            raw = response.read()
            return json.loads(raw) if raw else None

    def process(self, root, token, permissions):
        return StdioServerParameters(command=sys.executable, args=["-m", "qimeng_ai.cli", "mcp"], env={
            "PYTHONPATH": str(Path(__file__).resolve().parents[1] / "src"),
            "QIMENG_API_BASE": os.environ["QIMENG_TEST_BASE"], "QIMENG_API_TOKEN": token,
            "QIMENG_AI_STATE_DIR": str(root / "state"), "QIMENG_AI_TASK_ID": "cloud-live",
            "QIMENG_AI_STORAGES": json.dumps({"input": str(root)}),
            "QIMENG_AI_PERMISSIONS": permissions, "QIMENG_AI_MAX_SECONDS": "900",
        })

    async def test_stdio_intake_and_author_maintenance(self):
        if os.environ.get("CI") != "true":
            self.fail("live tests must run in cloud")
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            # Complete, generated image fixture; no real user file is ever read.
            import subprocess
            subprocess.run(["ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=orange:s=64x48", "-frames:v", "1", str(root / "sample.png")], check=True)
            library = self.request("POST", "/libraries", {"name": "MCP synthetic", "kind": "regular"}, key="mcp-live-library")
            batch = self.request("POST", "/batches", {"libraryId": library["id"], "mode": "intelligent", "label": "MCP fixture"}, key="mcp-live-batch")
            credential = self.request("POST", "/credentials", {"label": "MCP fixture", "batchId": batch["id"], "scopes": ["read", "ingest", "research", "organize"]})
            try:
                async with Client(self.process(root, credential["token"], "read,ingest,research,organize,local.read")) as client:
                    names = {tool.name for tool in (await client.list_tools()).tools}
                    self.assertIn("upload.file", names)
                    self.assertIn("plan.commit", names)
                    async def call(tool, arguments):
                        result = await client.call_tool(tool, arguments)
                        self.assertFalse(result.is_error, result.content)
                        return result.structured_content
                    listing = await call("files.search", {"storage": "input", "query": "sample.png"})
                    uploaded = await call("upload.file", {"fileRef": listing["items"][0]["fileRef"], "operationId": "mcp-live-upload",
                                          "libraryId": library["id"], "batchId": batch["id"], "fileName": "sample.png", "mode": "intelligent"})
                    self.assertEqual(uploaded["status"], "staged")
                    context = await call("task.context", {"id": batch["id"]})
                    await call("research.submit", {"id": batch["id"], "Idempotency-Key": "mcp-live-research",
                        "body": {"expectedRevision": context["batch"]["revision"], "uploadId": uploaded["upload"]["id"],
                                 "expectedHash": uploaded["sha256"], "outcome": "unknown", "candidates": [], "callsUsed": 1}})
                    context = await call("task.context", {"id": batch["id"]})
                    plan = await call("plan.prepare", {"id": batch["id"], "Idempotency-Key": "mcp-live-plan",
                        "body": {"expectedRevision": context["batch"]["revision"], "items": [{"uploadId": uploaded["upload"]["id"], "family": "Synthetic  Figure"}]}})
                    arguments = {"id": batch["id"], "planId": plan["id"], "Idempotency-Key": "mcp-live-commit", "body": {"fingerprint": plan["fingerprint"]}}
                    await call("plan.commit", arguments)
                    await call("plan.commit", arguments)
                    assets = await call("asset.query", {"libraryId": library["id"]})
                    self.assertEqual(len(assets["items"]), 1)
                    self.assertEqual(assets["items"][0]["hash"], uploaded["sha256"])
                    self.assertIn("unknown", assets["items"][0]["authorIds"])
            finally:
                self.request("DELETE", "/credentials/" + credential["id"])
            author = self.request("POST", "/authors", {"kind": "regular", "name": "MCP synthetic author", "biography": "Original"}, key="mcp-live-author")
            task = self.request("POST", "/author-tasks", {"authorId": author["id"], "expectedRevision": author["revision"], "allowedFields": ["biography"], "allowClear": True}, key="mcp-live-author-task")
            credential = self.request("POST", "/credentials", {"label": "MCP author", "authorTaskId": task["id"], "scopes": ["read", "author.profile.write"]})
            try:
                async with Client(self.process(root, credential["token"], "read,author.profile.write")) as client:
                    result = await client.call_tool("author.profile.update", {"id": author["id"], "Idempotency-Key": "mcp-live-clear",
                                                    "body": {"expectedRevision": author["revision"], "biography": ""}})
                    self.assertFalse(result.is_error, result.content)
                    self.assertEqual(result.structured_content["biography"], "")
                    denied = await client.call_tool("plan.commit", {})
                    self.assertTrue(denied.is_error)
            finally:
                self.request("DELETE", "/credentials/" + credential["id"])
