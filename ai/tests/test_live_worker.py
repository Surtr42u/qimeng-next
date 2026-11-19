"""An App submits real files. Only the local model is scripted in this integration."""
import asyncio
from datetime import date
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest.mock import patch
import urllib.request

from mcp import Client

from live_fixture import CloudGoFixture
from qimeng_ai.config import Config
from qimeng_ai.errors import ToolError
from qimeng_ai.runtime import Runtime
from qimeng_ai.state import encode
from qimeng_ai.worker import run_worker


class ChatHost(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        pass

    def do_POST(self):
        if self.path != "/v1/chat/completions" or self.headers.get("Authorization") != "Bearer " + self.server.host_key:
            self.send_error(403)
            return
        request = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        self.server.requests.append(request)
        first = request["messages"][1]["content"]
        report = json.loads(first[first.index("{"):])
        batch_id = self.server.batches[report["request"]]
        behavior = self.server.behavior.get(batch_id)
        if behavior == "offline":
            self.send_error(503)
            return
        calls = []
        if behavior != "wait":
            tools = [json.loads(m["content"]) for m in request["messages"] if m["role"] == "tool"]
            preview = next((r for r in reversed(tools) if r.get("previewComplete")), None)
            if batch_id in self.server.scripts:
                name, args = self.server.scripts[batch_id](tools)
            elif preview is None and report["plans"]:
                name, args = "plan_read", {"plan": report["plans"][0]["plan"]}
            elif preview is None:
                staged = [row for row in report["inputs"] if row.get("canPrepare")]
                name = "plan_prepare"
                args = {"items": [{"input": u["input"], "family": Path(u["name"]).stem} for u in staged]}
            else:
                if behavior == "hold_after_plan":
                    self.server.plan_ready.set()
                    self.server.release.wait(40)
                name = "plan_commit"
                args = {"plan": preview["plan"]}
            calls = [{"id": "call_" + str(len(self.server.requests)), "type": "function",
                      "function": {"name": name, "arguments": json.dumps(args)}}]
        reply = {"choices": [{"message": {"role": "assistant", "content": None, "tool_calls": calls}}]}
        if behavior != "without_usage":
            reply["usage"] = {"prompt_tokens": 17, "completion_tokens": 6}
        raw = json.dumps(reply).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        try:
            self.wfile.write(raw)
        except (BrokenPipeError, ConnectionResetError):
            pass  # The test kills the original executor after the real plan was saved.


@unittest.skipUnless(os.environ.get("QIMENG_TEST_BASE") == "http://127.0.0.1:18481",
                     "requires isolated cloud Go")
class AppIntakeWorker(CloudGoFixture, unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.assertEqual(os.environ.get("CI"), "true")
        self.root = Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.host = ThreadingHTTPServer(("127.0.0.1", 0), ChatHost)
        self.host.host_key = secrets.token_hex(20)
        self.host.requests, self.host.behavior, self.host.batches, self.host.scripts = [], {}, {}, {}
        self.host.plan_ready, self.host.release = threading.Event(), threading.Event()
        thread = threading.Thread(target=self.host.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(thread.join)
        self.addCleanup(self.host.server_close)
        self.addCleanup(self.host.shutdown)
        self.addCleanup(self.host.release.set)
        self.library = self.request("POST", "/libraries", {"name": "Queue " + self._testMethodName, "kind": "regular"},
                                    key="auto-lib-" + self._testMethodName)
        self.secret = self.grant(can_research=True, limit=1, images=True,
                                 daily=1 if self._testMethodName.startswith("test_app_receives") else 12)
        self.env = {"PATH": os.environ["PATH"], "PYTHONPATH": os.environ["PYTHONPATH"],
                    "QIMENG_API_BASE": os.environ["QIMENG_TEST_BASE"], "QIMENG_API_TOKEN": self.secret,
                    "QIMENG_AI_STATE_DIR": str(self.root / "state"), "QIMENG_AI_PERMISSIONS": "read,delegate,organize,research",
                    "QIMENG_AI_WORKER_ORIGIN": f"http://127.0.0.1:{self.host.server_port}",
                    "QIMENG_AI_WORKER_MODEL": "fixture", "QIMENG_AI_WORKER_TOKEN": self.host.host_key,
                    "QIMENG_AI_WORKER_POLL_SECONDS": "1", "QIMENG_AI_WORKER_IMAGE_LOOKUP": "true"}

    def grant(self, *, can_research, limit, images, daily=12):
        grant = self.request("POST", "/ai/delegations", {
            "label": "Explicit App executor", "libraryIds": [self.library["id"]], "authorIds": [],
            "allAuthors": False, "allowedFields": [], "candidateBlocks": [], "allowClear": False,
            "canIngest": True, "canOrganize": True, "canResearch": can_research, "canLookupImages": images,
            "maxResearchCalls": limit, "maxUpdates": 1, "maxTasksPerDay": daily, "validDays": 1})
        self.addCleanup(self.request, "DELETE", "/ai/delegations/" + grant["delegation"]["id"])
        return grant["credential"]["token"]

    def submit(self, name, *, mixed=False, mode="intelligent", research_calls=None):
        body = {"libraryId": self.library["id"], "label": name, "mode": mode}
        if research_calls is not None:
            body["maxResearchCalls"] = research_calls
        batch = self.request("POST", "/batches", body, key=name + "-batch")
        source = self.root / (name + ".png")
        color = hashlib.sha256(name.encode()).hexdigest()[:6]
        subprocess.run(["ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=0x" + color + ":s=32x24",
                        "-frames:v", "1", "-threads", "1", str(source)], check=True, timeout=30)
        raw = source.read_bytes()
        self.source_sha = hashlib.sha256(raw).hexdigest()
        good = {"id": name + "-input", "originalFileName": source.name, "size": len(raw), "expectedHash": self.source_sha}
        inputs = [good]
        if mixed:
            inputs.append({"id": name + "-notes", "originalFileName": "notes.txt", "size": 7})
        self.request("PUT", "/batches/" + batch["id"] + "/inputs",
                     {"expectedManifestRevision": batch["manifestRevision"], "inputs": inputs}, key=name + "-seal")
        upload = self.request("POST", "/uploads", {"batchId": batch["id"], "inputId": good["id"],
                              "libraryId": self.library["id"], "mode": mode, "fileName": source.name,
                              "size": len(raw), "mimeType": "image/png", "expectedHash": self.source_sha}, key=name + "-upload")
        url = os.environ["QIMENG_TEST_BASE"] + "/api/v1/uploads/" + upload["id"] + "/content"
        chunk = urllib.request.Request(url, method="PUT", data=raw,
                                       headers={"Authorization": "Bearer " + os.environ["QIMENG_E2E_TOKEN"],
                                                "Upload-Offset": "0", "Content-Type": "application/octet-stream"})
        with urllib.request.urlopen(chunk, timeout=15) as response:
            self.assertEqual(json.load(response)["offset"], len(raw))
        self.request("POST", "/uploads/" + upload["id"] + "/complete", key=name + "-complete")
        self.host.batches[name] = batch["id"]
        return batch["id"]

    def launch(self, *args, env=None):
        return subprocess.Popen([sys.executable, "-m", "qimeng_ai.cli", "worker", "--once", *args],
                                env=env or self.env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)

    async def worker(self, *args, env=None, expected=0):
        process = self.launch(*args, env=env)
        try:
            stdout, stderr = await asyncio.to_thread(process.communicate, timeout=35)
        finally:
            if process.poll() is None:
                process.kill()
                process.wait()
        self.assertEqual(process.returncode, expected, stderr)
        return json.loads(stderr if expected else stdout)

    async def test_daily_control_recovers_parked_claim_without_renewing_owned_task(self):
        runtime = Runtime(Config.load({**self.env, "QIMENG_AI_MAX_CALLS": "2"}))
        self.addCleanup(runtime.close)
        owned = self.submit("worker-owned-ledger")
        self.host.behavior[owned] = "offline"
        def poll():
            return {row["batchId"]: row["state"] for row in run_worker(runtime, once=True, env=self.env)["batches"]}

        with patch("qimeng_ai.runtime.date") as calendar:
            calendar.today.return_value = date(2026, 1, 1)
            self.assertEqual(poll(), {owned: "WORKER_HOST_UNAVAILABLE"})
            for _ in range(2):
                runtime.call("platform.session", {"taskId": owned})
            runtime.call("platform.session", {})
            self.assertEqual(runtime.call("platform.budget", {})["calls"], 2)
            new = self.submit("worker-next-day")
            self.assertEqual(poll()[new], "TASK_BUDGET_EXHAUSTED")
            with runtime.connect().transaction() as db:
                row = db.execute("SELECT claim_key,paused,revision FROM worker_claims WHERE batch=?", (new,)).fetchone()
            self.assertEqual((row["paused"], row["revision"]), ("TASK_BUDGET_EXHAUSTED", None))
            await asyncio.sleep(1.1)
            self.assertNotIn(new, poll())
            calendar.today.return_value = date(2026, 1, 2)
            self.assertEqual(poll(), {new: "completed"})
            self.assertEqual(runtime.call("platform.budget", {"taskId": owned})["local"]["calls"], 2)
            self.assertEqual(self.request("GET", "/batches/" + owned)["inputs"][0]["state"], "staged")
            self.assertEqual(runtime.call("platform.budget", {})["calls"], 1)
            receipt = runtime.transport.call("getOperation", {"key": row["claim_key"]})
            self.assertEqual(receipt["response"]["taskId"], new)
            item, = self.request("GET", "/batches/" + new)["inputs"]
            self.assertEqual(self.request("GET", "/assets/" + item["assetId"])["hash"], self.source_sha)

    async def test_interrupted_claim_keeps_original_key_across_elapsed_control_day(self):
        runtime = Runtime(Config.load(self.env))
        self.addCleanup(runtime.close)
        original_call = runtime.transport.call
        def drop_claim_reply(operation, arguments):
            response = original_call(operation, arguments)
            if operation == "startDelegatedTask":
                raise ToolError("NETWORK_ERROR", "合成认领应答中断", uncertain=True)
            return response

        batch = self.submit("worker-claim-interrupted")
        with patch("qimeng_ai.runtime.date") as calendar:
            calendar.today.return_value = date(2026, 1, 1)
            with patch.object(runtime.transport, "call", side_effect=drop_claim_reply):
                failed, = run_worker(runtime, once=True, env=self.env)["batches"]
            self.assertEqual(failed["state"], "NETWORK_ERROR")
            control = runtime.connect()
            with control.transaction() as db:
                row = db.execute("SELECT claim_key,body FROM worker_claims WHERE batch=?", (batch,)).fetchone()
                db.execute("UPDATE tasks SET started=started-max_seconds WHERE id=?", (control.config.task_id,))
            self.assertEqual(runtime.transport.call("listReadyIntakeBatches", {})["items"], [])
            await asyncio.sleep(1.1)
            expired, = run_worker(runtime, once=True, env=self.env)["batches"]
            self.assertEqual(expired["state"], "TASK_BUDGET_EXHAUSTED")
            calendar.today.return_value = date(2026, 1, 2)
            body = json.loads(row["body"])
            for key, label in (("changed-operation-key", body["label"]), (row["claim_key"], "另一个请求")):
                with self.assertRaises(ToolError) as blocked:
                    runtime.call("task.start", {"operationId": key, "body": {**body, "label": label}})
                self.assertEqual(blocked.exception.code, "WRITE_UNRESOLVED")
            self.assertEqual([p["key"] for p in runtime.call("platform.budget", {})["pending"]], [row["claim_key"]])
            await asyncio.sleep(1.1)
            done, = run_worker(runtime, once=True, env=self.env)["batches"]
            self.assertEqual((done["batchId"], done["state"]), (batch, "completed"))
            self.assertEqual(runtime.call("platform.budget", {})["pending"], [])
            receipt = runtime.transport.call("getOperation", {"key": row["claim_key"]})
            self.assertEqual((receipt["response"]["taskId"], receipt["response"]["credential"]["generation"]), (batch, 1))
            final = self.request("GET", "/batches/" + batch)
            self.assertEqual([p["state"] for p in final["plans"]], ["completed"])
            self.assertEqual(self.request("GET", "/assets/" + final["inputs"][0]["assetId"])["hash"], self.source_sha)

    async def test_app_receives_mixed_work_across_process_death_without_a_chat_prompt(self):
        self.submit("worker-ordinary", mode="ordinary")
        self.assertEqual((await self.worker())["state"], "idle")
        self.assertEqual(self.host.requests, [])
        batch = self.submit("worker-mixed", mixed=True)
        self.host.behavior[batch] = "hold_after_plan"
        first = self.launch()
        try:
            self.assertTrue(await asyncio.to_thread(self.host.plan_ready.wait, 20))
            pending = self.request("GET", "/batches/" + batch)
            self.assertEqual([p["state"] for p in pending["plans"]], ["prepared"])
            self.assertEqual((await self.worker(expected=2))["code"], "WORKER_ALREADY_RUNNING")
            other = {**self.env, "QIMENG_AI_STATE_DIR": str(self.root / "independent")}
            unrelated = await self.worker(env=other)
            self.assertEqual((unrelated["state"], unrelated["admission"]), ("idle", "delegation_daily_limit"))
        finally:
            first.kill()
            await asyncio.to_thread(first.communicate)
            self.host.behavior.pop(batch, None)
            self.host.release.set()
        replay = await self.worker()
        self.assertEqual(replay["admission"], "delegation_daily_limit")
        result, = replay["batches"]
        self.assertEqual((result["batchId"], result["state"]), (batch, "partial"))
        info = result["modelUsage"]
        self.assertEqual((info["promptTokens"], info["completionTokens"]), (17 * info["turns"], 6 * info["turns"]))
        self.assertGreaterEqual(info["toolCalls"], 1)
        self.assertEqual(info["toolResults"], [{"tool": "plan.read", "code": "ok"}, {"tool": "plan.commit", "code": "ok"}])
        self.assertEqual(info["omittedToolResults"], 0)
        self.assertGreater(info["requestBytes"], info["toolSchemaBytes"])
        self.assertGreater(info["toolSchemaBytes"], 0)
        self.assertGreater(info["responseBytes"], 0)
        final = self.request("GET", "/batches/" + batch)
        self.assertEqual([p["id"] for p in final["plans"]], [pending["plans"][0]["id"]])
        self.assertEqual([p["state"] for p in final["plans"]], ["completed"])
        good, bad = final["inputs"]
        self.assertEqual((good["state"], bad["state"], bad["errorCode"]), ("ingested", "failed", "unsupported_media"))
        self.assertEqual(self.request("GET", "/assets/" + good["assetId"])["hash"], self.source_sha)
        self.assertEqual((await self.worker())["state"], "idle")
        for request in self.host.requests:
            encoded = json.dumps(request)
            self.assertFalse(any(key in encoded for key in (self.secret, self.host.host_key, os.environ["QIMENG_E2E_TOKEN"])))
            names = {entry["function"]["name"] for entry in request["tools"]}
            self.assertFalse(names & {"task_start", "task_resume", "source_recycle", "research_image_search",
                                      "reportIntakeExecutorStatus", "listReadyIntakeBatches"})

    async def test_waiting_batch_does_not_wake_the_model_or_starve_later_work(self):
        waiting = self.submit("worker-attention")
        self.host.behavior[waiting] = "wait"
        completed = self.submit("worker-later")
        result = (await self.worker())["batches"]
        self.assertEqual({b["batchId"]: b["state"] for b in result}, {waiting: "needs_attention", completed: "completed"})
        paused = self.request("GET", "/batches/" + waiting)["executorStatus"]
        self.assertEqual((paused["state"], paused["source"], paused["reason"]), ("paused", "worker", "needs_attention"))
        count = len(self.host.requests)
        await asyncio.sleep(1.1)
        self.assertEqual((await self.worker())["state"], "idle")
        self.assertEqual(len(self.host.requests), count)
        self.assertEqual(self.request("GET", "/batches/" + waiting)["executorStatus"], paused)
        del self.host.behavior[waiting]
        resumed, = (await self.worker("--retry-batch", waiting))["batches"]
        self.assertEqual(resumed["state"], "completed")

    async def test_unavailable_model_reports_one_pause_and_recovers_the_same_task(self):
        batch = self.submit("worker-offline")
        self.host.behavior[batch] = "offline"
        failed, = (await self.worker())["batches"]
        self.assertEqual(failed["state"], "WORKER_HOST_UNAVAILABLE")
        self.assertIsNone(failed["modelUsage"]["promptTokens"])
        paused = self.request("GET", "/batches/" + batch)["executorStatus"]
        self.assertEqual((paused["state"], paused["reason"]), ("paused", "host_unavailable"))
        await asyncio.sleep(1.1)
        failed, = (await self.worker())["batches"]
        self.assertEqual(failed["state"], "WORKER_HOST_UNAVAILABLE")
        self.assertEqual(self.request("GET", "/batches/" + batch)["executorStatus"], paused)
        del self.host.behavior[batch]
        await asyncio.sleep(1.1)
        completed, = (await self.worker())["batches"]
        self.assertEqual(completed["state"], "completed")
        self.assertEqual(len(self.request("GET", "/batches/" + batch)["plans"]), 1)

    async def test_default_app_batches_publish_with_zero_or_limited_research(self):
        cases = [
            ("android-no-research", None, False, 0, False, True),
            ("web-zero-budget", 8, True, 0, False, True),
            ("web-local-research", 8, True, 1, False, True),
            ("android-frame-opt-in", None, True, 3, True, True),
            ("web-organize-worker", 8, True, 3, True, False),
        ]
        for label, app_limit, allowed, limit, images, local_research in cases:
            with self.subTest(label=label):
                secret = self.grant(can_research=allowed, limit=limit, images=images)
                env = {**self.env, "QIMENG_API_TOKEN": secret}
                if not local_research:
                    env.update(QIMENG_AI_PERMISSIONS="read,delegate,organize", QIMENG_AI_WORKER_IMAGE_LOOKUP="false")
                batch = self.submit(label, mixed=True, research_calls=app_limit)
                self.host.requests.clear()
                effective = limit if local_research else 0
                def script(results):
                    if not results:
                        return "worker_tools", {"group": "research"}
                    if effective and len(results) == 1:
                        return "research_submit", {"input": 1, "body": {"outcome": "unknown", "candidates": [], "callsUsed": 1}}
                    if "plan" not in results[-1]:
                        return "plan_prepare_originals", {}
                    return "plan_commit", {"plan": results[-1]["plan"]}
                self.host.scripts[batch] = script
                outcome, = (await self.worker(env=env))["batches"]
                self.assertEqual((outcome["batchId"], outcome["state"]), (batch, "partial"))
                first, expanded = self.host.requests[:2]
                report = first["messages"][1]["content"]
                self.assertEqual(json.loads(report[report.index("{"):])["remainingResearchCalls"], effective)
                def functions(request):
                    return {entry["function"]["name"]: entry["function"]["parameters"] for entry in request["tools"]}
                groups = functions(first)["worker_tools"]["properties"]["group"]["enum"]
                self.assertEqual("research" in groups, effective > 0)
                tools = functions(expanded)
                self.assertNotIn("platform_artifact_read", tools)
                self.assertNotIn("research_provider_query", tools)
                self.assertEqual("research_image_search" in tools, bool(images and effective))
                if effective:
                    self.assertEqual(tools["research_submit"]["properties"]["body"]["properties"]["callsUsed"]["maximum"], effective)
                    if effective == 1:
                        self.assertNotIn("research_submit", functions(self.host.requests[2]))
                    self.assertEqual(outcome["modelUsage"]["toolResults"][1], {"tool": "research.submit", "code": "ok"})
                else:
                    self.assertFalse(any(name.startswith("research_") for name in tools))
                    self.assertEqual(outcome["modelUsage"]["toolResults"][0]["code"], "INVALID_INPUT")
                final = self.request("GET", "/batches/" + batch)
                self.assertEqual((final["batch"]["maxResearchCalls"], final["batch"]["researchCalls"]), (8, int(bool(effective))))
                good, bad = final["inputs"]
                self.assertEqual((good["state"], bad["state"], bad["errorCode"]), ("ingested", "failed", "unsupported_media"))
                self.assertEqual(self.request("GET", "/assets/" + good["assetId"])["hash"], self.source_sha)

    async def test_worker_rechecks_frozen_intent_and_original_plan_on_real_go(self):
        batch = self.submit("worker-intent-race", mixed=True, research_calls=1)
        path = "/batches/" + batch
        original = self.request("GET", path)
        upload = original["items"][0]["upload"]

        def intent(number, family):
            return {"items": [{"input": number, "family": family}]}

        def script(results):
            step = len(results)
            if step == 0:
                return "plan_prepare", intent(2, "Unreceived")
            if step == 1:
                return "plan_prepare", intent(1, "Original")
            if step == 2:
                current = self.request("GET", path)
                self.request("POST", path + "/research", {"expectedRevision": current["batch"]["revision"],
                    "uploadId": upload["id"], "expectedHash": upload["hash"], "outcome": "unknown", "candidates": [], "callsUsed": 1},
                    key="real-concurrent-intake-update")
                return "plan_commit", {"plan": results[-1]["plan"]}
            if step == 3:
                return "plan_prepare", intent(1, "Reviewed")
            if step == 4:
                return "task_context", {}
            if step == 5:
                return "plan_prepare", intent(1, "Reviewed")
            # Compatibility presenter supplies the exact older Go parameters.
            # The fixture can inspect the owner report; the genuine model never has this access.
            pending = self.request("GET", path)
            plans = [self.request("GET", path + "/plans/" + p["id"]) for p in pending["plans"]]
            chosen, = [p for p in plans if p["items"][0]["namingFamily"] == "Reviewed"]
            if step == 6:
                return "plan_commit", {"planId": chosen["id"], "body": {"fingerprint": "0" * 64}}
            return "plan_commit", {"planId": chosen["id"], "body": {"fingerprint": chosen["fingerprint"]}}

        self.host.scripts[batch] = script
        result, = (await self.worker())["batches"]
        self.assertEqual(result["state"], "partial")
        self.assertEqual([event["code"] for event in result["modelUsage"]["toolResults"]],
                         ["WORKER_NO_READY_INPUT", "ok", "revision_conflict", "revision_conflict", "ok", "ok", "plan_fingerprint_conflict", "ok"])
        final = self.request("GET", path)
        self.assertEqual(final["inputs"][1]["errorCode"], "unsupported_media")
        self.assertEqual(len(final["plans"]), 2)
        self.assertEqual(sum(plan["state"] == "completed" for plan in final["plans"]), 1)
        asset = self.request("GET", "/assets/" + final["inputs"][0]["assetId"])
        self.assertEqual((asset["hash"], asset["authorStatus"]), (upload["hash"], "unknown"))
        self.assertTrue(asset["fileName"].startswith("Reviewed"))
        self.assertEqual((await self.worker())["state"], "idle")

    async def test_naming_and_evidenced_author_selection_use_distinct_worker_inputs(self):
        batch = self.submit("worker-optional-author", research_calls=1)
        self.host.behavior[batch] = "without_usage"
        path = "/batches/" + batch
        detail = self.request("GET", path)
        upload = detail["items"][0]["upload"]
        author = self.request("POST", "/authors", {"kind": "regular", "name": "合成版画校对员"}, key="worker-optional-author-create")
        self.request("POST", path + "/research", {"expectedRevision": detail["batch"]["revision"], "uploadId": upload["id"],
            "expectedHash": upload["hash"], "outcome": "candidates", "callsUsed": 1,
            "candidates": [{"authorId": author["id"], "sourceUrl": "https://example.org/synthetic-credit",
                            "evidence": "Synthetic fixture records this author's credit for this input"}]}, key="worker-optional-author-evidence")

        def script(results):
            step = len(results)
            if step == 0:
                return "plan_prepare", {"items": [{"input": 1, "family": "版画", "authorIds": ["guessed-person"]}]}
            if step == 1:
                return "worker_tools", {"group": "authors"}
            if step == 2:
                return "author_query", {"exactName": author["name"]}
            if step == 3:
                found, = results[-1]["items"]
                return "plan_prepare", {"items": [{"input": 1, "family": "版画", "authorIds": [found["id"]]}]}
            return "plan_commit", {"plan": results[-1]["plan"]}

        self.host.scripts[batch] = script
        outcome, = (await self.worker())["batches"]
        self.assertEqual(outcome["state"], "completed")
        self.assertEqual([event["code"] for event in outcome["modelUsage"]["toolResults"]], ["WORKER_ENTITY_LOOKUP_REQUIRED", "ok", "ok", "ok", "ok"])
        def plan_fields(request):
            schema = next(tool["function"]["parameters"] for tool in request["tools"] if tool["function"]["name"] == "plan_prepare")
            return set(schema["properties"]["items"]["items"]["properties"])
        self.assertEqual(plan_fields(self.host.requests[0]), {"input", "family"})
        self.assertIn("authorIds", plan_fields(self.host.requests[2]))
        self.assertNotIn("collectionId", plan_fields(self.host.requests[2]))
        actual = self.request("GET", path)
        self.assertEqual(len(actual["plans"]), 1)
        asset = self.request("GET", "/assets/" + actual["inputs"][0]["assetId"])
        self.assertEqual((asset["authorIds"], asset["hash"]), ([author["id"]], upload["hash"]))
        self.assertIsNone(outcome["modelUsage"]["promptTokens"])
        self.assertIsNone(outcome["modelUsage"]["completionTokens"])

        stdio = self.process(self.root, self.secret, self.env["QIMENG_AI_PERMISSIONS"],
                             {"QIMENG_AI_TASK_ID": "interactive", "QIMENG_AI_ALLOW_MEDIA_TO_MODEL": "false"})
        async with Client(stdio) as client:
            listed = (await client.list_tools()).tools
        names = {tool.name for tool in listed}
        self.assertEqual(len(names), len(listed))
        self.assertTrue({"task.start", "task.resume", "source.recycle", "platform.artifact.read", "research.image.search"} <= names)

        def schema_sample(schemas):
            return {"toolCount": len(schemas), "inputSchemaBytes": len(encode(schemas).encode("utf-8"))}
        samples = {"stdioDefault": schema_sample([tool.input_schema for tool in listed])}
        for label, index in (("workerFirst", 0), ("workerAuthors", 2)):
            sent = self.host.requests[index]["tools"]
            names = {tool["function"]["name"] for tool in sent}
            self.assertEqual("author_query" in names, label == "workerAuthors")
            self.assertTrue(names.isdisjoint({"task_start", "task_resume", "author_profile_update", "source_recycle",
                                              "research_image_search", "platform_artifact_read", "collection_query"}))
            samples[label] = {**schema_sample([tool["function"]["parameters"] for tool in sent]),
                              "sentToolsBytes": len(encode(sent).encode("utf-8"))}
        self.assertLess(samples["workerFirst"]["toolCount"], samples["workerAuthors"]["toolCount"])
        self.assertLess(samples["workerFirst"]["inputSchemaBytes"], samples["workerAuthors"]["inputSchemaBytes"])
        evidence = {"toolMenus": samples, "verifiedAssetAuthorAndHash": True,
                    "promptTokens": None, "completionTokens": None, "cost": None}
        print("Intake tool measurements " + encode(evidence), flush=True)
        if summary := os.environ.get("GITHUB_STEP_SUMMARY"):
            with open(summary, "a", encoding="utf-8") as report:
                report.write("\n### 智能整理工具目录实测\n\n| 入口 | 工具数 | 输入 Schema 字节 | Worker 工具数组字节 |\n|---|---:|---:|---:|\n")
                for label, sample in samples.items():
                    report.write(f"| {label} | {sample['toolCount']} | {sample['inputSchemaBytes']} | {sample.get('sentToolsBytes', '')} |\n")
                report.write("\n按 UTF-8 紧凑 JSON 计数。脚本宿主完成作者选择，独立回读的资产作者和摘要一致。模型用量与费用未提供。\n")

    async def test_original_name_preview_and_mixed_intent_require_a_separate_plan_choice(self):
        batch = self.submit("worker-original-intents", mixed=True)
        captured = []
        def script(results):
            captured[:] = results
            step = len(results)
            if step == 0:
                return "plan_prepare", {"items": [{"input": 1, "family": "Must not apply"}, {"input": 99, "family": "Outside"}]}
            if step == 1:
                return "plan_prepare", {"items": [{"input": ["attachment"], "family": "Wrong selector"}]}
            if step == 2:
                return "worker_tools", {"group": "naming"}
            if step == 3:
                return "naming_suggest", {"q": "原图", "mediaType": "image"}
            if step == 4:
                return "naming_context", {"id": "another-library", "family": "原图", "mediaType": "image"}
            if step == 5:
                return "plan_prepare", {"items": [{"input": 1, "family": "Proposed"}, {"input": 2, "family": "Unusable"}]}
            if step == 6:
                return "worker_tools", {"group": "planning"}
            if step == 7:
                return "plan_prepare_originals", {}
            if step == 8:
                return "plan_commit", {}
            return "plan_commit", {"plan": results[7]["plan"]}

        self.host.scripts[batch] = script
        outcome, = (await self.worker())["batches"]
        self.assertEqual(outcome["state"], "partial")
        self.assertEqual([event["code"] for event in outcome["modelUsage"]["toolResults"]],
            ["WORKER_INPUT_OUT_OF_RANGE", "WORKER_INPUT_SELECTOR_TYPE", "ok", "ok", "WORKER_NAMING_LIBRARY_MISMATCH",
             "ok", "ok", "ok", "WORKER_PLAN_REVIEW_REQUIRED", "ok"])
        self.assertEqual(captured[0]["details"]["readyInputs"], [1])
        for index in (5, 7):
            self.assertEqual([row["input"] for row in captured[index]["items"]], [1])
            self.assertEqual(captured[index]["notPlanned"], [{"input": 2, "state": "failed", "reason": "unsupported_media"}])
        first = next(tool for tool in self.host.requests[0]["tools"] if tool["function"]["name"] == "plan_prepare")
        self.assertEqual(first["function"]["parameters"]["properties"]["items"]["items"]["properties"]["input"]["enum"], [1])
        detail = self.request("GET", "/batches/" + batch)
        self.assertEqual(sum(p["state"] == "completed" for p in detail["plans"]), 1)
        self.assertEqual(len(detail["plans"]), 2)
        asset = self.request("GET", "/assets/" + detail["inputs"][0]["assetId"])
        self.assertEqual(asset["hash"], self.source_sha)
        self.assertTrue(asset["fileName"].startswith("worker-original-intents"))
