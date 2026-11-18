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
    async def test_scanned_file_research_and_metadata_use_the_asset_grant(self):
        self.assertEqual(os.environ.get("CI"), "true")
        import subprocess
        library = self.request("POST", "/libraries", {"name": "Existing original", "kind": "regular"}, key="mcp-scan-library")
        original = Path(os.environ["QIMENG_DATA_DIR"]) / "libraries" / library["id"] / "archive-source.png"
        subprocess.run(["ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=cyan:s=18x20", "-frames:v", "1", str(original)], check=True)
        scan = self.request("POST", "/libraries/" + library["id"] + "/scans", {"expectedRevision": library["revision"]}, key="mcp-scan-existing")
        for _ in range(100):
            scan = self.request("GET", "/scans/" + scan["id"])
            if scan["state"] not in {"queued", "running"}:
                break
            await asyncio.sleep(0.1)
        self.assertEqual(scan["state"], "completed")
        asset, = self.request("GET", "/assets?libraryId=" + library["id"])["items"]
        grant = self.request("POST", "/ai/delegations", {
            "label":"Existing work task","libraryIds":[library["id"]],"authorIds":[],"allAuthors":False,
            "allowedFields":[],"candidateBlocks":[],"canIngest":False,"canOrganize":False,"canResearch":True,
            "canResearchAssets":True,"assetFields":["tags"],"maxResearchCalls":2,"maxUpdates":1,"maxTasksPerDay":2,"validDays":1})
        with tempfile.TemporaryDirectory() as directory:
            async with Client(self.process(Path(directory), grant["credential"]["token"], "read,delegate,research,asset.metadata.write")) as client:
                async def call(name, args):
                    r = await client.call_tool(name,args)
                    self.assertFalse(r.is_error,r.content)
                    return r
                start = await call("task.start",{"operationId":"scanned-asset-start","body":{
                    "kind":"asset","label":"Read the existing work","assetId":asset["id"],"expectedRevision":asset["revision"],
                    "expectedContentVersion":asset["contentVersion"],"assetFields":["tags"]}})
                task_id = start.structured_content["taskId"]
                target = {"taskId":task_id,"id":task_id}
                material = (await call("asset.research.materials",target)).structured_content
                self.assertEqual(material["assetId"],asset["id"])
                self.assertNotIn("uploadId",material)
                picture = await call("platform.artifact.read",{"taskId":task_id,"artifactRef":material["artifacts"][0]["artifactRef"]})
                self.assertTrue(any(part.type=="image" for part in picture.content))
                update = {**target,"Idempotency-Key":"scanned-asset-tags","body":{"expectedRevision":asset["revision"],"tags":["fixture"],
                          "evidence":[{"field":"tags","url":"https://example.org/fixture","observation":"Synthetic sample verified."}]}}
                await call("asset.metadata.apply",update)
                await call("asset.metadata.apply",update)
                records = (await call("asset.evidence.query",target)).structured_content
                self.assertEqual(len(records["items"]),1)
                self.assertEqual(self.request("GET","/assets/"+asset["id"])["tags"],["fixture"])
        self.request("DELETE","/ai/delegations/"+grant["delegation"]["id"])

    async def test_persistent_permission_creates_and_resumes_task_across_hosts(self):
        if os.environ.get("CI") != "true":
            self.fail("live tests must run in cloud")
        library = self.request("POST", "/libraries", {"name": "Persistent MCP synthetic", "kind": "regular"}, key="mcp-persistent-library")
        grant = self.request("POST", "/ai/delegations", {
            "label": "Synthetic cross-host executor", "libraryIds": [library["id"]], "authorIds": [],
            "allAuthors": False, "allowedFields": [], "candidateBlocks": [], "allowClear": False,
            "canIngest": True, "canResearch": True, "canOrganize": True,
            "maxResearchCalls": 2, "maxUpdates": 1, "maxTasksPerDay": 2, "validDays": 1,
            "maxCallsPerTask": 10, "taskSeconds": 3600,
        })
        with tempfile.TemporaryDirectory() as first, tempfile.TemporaryDirectory() as second:
            token = grant["credential"]["token"]
            async with Client(self.process(Path(first), token, "read,delegate,ingest,research,organize")) as client:
                result = await client.call_tool("task.start", {"operationId": "mcp-persistent-start", "body": {
                    "kind": "intake", "label": "Synthetic task", "libraryId": library["id"], "mode": "intelligent"}})
                self.assertFalse(result.is_error, result.content)
                task = result.structured_content
                self.assertNotIn("credential", task)
                self.assertNotIn(token, str(result.content))
                original = await client.call_tool("task.context", {"taskId": task["taskId"], "id": task["taskId"]})
                self.assertFalse(original.is_error, original.content)
                budget = await client.call_tool("platform.budget", {"taskId": task["taskId"]})
                self.assertEqual(budget.structured_content["server"]["calls"], 1)
                async with Client(self.process(Path(second), token, "read,delegate,ingest,research,organize")) as other:
                    result = await other.call_tool("task.resume", {"operationId": "mcp-persistent-resume", "id": task["taskId"]})
                    self.assertFalse(result.is_error, result.content)
                    resumed = await other.call_tool("platform.budget", {"taskId": task["taskId"]})
                    self.assertEqual(resumed.structured_content["server"]["calls"], 1)
                    stale = await client.call_tool("task.context", {"taskId": task["taskId"], "id": task["taskId"]})
                    self.assertTrue(stale.is_error)
                    context = await other.call_tool("task.context", {"taskId": task["taskId"], "id": task["taskId"]})
                    self.assertFalse(context.is_error, context.content)
                    self.assertEqual(context.structured_content["batch"]["id"], task["taskId"])
        self.request("DELETE", "/ai/delegations/" + grant["delegation"]["id"])

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

    def process(self, root, token, permissions, extra_environment=None):
        source = root / "input"
        source.mkdir(exist_ok=True)
        return StdioServerParameters(command=sys.executable, args=["-m", "qimeng_ai.cli", "mcp"], env={
            "PYTHONPATH": str(Path(__file__).resolve().parents[1] / "src"),
            "QIMENG_API_BASE": os.environ["QIMENG_TEST_BASE"], "QIMENG_API_TOKEN": token,
            "QIMENG_AI_STATE_DIR": str(root / "state"), "QIMENG_AI_TASK_ID": "cloud-live",
            "QIMENG_AI_STORAGES": json.dumps({"input": str(source)}),
            "QIMENG_AI_PERMISSIONS": permissions, "QIMENG_AI_MAX_SECONDS": "900",
            "QIMENG_AI_ALLOW_MEDIA_TO_MODEL": "true",
            **(extra_environment or {}),
        })

    async def test_protected_original_goes_to_the_native_trash_only_once(self):
        self.assertEqual(os.environ.get("CI"), "true")
        import subprocess
        library = self.request("POST", "/libraries", {"name": "Protected synthetic input", "kind": "regular"}, key="mcp-protection-library")
        batch = self.request("POST", "/batches", {"libraryId": library["id"], "mode": "ordinary", "label": "Source disposition"}, key="mcp-protection-batch")
        credential = self.request("POST", "/credentials", {"label": "Source cleanup fixture", "batchId": batch["id"],
                                                            "scopes": ["read", "ingest", "source.cleanup"]})
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "input").mkdir()
            source = root / "input/chosen.png"
            subprocess.run(["ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=0x476842:s=44x28",
                            "-frames:v", "1", str(source)], check=True)
            expected = source.read_bytes()
            env = {"QIMENG_AI_RECYCLE_STORAGES": '["input"]', "XDG_DATA_HOME": str(root/"xdg")}
            async with Client(self.process(root, credential["token"], "read,ingest,local.read,local.recycle,source.cleanup", env)) as client:
                async def call(name, args):
                    response = await client.call_tool(name, args)
                    self.assertFalse(response.is_error, response.content)
                    return response.structured_content
                listed = {item.name for item in (await client.list_tools()).tools}
                self.assertFalse({"issueSourceCleanupPermit", "recordSourceCleanupReceipt"} & listed)
                sealed = await call("intake.register", {"batchId": batch["id"], "storage":"input", "operationId":"protect-manifest"})
                item = sealed["inputs"][0]
                ingested = await call("upload.file", {"batchId":batch["id"], "libraryId":library["id"], "inputId":item["inputId"],
                                      "fileRef":item["fileRef"], "operationId":"protect-upload", "mode":"ordinary", "fileName":"chosen.png"})
                self.assertEqual(ingested["status"], "ingested")
                protection = await call("backup.protect", {"id":batch["id"], "Idempotency-Key":"protect-batch-input"})
                for _ in range(60):
                    protection = self.request("GET", "/batches/"+batch["id"]+"/protections/"+protection["id"])
                    if protection["state"] not in {"queued","running"}:
                        break
                    await asyncio.sleep(.25)
                self.assertEqual(protection["state"], "completed", protection.get("errorCode"))
                self.assertTrue(source.is_file())
                args = {"batchId":batch["id"], "protectionId":protection["id"], "inputId":item["inputId"],
                        "fileRef":item["fileRef"], "operationId":"native-recycle-protected"}
                self.assertEqual((await call("source.recycle", args))["outcome"], "recycled")
                source.write_bytes(b"new synthetic file with the old name")
                self.assertEqual((await call("source.recycle", args))["outcome"], "recycled")
                self.assertEqual(source.read_bytes(), b"new synthetic file with the old name")
                trashed, = (root/"xdg/Trash/files").iterdir()
                self.assertEqual(trashed.read_bytes(), expected)
                result = await call("task.read", {"id":batch["id"]})
                self.assertEqual(result["batch"]["cleanupState"], "recycled")

    async def test_stdio_intake_and_author_maintenance(self):
        if os.environ.get("CI") != "true":
            self.fail("live tests must run in cloud")
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "input").mkdir()
            # Complete, generated image fixture; no real user file is ever read.
            import subprocess
            subprocess.run(["ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=orange:s=64x48", "-frames:v", "1", str(root / "input/sample.png")], check=True)
            library = self.request("POST", "/libraries", {"name": "MCP synthetic", "kind": "regular"}, key="mcp-live-library")
            batch = self.request("POST", "/batches", {"libraryId": library["id"], "mode": "intelligent", "label": "MCP fixture"}, key="mcp-live-batch")
            credential = self.request("POST", "/credentials", {"label": "MCP fixture", "batchId": batch["id"], "scopes": ["read", "ingest", "research", "organize"]})
            try:
                async with Client(self.process(root, credential["token"], "read,ingest,research,organize,local.read,local.research")) as client:
                    names = {tool.name for tool in (await client.list_tools()).tools}
                    self.assertIn("upload.file", names)
                    self.assertIn("plan.commit", names)
                    async def call(tool, arguments):
                        result = await client.call_tool(tool, arguments)
                        self.assertFalse(result.is_error, result.content)
                        return result.structured_content
                    listing = await call("files.search", {"storage": "input", "query": "sample.png"})
                    probe = await call("research.probe", {"fileRef": listing["items"][0]["fileRef"], "frames": 1})
                    self.assertEqual(probe["businessWrites"], 0)
                    reference = probe["frameRefs"][0]["artifactRef"]
                    picture = await client.call_tool("platform.artifact.read", {"artifactRef": reference})
                    self.assertFalse(picture.is_error, picture.content)
                    self.assertTrue(any(block.type == "image" for block in picture.content))
                    bypass = await client.call_tool("platform.result.read", {"resultRef": "r_" + reference[2:]})
                    self.assertTrue(bypass.is_error)
                    registered = await call("intake.register", {"batchId": batch["id"], "operationId": "mcp-live-manifest", "storage": "input"})
                    self.assertEqual(registered["inputCount"], 1)
                    uploaded = await call("upload.file", {"fileRef": listing["items"][0]["fileRef"], "operationId": "mcp-live-upload",
                                          "libraryId": library["id"], "batchId": batch["id"], "inputId": registered["inputs"][0]["inputId"], "fileName": "sample.png", "mode": "intelligent"})
                    self.assertEqual(uploaded["status"], "staged")
                    materials = await call("research.materials", {"id": uploaded["upload"]["id"], "expectedHash": uploaded["sha256"]})
                    self.assertEqual(materials["businessWrites"], 0)
                    # A second executor has no first executor's local cache or local-read permission.
                    with tempfile.TemporaryDirectory() as remote_directory:
                        async with Client(self.process(Path(remote_directory), credential["token"], "read,research")) as remote:
                            remote_picture = await remote.call_tool("platform.artifact.read", {"artifactRef": materials["artifacts"][0]["artifactRef"]})
                            self.assertFalse(remote_picture.is_error, remote_picture.content)
                            self.assertTrue(any(block.type == "image" and block.mime_type == "image/jpeg" for block in remote_picture.content))
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
