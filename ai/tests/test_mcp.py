"""官方SDK真实协议层：发现完整清单、统一错误和受限写入。"""
from pathlib import Path
import tempfile
import unittest

from mcp import Client

from qimeng_ai.config import Config
from qimeng_ai.mcp_server import create_server
from qimeng_ai.runtime import Runtime
from qimeng_ai.transport import load_contract


class SyntheticTransport:
    def __init__(self):
        self.calls = []

    def call(self, operation, arguments, **kwargs):
        self.calls.append(operation)
        if operation == "getInstance":
            return {"id": "synthetic-instance", "apiVersion": "0.1.0"}
        if operation == "getCurrentSession":
            return {"id": "synthetic-reader", "scopes": ["read"]}
        if operation == "listAuthors":
            return {"items": [{"id": "synthetic-author", "name": "Synthetic"}]}
        raise AssertionError("unexpected mutation or operation")

    def close(self):
        pass


class MCPProtocolTest(unittest.IsolatedAsyncioTestCase):
    async def test_full_discovery_query_and_permission_failure_share_runtime(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Config.load({"QIMENG_API_BASE": "http://127.0.0.1:18481", "QIMENG_API_TOKEN": "synthetic-only",
                                  "QIMENG_AI_STATE_DIR": directory})
            transport = SyntheticTransport()
            runtime = Runtime(config, contract=load_contract(), transport=transport)
            async with Client(create_server(runtime), raise_exceptions=True) as client:
                tools = await client.list_tools()
                names = {tool.name for tool in tools.tools}
                self.assertIn("author.query", names)
                self.assertIn("plan.commit", names)
                self.assertNotIn("createCredential", names)
                query = await client.call_tool("author.query", {})
                self.assertFalse(query.is_error)
                self.assertEqual(query.structured_content["items"][0]["id"], "synthetic-author")
                denied = await client.call_tool("plan.commit", {})
                self.assertTrue(denied.is_error)
                self.assertEqual(denied.structured_content["code"], "PERMISSION_DENIED")
                self.assertNotIn("commitPlan", transport.calls)
