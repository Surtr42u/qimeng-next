"""An App submits real files. Only the local model is scripted in this integration."""
import asyncio
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
import urllib.request

from live_fixture import CloudGoFixture


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
        reply = {"choices": [{"message": {"role": "assistant", "content": None, "tool_calls": calls}}],
                 "usage": {"prompt_tokens": 17, "completion_tokens": 6}}
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
        grant = self.request("POST", "/ai/delegations", {
            "label": "Explicit App executor", "libraryIds": [self.library["id"]], "authorIds": [],
            "allAuthors": False, "allowedFields": [], "candidateBlocks": [], "allowClear": False,
            "canIngest": True, "canOrganize": True, "canResearch": True, "canLookupImages": True,
            "maxResearchCalls": 1, "maxUpdates": 1,
            "maxTasksPerDay": 1 if self._testMethodName.startswith("test_app_receives") else 12, "validDays": 1})
        self.addCleanup(self.request, "DELETE", "/ai/delegations/" + grant["delegation"]["id"])
        self.secret = grant["credential"]["token"]
        self.env = {"PATH": os.environ["PATH"], "PYTHONPATH": os.environ["PYTHONPATH"],
                    "QIMENG_API_BASE": os.environ["QIMENG_TEST_BASE"], "QIMENG_API_TOKEN": self.secret,
                    "QIMENG_AI_STATE_DIR": str(self.root / "state"), "QIMENG_AI_PERMISSIONS": "read,delegate,organize,research",
                    "QIMENG_AI_WORKER_ORIGIN": f"http://127.0.0.1:{self.host.server_port}",
                    "QIMENG_AI_WORKER_MODEL": "fixture", "QIMENG_AI_WORKER_TOKEN": self.host.host_key,
                    "QIMENG_AI_WORKER_POLL_SECONDS": "1", "QIMENG_AI_WORKER_IMAGE_LOOKUP": "true"}

    def submit(self, name, *, mixed=False, mode="intelligent", research_calls=0):
        batch = self.request("POST", "/batches", {"libraryId": self.library["id"], "label": name,
                             "mode": mode, "maxResearchCalls": research_calls}, key=name + "-batch")
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
                         ["WORKER_INPUT_NOT_STAGED", "ok", "revision_conflict", "revision_conflict", "ok", "ok", "plan_fingerprint_conflict", "ok"])
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
