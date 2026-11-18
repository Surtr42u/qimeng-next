"""A real stdio client, a fresh CLI process and the cloud Go binary share one frame receipt."""
import asyncio
from email import policy
from email.parser import BytesParser
import hashlib
import hmac
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest

from mcp import Client

from live_fixture import CloudGoFixture


class LensFixture(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        pass

    def do_POST(self):
        self.server.attempts += 1
        size = int(self.headers.get("Content-Length", 0))
        authorized = hmac.compare_digest(self.headers.get("Authorization", ""),
                                         "Bearer " + self.server.secret)
        if not authorized or self.path != "/lens" or not 0 < size <= (2 << 20) + 4096:
            self.send_error(400)
            return
        content_type = self.headers.get("Content-Type", "")
        message = BytesParser(policy=policy.default).parsebytes(
            f"Content-Type: {content_type}\r\n\r\n".encode() + self.rfile.read(size))
        parts = list(message.iter_parts())
        if len(parts) != 1:
            self.send_error(400)
            return
        part = parts[0]
        frame = part.get_payload(decode=True)
        if (part.get_param("name", header="Content-Disposition") != "file" or
                part.get_filename() != "frame.jpg" or part.get_content_type() != "image/jpeg" or
                not frame.startswith(b"\xff\xd8") or not frame.endswith(b"\xff\xd9")):
            self.send_error(400)
            return
        digest = hashlib.sha256(frame).hexdigest()
        self.server.frames.append((digest, len(frame)))
        self.server.received.set()
        if self.server.hold and not self.server.release.wait(60):
            self.send_error(503)
            return
        raw = json.dumps({
            "ok": True, "status": "ok", "frame_sha256": digest,
            "links": [{"url": "https://example.org/work/42?tracking=fixture", "title": "Synthetic work"},
                      {"url": "http://127.0.0.1/private-link", "title": "Discard"}],
            "cites": [], "topTitles": [{"domain": "example.net", "title": "Unlinked hint"}],
            "topDomains": [], "fixtureRawText": "unfiltered-sidecar-marker",
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)


@unittest.skipUnless(os.environ.get("QIMENG_TEST_BASE") == "http://127.0.0.1:18481",
                     "requires the isolated cloud Go binary")
class ImageLookupStdio(CloudGoFixture, unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.assertEqual(os.environ.get("CI"), "true")
        self.assertEqual(os.environ.get("QIMENG_RESEARCH_LENS_ENDPOINT"), "http://127.0.0.1:18483")
        self.sidecar = ThreadingHTTPServer(("127.0.0.1", 18483), LensFixture)
        self.sidecar.secret = os.environ["QIMENG_RESEARCH_SIDECAR_TOKEN"]
        self.sidecar.attempts, self.sidecar.frames = 0, []
        self.sidecar.received, self.sidecar.release = threading.Event(), threading.Event()
        self.sidecar.hold = False
        thread = threading.Thread(target=self.sidecar.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(thread.join)
        self.addCleanup(self.sidecar.server_close)
        self.addCleanup(self.sidecar.shutdown)
        self.addCleanup(self.sidecar.release.set)

        self.root = Path(self.enterContext(tempfile.TemporaryDirectory()))
        library = self.request("POST", "/libraries", {"name": "Frame evidence", "kind": "regular"},
                               key="stdio-frame-library")
        target = Path(os.environ["QIMENG_DATA_DIR"]) / "libraries" / library["id"] / "private-original.mp4"
        generated = target.with_suffix(".part")
        subprocess.run(["ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=s=96x64:r=8",
                        "-t", "2", "-c:v", "mpeg4", "-threads", "1", "-pix_fmt", "yuv420p",
                        "-f", "mp4", str(generated)],
                       check=True, timeout=30)
        self.source_sha = hashlib.sha256(generated.read_bytes()).hexdigest()
        generated.replace(target)
        scan = self.request("POST", "/libraries/" + library["id"] + "/scans",
                            {"expectedRevision": library["revision"]}, key="stdio-frame-scan")
        for _ in range(100):
            scan = self.request("GET", "/scans/" + scan["id"])
            if scan["state"] not in {"queued", "running"}:
                break
            await asyncio.sleep(0.1)
        self.assertEqual(scan["state"], "completed")
        self.asset, = self.request("GET", "/assets?libraryId=" + library["id"])["items"]
        self.task_body = {"kind": "asset", "label": "Inspect synthetic frame", "assetId": self.asset["id"],
                          "expectedContentVersion": self.asset["contentVersion"], "expectedRevision": self.asset["revision"]}

    def executor(self, *, allow_lookup, allow_preview):
        grant = self.request("POST", "/ai/delegations", {
            "label": "Frame permission", "libraryIds": [self.asset["libraryId"]], "authorIds": [],
            "allAuthors": False, "allowedFields": [], "candidateBlocks": [], "allowClear": False,
            "canIngest": False, "canOrganize": False, "canResearch": True, "canResearchAssets": True,
            "canLookupImages": allow_lookup, "maxResearchCalls": 1, "maxUpdates": 1,
            "maxTasksPerDay": 2, "validDays": 1})
        self.addCleanup(self.request, "DELETE", "/ai/delegations/" + grant["delegation"]["id"])
        return self.process(self.root, grant["credential"]["token"], "read,delegate,research",
                            {"QIMENG_AI_ALLOW_MEDIA_TO_MODEL": str(allow_preview).lower()})

    async def call(self, client, name, arguments):
        result = await client.call_tool(name, arguments)
        self.assertFalse(result.is_error, result.content)
        self.assertTrue(all(part.type == "text" for part in result.content))
        return result.structured_content

    async def denied(self, client, name, arguments, code):
        result = await client.call_tool(name, arguments)
        self.assertTrue(result.is_error)
        self.assertEqual(result.structured_content["code"], code)

    async def materials(self, client, key, **options):
        task = await self.call(client, "task.start", {"operationId": key, "body": {**self.task_body, **options}})
        self.assertNotIn("credential", task)
        task_id = task["taskId"]
        material = await self.call(client, "asset.research.materials", {"taskId": task_id, "id": task_id})
        self.assertEqual(material["sourceSha256"], self.source_sha)
        frame = material["artifacts"][len(material["artifacts"]) // 2]
        self.assertEqual(frame["sourceSha256"], self.source_sha)
        self.assertNotEqual(frame["sha256"], self.source_sha)
        args = {"taskId": task_id, "Idempotency-Key": "stdio-frame-search",
                "body": {"frameId": frame["id"], "frameSha256": frame["sha256"],
                         "assetId": material["assetId"], "contentVersion": material["contentVersion"]}}
        return frame, args

    async def cli(self, params, argv, payload=None, *, expected=0):
        run = await asyncio.to_thread(subprocess.run, [sys.executable, "-m", "qimeng_ai.cli", *argv],
                                      input=json.dumps(payload), text=True, capture_output=True, timeout=30,
                                      env={"PATH": os.environ["PATH"], **params.env})
        self.assertEqual(run.returncode, expected, run.stderr)
        return json.loads(run.stderr if expected else run.stdout)

    async def test_single_frame_uses_separate_permissions_and_one_durable_receipt(self):
        preview_only = self.executor(allow_lookup=False, allow_preview=True)
        async with Client(preview_only) as client:
            await self.denied(client, "task.start", {"operationId": "frame-no-parent-permission",
                              "body": {**self.task_body, "allowImageLookup": True}}, "image_lookup_denied")
            frame, args = await self.materials(client, "frame-preview-only")
            picture = await client.call_tool("platform.artifact.read",
                                             {"taskId": args["taskId"], "artifactRef": frame["artifactRef"]})
            self.assertFalse(picture.is_error)
            self.assertTrue(any(part.type == "image" for part in picture.content))
            await self.denied(client, "research.image.search", args, "research_provider_denied")
        self.assertEqual(self.sidecar.attempts, 0)

        params = self.executor(allow_lookup=True, allow_preview=False)
        async with Client(params) as client:
            tools = {tool.name: tool for tool in (await client.list_tools()).tools}
            self.assertTrue(tools["research.image.search"].annotations.open_world_hint)
            _, opted_out = await self.materials(client, "frame-no-task-permission")
            await self.denied(client, "research.image.search", opted_out, "research_provider_denied")
            frame, args = await self.materials(client, "frame-task-allowed", allowImageLookup=True)
            task = {"taskId": args["taskId"]}
            context = await self.call(client, "research.context", task)
            self.assertTrue(context["imageLookupEnabled"] and context["imageLookupAllowed"])
            self.assertEqual(context["remainingResearchCalls"], 1)
            await self.denied(client, "platform.artifact.read", {**task, "artifactRef": frame["artifactRef"]},
                              "MEDIA_TRANSFER_NOT_AUTHORIZED")
            budget = (await self.call(client, "platform.budget", task))["server"]["calls"]
            await self.denied(client, "research.image.search", {**args, "body": {**args["body"], "frameSha256": "0" * 64}},
                              "research_frame_mismatch")
            await self.denied(client, "research.image.search", {**args, "body": {**args["body"], "contentVersion": frame["contentVersion"] + 1}},
                              "research_subject_mismatch")
            self.assertEqual((await self.call(client, "platform.budget", task))["server"]["calls"], budget)
            self.assertEqual(self.sidecar.attempts, 0)

            self.sidecar.hold = True
            first = asyncio.create_task(self.call(client, "research.image.search", args))
            try:
                self.assertTrue(await asyncio.to_thread(self.sidecar.received.wait, 15))
                pending = await self.cli(params, ["call", "research.image.search"], args)
                self.assertEqual(pending["state"], "pending")
                admin = ["admin", "--task-id", task["taskId"]]
                local = await self.cli(params, [*admin, "status"])
                self.assertEqual([row["key"] for row in local["pending"]], [args["Idempotency-Key"]])
                refused = await self.cli(params, [*admin, "reconcile", args["Idempotency-Key"]], expected=2)
                self.assertEqual(refused["code"], "research_query_pending")
            finally:
                self.sidecar.release.set()
                result = await first
            self.assertEqual(result["id"], pending["id"])
            self.assertEqual((result["subjectId"], result["subjectHash"], result["contentVersion"]),
                             (self.asset["id"], self.source_sha, self.asset["contentVersion"]))
            self.assertEqual((result["frameId"], result["frameSha256"]), (frame["id"], frame["sha256"]))
            self.assertEqual((result["provider"], result["operation"], result["state"], result["evidenceKind"]),
                             ("lens", "image_lookup", "ok", "lead"))
            self.assertEqual(result["items"], [{"url": "https://example.org/work/42", "title": "Synthetic work"}])
            self.assertEqual(result["imageHints"], [{"domain": "example.net", "title": "Unlinked hint"}])
            rendered = json.dumps(result)
            self.assertNotIn("unfiltered-sidecar-marker", rendered)
            self.assertFalse(self.sidecar.secret in rendered or params.env["QIMENG_API_TOKEN"] in rendered,
                             "A credential appeared in the model-visible receipt")
            self.assertEqual(await self.cli(params, ["call", "research.image.search"], args), result)
            self.assertEqual((await self.call(client, "platform.budget", task))["server"]["calls"], budget + 1)
            receipt = await self.call(client, "operation.read", {**task, "key": args["Idempotency-Key"],
                                                                  "operation": "lookupResearchImage"})
            self.assertEqual((receipt["state"], receipt["operation"], receipt["response"]),
                             ("committed", "lookupResearchImage", result))
            self.assertEqual(await self.call(client, "research.provider.result", {**task, "id": result["id"]}), result)
            after = await self.call(client, "research.context", task)
            self.assertEqual(after["remainingResearchCalls"], 0)
            self.assertEqual([entry["id"] for entry in after["queries"]], [result["id"]])
            await self.denied(client, "research.image.search", {**args, "Idempotency-Key": "frame-out-of-budget"},
                              "research_budget_exhausted")
        self.assertEqual(self.sidecar.frames, [(frame["sha256"], frame["size"])])
        self.assertEqual(self.sidecar.attempts, 1)
        self.assertEqual(self.request("GET", "/assets/" + self.asset["id"])["revision"], self.asset["revision"])
