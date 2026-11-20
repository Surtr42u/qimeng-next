"""官方SDK真实协议层：发现完整清单、统一错误和受限写入。"""
import json
from pathlib import Path
import tempfile
import unittest

from mcp import Client

from qimeng_ai.config import Config
from qimeng_ai.mcp_server import create_server
from qimeng_ai.runtime import Runtime
from qimeng_ai.state import encode
from qimeng_ai.transport import load_contract


class SyntheticTransport:
    def __init__(self, authors=None):
        self.calls = []
        self.authors = authors if authors is not None else [{"id": "synthetic-author", "name": "Synthetic"}]

    def call(self, operation, arguments, **kwargs):
        self.calls.append(operation)
        if operation == "getInstance":
            return {"id": "synthetic-instance", "apiVersion": "0.1.0"}
        if operation == "getCurrentSession":
            return {"id": "synthetic-reader", "scopes": ["read"]}
        if operation == "listAuthors":
            return {"items": self.authors}
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
                self.assertNotIn("plan.commit", names)
                self.assertNotIn("upload.prepare", names)
                self.assertNotIn("createCredential", names)
                query = await client.call_tool("author.query", {})
                self.assertFalse(query.is_error)
                self.assertEqual(query.structured_content["items"][0]["id"], "synthetic-author")
                denied = await client.call_tool("plan.commit", {})
                self.assertTrue(denied.is_error)
                self.assertEqual(denied.structured_content["code"], "TOOL_UNKNOWN")
                self.assertNotIn("commitPlan", transport.calls)

    async def test_result_pages_at_minimum_budget_reconstruct_the_original_public_text(self):
        budget = 2048
        long_key = "长" * 700
        redaction_prefix = "Z" * 1014
        author = {
            "ascii": "a" * 3075,
            "中文😀": "中文😀🙂" * 800,
            "escaped~/": ('"\\\n\t\x01' * 900),
            "redacted": redaction_prefix + "Bearer synthetic-private",
            "nested": {long_key: "value"},
        }
        expected = {**author, "redacted": redaction_prefix + "Bearer [credential]"}
        with tempfile.TemporaryDirectory() as directory:
            config = Config.load({"QIMENG_API_BASE": "http://127.0.0.1:18481", "QIMENG_API_TOKEN": "synthetic-only",
                                  "QIMENG_AI_STATE_DIR": directory, "QIMENG_AI_OUTPUT_BYTES": str(budget)})
            runtime = Runtime(config, contract=load_contract(), transport=SyntheticTransport([author]))
            async with Client(create_server(runtime), raise_exceptions=True) as client:
                query = await client.call_tool("author.query", {})
                self.assertFalse(query.is_error)
                summary = query.structured_content
                self.assertTrue(summary["truncated"])
                self.assertEqual(summary["totalBytes"], len(encode({"items": [expected]}).encode()))
                reference = summary["resultRef"]
                reads = 0

                async def read(pointer, **arguments):
                    nonlocal reads
                    reads += 1
                    return await client.call_tool("platform.result.read", {
                        "resultRef": reference, "pointer": pointer, **arguments})

                for field, text in expected.items():
                    if not isinstance(text, str):
                        continue
                    with self.subTest(field=field):
                        pointer = "/items/0/" + field.replace("~", "~0").replace("/", "~1")
                        offset, chunks = 0, []
                        for _ in range(30):
                            result = await read(pointer, offset=offset)
                            self.assertFalse(result.is_error, result.structured_content)
                            page = result.structured_content
                            shown = result.content[0].text
                            self.assertLessEqual(len(shown.encode()), budget)
                            self.assertEqual(json.loads(shown), page)
                            self.assertEqual((page["resultRef"], page["pointer"], page["totalItems"]),
                                             (reference, pointer, len(text)))
                            self.assertNotIn("truncated", page)
                            self.assertTrue(page["items"])
                            chunks.append(page["items"])
                            offset += len(chunks[-1])
                            if offset == len(text):
                                self.assertIsNone(page["nextOffset"])
                                break
                            self.assertEqual(page["nextOffset"], offset)
                        self.assertGreater(len(chunks), 1)
                        self.assertEqual("".join(chunks), text)
                        first_again = await read(pointer)
                        self.assertEqual(first_again.structured_content["items"], chunks[0])

                for pointer, code in (("/items", "FIELD_TOO_LARGE"), ("/missing", "POINTER_INVALID"),
                                      ("/items/0/nested/" + long_key, "OUTPUT_BUDGET_TOO_SMALL")):
                    error = await read(pointer)
                    self.assertTrue(error.is_error)
                    self.assertEqual(error.structured_content["code"], code)
                status = await client.call_tool("platform.budget", {})
                self.assertEqual(status.structured_content["calls"], reads + 1)
