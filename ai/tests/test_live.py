"""云端同提交Go服务+真正stdio MCP子进程，只有显式隔离地址才执行。"""
import asyncio
import base64
import json
import os
from pathlib import Path
import tempfile
import unittest

from mcp import Client

from live_fixture import CloudGoFixture


@unittest.skipUnless(os.environ.get("QIMENG_TEST_BASE") == "http://127.0.0.1:18481", "requires cloud isolated Go service")
class LiveMCP(CloudGoFixture, unittest.IsolatedAsyncioTestCase):
    async def test_direct_delegated_secret_reads_and_spends_its_actual_server_budget(self):
        import secrets
        import urllib.request

        library = self.request("POST", "/libraries", {"name": "Direct child budget", "kind": "regular"},
                               key="direct-child-library")
        grant = self.request("POST", "/ai/delegations", {"label": "Direct child", "libraryIds": [library["id"]],
                             "authorIds": [], "allAuthors": False, "allowedFields": [], "candidateBlocks": [],
                             "allowClear": False, "canIngest": True, "canResearch": False, "canOrganize": False,
                             "maxResearchCalls": 0, "maxUpdates": 1, "maxTasksPerDay": 1, "validDays": 1,
                             "maxCallsPerTask": 10})
        self.addCleanup(self.request, "DELETE", "/ai/delegations/" + grant["delegation"]["id"])
        child = secrets.token_urlsafe(32)
        request = urllib.request.Request(os.environ["QIMENG_TEST_BASE"] + "/api/v1/ai/delegated-tasks",
                                         data=json.dumps({"kind": "intake", "mode": "ordinary", "label": "Direct task",
                                                          "libraryId": library["id"], "credentialSecret": child}).encode(),
                                         headers={"Content-Type": "application/json", "Idempotency-Key": "direct-child-start",
                                                  "Authorization": "Bearer " + grant["credential"]["token"]})
        with urllib.request.urlopen(request, timeout=10) as response:
            task = json.load(response)["taskId"]
        with tempfile.TemporaryDirectory() as directory:
            async with Client(self.process(Path(directory), child, "read")) as client:
                async def budget():
                    result = await client.call_tool("platform.budget", {})
                    self.assertFalse(result.is_error, result.content)
                    self.assertEqual(result.structured_content["local"]["taskId"], task)
                    return result.structured_content["server"]

                self.assertEqual((await budget())["calls"], 0)
                for n in range(1, 11):
                    read = await client.call_tool("library.query", {"limit": n})
                    self.assertFalse(read.is_error, read.content)
                    if n == 1:
                        self.assertEqual((await budget())["calls"], 1)
                denied = await client.call_tool("library.query", {"limit": 11})
                self.assertTrue(denied.is_error)
                self.assertEqual(denied.structured_content["code"], "task_budget_exhausted")
                self.assertEqual((await budget())["calls"], 10)

    async def test_direct_author_session_resolves_the_granted_target_and_replays_its_original_baseline(self):
        author = self.request("POST", "/authors", {"kind": "regular", "name": "Single target", "biography": "Before"},
                              key="bound-author-target")
        other = self.request("POST", "/authors", {"kind": "regular", "name": "Another target", "biography": "Unrelated"},
                             key="bound-author-other")

        def grant(key, revision):
            task = self.request("POST", "/author-tasks", {"authorId": author["id"], "expectedRevision": revision,
                                "allowedFields": ["biography"], "allowClear": False, "maxUpdates": 2}, key=key)
            credential = self.request("POST", "/credentials", {"label": "Single author", "authorTaskId": task["id"],
                                      "scopes": ["read", "author.profile.write"]})
            self.addCleanup(self.request, "DELETE", "/credentials/" + credential["id"])
            return task, credential["token"]

        async def get(client, name, args):
            result = await client.call_tool(name, args)
            self.assertFalse(result.is_error, result.content)
            return result.structured_content

        task, token = grant("bound-author-first", author["revision"])
        args = {"Idempotency-Key": "bound-author-write", "body": {"biography": "Reviewed text"}}
        with tempfile.TemporaryDirectory() as directory:
            process = self.process(Path(directory), token, "read,author.profile.write")
            async with Client(process) as first:
                tools = {t.name: t for t in (await first.list_tools()).tools}
                self.assertNotIn("taskId", tools["tag.vocabulary.query"].input_schema["properties"])
                shown = tools["author.profile.update"].input_schema
                self.assertEqual(set(shown["properties"]), {"Idempotency-Key", "body"})
                self.assertEqual(set(shown["properties"]["body"]["properties"]), {"biography"})
                description = await get(first, "platform.tool.describe", {"tool": "author.profile.update"})
                self.assertEqual(description["inputSchema"], shown)
                self.assertEqual((await get(first, "author.task.context", {}))["task"]["id"], task["id"])
                self.assertEqual((await get(first, "author.read", {}))["id"], author["id"])
                for name, original in (("author.read", {"id": other["id"]}),
                                       ("author.profile.update", {**args, "id": other["id"]}),
                                       ("author.task.context", {"id": "another-task"})):
                    denied = await first.call_tool(name, original)
                    self.assertTrue(denied.is_error)
                    self.assertEqual(denied.structured_content["code"], "task_scope_denied")
                    self.assertNotIn("Unrelated", str(denied.content))
                clear = await first.call_tool("author.profile.update", {**args, "Idempotency-Key": "bound-author-clear",
                                                                        "body": {"biography": ""}})
                self.assertTrue(clear.is_error)
                self.assertEqual(clear.structured_content["code"], "clear_not_authorized")
                changed = await get(first, "author.profile.update", args)
                self.assertEqual(changed["readback"], {"state": "verified", "author": {
                    "id": author["id"], "revision": changed["writeReceipt"]["revision"], "biography": "Reviewed text"}})
            async with Client(process) as restarted:
                self.assertEqual(await get(restarted, "author.profile.update", args), changed)
                legacy = {"taskId": task["id"], "id": author["id"], "Idempotency-Key": args["Idempotency-Key"],
                          "body": {"expectedRevision": author["revision"], "biography": "Reviewed text"}}
                self.assertEqual(await get(restarted, "author.profile.update", legacy), changed["writeReceipt"])
                altered = await restarted.call_tool("author.profile.update", {**args, "body": {"biography": "Other text"}})
                self.assertTrue(altered.is_error)
                self.assertEqual(altered.structured_content["code"], "idempotency_conflict")
                self.assertEqual((await get(restarted, "author.read", {}))["biography"], "Reviewed text")
        actual = self.request("GET", "/author-tasks/" + task["id"])
        self.assertEqual((actual["task"]["usedUpdates"], actual["author"]["revision"]), (1, author["revision"] + 1))

        stale, token = grant("bound-author-second", changed["writeReceipt"]["revision"])
        human = self.request("PATCH", "/authors/" + author["id"], {"expectedRevision": changed["writeReceipt"]["revision"],
                             "biography": "Human edit"}, key="bound-author-human")
        with tempfile.TemporaryDirectory() as directory:
            async with Client(self.process(Path(directory), token, "read,author.profile.write")) as client:
                denied = await client.call_tool("author.profile.update", {**args, "Idempotency-Key": "bound-author-stale"})
                self.assertTrue(denied.is_error)
                self.assertEqual(denied.structured_content["code"], "revision_conflict")
                after = await get(client, "author.task.context", {})
                self.assertEqual((after["task"]["usedUpdates"], after["task"]["expectedRevision"]), (0, stale["expectedRevision"]))
                self.assertEqual((after["author"]["biography"], after["author"]["revision"]), ("Human edit", human["revision"]))
        self.assertEqual(self.request("GET", "/authors/" + other["id"])["biography"], "Unrelated")

    async def test_committed_author_write_distinguishes_a_lost_read_and_a_later_human_edit(self):
        from qimeng_ai.config import Config
        from qimeng_ai.errors import ToolError
        from qimeng_ai.runtime import Runtime
        from qimeng_ai.transport import Transport, load_contract

        author = self.request("POST", "/authors", {"kind": "regular", "name": "Readback check"}, key="readback-author")
        task = self.request("POST", "/author-tasks", {"authorId": author["id"], "expectedRevision": author["revision"],
                            "allowedFields": ["biography"], "allowClear": False, "maxUpdates": 1}, key="readback-task")
        issued = self.request("POST", "/credentials", {"label": "Readback check", "authorTaskId": task["id"],
                              "scopes": ["read", "author.profile.write"]})
        self.addCleanup(self.request, "DELETE", "/credentials/" + issued["id"])
        results = []
        owner_request = self.request

        class InterleaveRead(Transport):
            def call(self, operation_id, arguments, **kwargs):
                if operation_id == "getAuthor" and len(results) == 1:
                    owner_request("PATCH", "/authors/" + author["id"], {"expectedRevision": author["revision"] + 1,
                                  "biography": "Updated by the owner"}, key="readback-owner-edit")
                actual = super().call(operation_id, arguments, **kwargs)
                if operation_id == "getAuthor":
                    results.append(actual)
                    if len(results) == 1:
                        raise ToolError("NETWORK_ERROR", "Read response was lost")
                return actual

        with tempfile.TemporaryDirectory() as directory:
            cfg = Config.load({"QIMENG_API_BASE": os.environ["QIMENG_TEST_BASE"], "QIMENG_API_TOKEN": issued["token"],
                               "QIMENG_AI_STATE_DIR": directory, "QIMENG_AI_PERMISSIONS": "read,author.profile.write"})
            contract = load_contract()
            runtime = Runtime(cfg, contract=contract, transport=InterleaveRead(cfg, contract))
            self.addCleanup(runtime.close)
            args = {"Idempotency-Key": "readback-original-key", "body": {"biography": "Saved by the task"}}
            lost = runtime.call("author.profile.update", args)
            self.assertEqual((lost["state"], lost["operationKey"], lost["writeReceipt"]["biography"]),
                             ("committed", args["Idempotency-Key"], "Saved by the task"))
            self.assertEqual(lost["readback"], {"state": "unavailable", "code": "NETWORK_ERROR",
                                                "nextAction": {"tool": "author.read", "arguments": {}}})
            self.assertEqual(runtime.state.status()["pending"], [])
            later = runtime.call("author.profile.update", args)
            self.assertEqual(later["writeReceipt"], lost["writeReceipt"])
            self.assertEqual((later["readback"]["state"], later["readback"]["author"]["biography"]),
                             ("changed", "Updated by the owner"))
            self.assertEqual(later["readback"]["author"]["revision"], results[1]["revision"])
        final = self.request("GET", "/author-tasks/" + task["id"])
        self.assertEqual((final["task"]["usedUpdates"], final["author"]["revision"]), (1, author["revision"] + 2))

    async def test_read_only_alias_lookup_preserves_each_candidate_identity(self):
        self.assertEqual(os.environ.get("CI"), "true")
        library = self.request("POST", "/libraries", {"name": "Alias lookup", "kind": "regular"}, key="mcp-alias-library")
        batch = self.request("POST", "/batches", {"libraryId": library["id"], "mode": "ordinary", "label": "Lookup fixture"}, key="mcp-alias-batch")
        alias = "合成共用别称"
        tags = {}
        for index, name in enumerate(["合成春卷", "合成秋卷"]):
            tag = self.request("POST", "/tags", {"name": name}, key=f"mcp-alias-tag-{index}")
            edited = self.request("PUT", f"/tag-vocabulary/{tag['id']}/aliases",
                                  {"aliases": [alias], "expectedRevision": tag["revision"]}, key=f"mcp-alias-set-{index}")
            tags[edited["id"]] = edited
        collection = self.request("POST", "/collections", {"name": "合成画册", "kind": "image_set", "libraryId": library["id"]}, key="mcp-alias-collection")
        self.request("PUT", f"/collections/{collection['id']}/aliases", {"aliases": [alias], "expectedRevision": collection["revision"]}, key="mcp-alias-group-set")
        credential = self.request("POST", "/credentials", {"label": "Vocabulary reader", "batchId": batch["id"], "scopes": ["read"]})
        try:
            with tempfile.TemporaryDirectory() as root:
                async with Client(self.process(Path(root), credential["token"], "read")) as client:
                    listing = {tool.name: tool.input_schema for tool in (await client.list_tools()).tools}
                    self.assertNotIn("taskId", listing["tag.vocabulary.query"]["properties"])
                    async def items(tool, **arguments):
                        result = await client.call_tool(tool, arguments)
                        self.assertFalse(result.is_error, result.content)
                        return result.structured_content["items"]

                    found = await items("tag.vocabulary.query", q=alias)
                    self.assertEqual({tag["id"]: tag for tag in found}, tags)
                    self.assertCountEqual(await items("tag.query", q=alias), [tag["name"] for tag in tags.values()])
                    found_groups = await items("collection.query", q=alias, libraryId=library["id"])
                    self.assertEqual([group["id"] for group in found_groups], [collection["id"]])

                    cleared = found[0]
                    self.request("PUT", f"/tag-vocabulary/{cleared['id']}/aliases",
                                 {"aliases": [], "expectedRevision": cleared["revision"]}, key="mcp-alias-clear")
                    remaining = await items("tag.vocabulary.query", q=alias)
                    self.assertEqual({tag["id"] for tag in remaining}, set(tags) - {cleared["id"]})
        finally:
            self.request("DELETE", "/credentials/" + credential["id"])

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
            "allowedFields":[],"candidateBlocks":[],"allowClear":False,"canIngest":False,"canOrganize":False,"canResearch":True,
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
                listing = {tool.name: tool.input_schema for tool in (await client.list_tools()).tools}
                self.assertIn("taskId", listing["tag.vocabulary.query"]["properties"])
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

    async def test_mixed_selection_survives_executor_rotation_and_protects_only_published_media(self):
        import hashlib
        import subprocess
        from urllib.error import HTTPError

        self.assertEqual(os.environ.get("CI"), "true")
        library = self.request("POST", "/libraries", {"name": "Mixed intake fixture", "kind": "regular"}, key="mixed-intake-library")
        grant = self.request("POST", "/ai/delegations", {
            "label": "Mixed selection", "libraryIds": [library["id"]], "authorIds": [], "allAuthors": False,
            "allowedFields": [], "candidateBlocks": [], "allowClear": False, "canIngest": True,
            "canResearch": False, "canOrganize": False, "canRecycleSources": True, "maxResearchCalls": 0,
            "maxUpdates": 1, "maxTasksPerDay": 2, "validDays": 1, "maxCallsPerTask": 100, "taskSeconds": 3600,
        })
        self.addCleanup(self.request, "DELETE", "/ai/delegations/" + grant["delegation"]["id"])
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "originals"
            source.mkdir()
            (source / "empty.png").touch()
            (source / "notes.TXT").write_bytes(b"Synthetic accompanying text")
            for name, options in [("picture.png", ["-frames:v", "1"]), ("video.mp4", ["-t", "0.5", "-c:v", "libx264", "-pix_fmt", "yuv420p"])]:
                subprocess.run(["ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=green:s=48x32", *options,
                                "-threads", "1", str(source / name)], check=True)
            originals = {p.name: p.read_bytes() for p in source.iterdir()}
            env = {"QIMENG_AI_STORAGES": json.dumps({"input": str(source)}), "QIMENG_AI_RECYCLE_STORAGES": '["input"]',
                   "XDG_DATA_HOME": str(root / "xdg")}
            permissions = "read,delegate,ingest,local.read,source.cleanup,local.recycle"
            token = grant["credential"]["token"]
            (root / "first").mkdir()
            (root / "next").mkdir()

            async def call(client, name, **args):
                result = await client.call_tool(name, args)
                self.assertFalse(result.is_error, result.content)
                return result.structured_content

            async with Client(self.process(root / "first", token, permissions, env)) as first:
                started = await call(first, "task.start", operationId="mixed-intake-start", body={
                    "kind": "intake", "libraryId": library["id"], "label": "All four selections", "mode": "ordinary"})
                task = started["taskId"]
                registration = {"taskId": task, "batchId": task, "operationId": "mixed-intake-manifest", "storage": "input"}
                sealed = await call(first, "intake.register", **registration)
                entries = {entry["originalFileName"]: entry for entry in sealed["inputs"]}
                self.assertEqual(set(entries), originals.keys())
                self.assertEqual({k: (v["state"], v.get("errorCode")) for k, v in entries.items()}, {
                    "empty.png": ("failed", "empty_input"), "notes.TXT": ("failed", "unsupported_media"),
                    "picture.png": ("pending", None), "video.mp4": ("pending", None)})

                def transfer(name, entry):
                    return {"taskId": task, "batchId": task, "libraryId": library["id"], "mode": "ordinary",
                            "operationId": "mixed-upload-" + name, "fileName": name,
                            "fileRef": entry["fileRef"], "inputId": entry["inputId"]}

                published = await call(first, "upload.file", **transfer("picture.png", entries["picture.png"]))
                self.assertEqual(published["status"], "ingested")
                async with Client(self.process(root / "next", token, permissions, env)) as other:
                    await call(other, "task.resume", operationId="mixed-intake-resume", id=task)
                    stale = await first.call_tool("task.context", {"taskId": task, "id": task})
                    self.assertTrue(stale.is_error)
                    replay = await call(other, "intake.register", **registration)
                    self.assertEqual(replay["batch"], sealed["batch"])
                    fresh = {entry["originalFileName"]: entry for entry in replay["inputs"]}
                    self.assertEqual({k: v["inputId"] for k, v in fresh.items()}, {k: v["inputId"] for k, v in entries.items()})
                    again = await call(other, "upload.file", **transfer("picture.png", fresh["picture.png"]))
                    self.assertEqual((again["upload"]["id"], again["asset"]["id"]), (published["upload"]["id"], published["asset"]["id"]))
                    video = await call(other, "upload.file", **transfer("video.mp4", fresh["video.mp4"]))
                    self.assertEqual(video["status"], "ingested")
                    report = await call(other, "task.context", taskId=task, id=task)
                    self.assertEqual((report["batch"]["expectedInputs"], report["batch"]["state"], len(report["inputs"]), len(report["items"])), (4, "partial", 4, 2))
                    self.assertNotIn("upload.file", report["nextActions"])
                    assets = await call(other, "asset.query", taskId=task, libraryId=library["id"])
                    self.assertEqual({a["id"]: a["hash"] for a in assets["items"]}, {
                        published["asset"]["id"]: hashlib.sha256(originals["picture.png"]).hexdigest(),
                        video["asset"]["id"]: hashlib.sha256(originals["video.mp4"]).hexdigest()})

                    protection = await call(other, "backup.protect", taskId=task, id=task, **{"Idempotency-Key": "mixed-input-protection"})
                    for _ in range(60):
                        observed = self.request("GET", f"/batches/{task}/protections/{protection['id']}")
                        if observed["state"] not in {"queued", "running"}:
                            break
                        await asyncio.sleep(.25)
                    protection = await call(other, "backup.protection.read", taskId=task, id=task, protectionId=protection["id"])
                    self.assertEqual(protection["state"], "completed")
                    covered = {item["inputId"]: item for item in protection["items"]}
                    self.assertEqual(set(covered), {v["inputId"] for v in entries.values()})
                    for name in ("picture.png", "video.mp4"):
                        self.assertEqual(covered[fresh[name]["inputId"]]["state"], "protected")
                    for name, reason in (("empty.png", "empty_input"), ("notes.TXT", "unsupported_media")):
                        input_id = fresh[name]["inputId"]
                        self.assertEqual((covered[input_id]["state"], covered[input_id]["reason"]), ("retained", reason))
                        self.assertFalse(covered[input_id].get("assetId") or covered[input_id].get("uploadId"))
                        denied = await other.call_tool("source.recycle", {"taskId": task, "batchId": task, "inputId": input_id,
                            "fileRef": fresh[name]["fileRef"], "protectionId": protection["id"], "operationId": "mixed-deny-" + name})
                        self.assertTrue(denied.is_error)
                        self.assertEqual(denied.structured_content["code"], "INPUT_NOT_INGESTED")
                        with self.assertRaises(HTTPError) as denied_permit:
                            self.request("POST", f"/batches/{task}/protections/{protection['id']}/cleanup-permits",
                                {"inputId": input_id, "expectedHash": published["sha256"],
                                 "sourceSize": len(originals["picture.png"])}, key="mixed-permit-" + name)
                        with denied_permit.exception as error:
                            self.assertEqual((error.code, json.load(error)["code"]), (409, "input_not_protected"))
                    self.assertEqual({p.name: p.read_bytes() for p in source.iterdir()}, originals)

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
                    session = await client.call_tool("platform.session", {})
                    self.assertFalse(session.is_error, session.content)
                    self.assertEqual(session.structured_content["authorTaskId"], task["id"])
                    self.assertNotIn("token", session.structured_content)
                    context = await client.call_tool("author.task.context", {"id": session.structured_content["authorTaskId"]})
                    self.assertFalse(context.is_error, context.content)
                    self.assertEqual(context.structured_content["task"]["allowedFields"], ["biography"])
                    denied = await client.call_tool("author.profile.update", {"id": author["id"], "Idempotency-Key": "mcp-ungranted-author-source",
                        "body": {"expectedRevision": author["revision"], "acquisitionSources": [], "biography": "Must not apply"}})
                    self.assertTrue(denied.is_error)
                    self.assertEqual((denied.structured_content["code"], denied.structured_content["details"]["nextTool"]),
                                     ("scope_denied", "platform.session"))
                    self.assertIn("acquisitionSources", denied.structured_content["message"])
                    result = await client.call_tool("author.profile.update", {"id": author["id"], "Idempotency-Key": "mcp-live-clear",
                                                    "body": {"expectedRevision": author["revision"], "biography": ""}})
                    self.assertFalse(result.is_error, result.content)
                    self.assertEqual(result.structured_content["biography"], "")
                    denied = await client.call_tool("plan.commit", {})
                    self.assertTrue(denied.is_error)
            finally:
                self.request("DELETE", "/credentials/" + credential["id"])
