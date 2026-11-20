"""Exercise the unmodified Hermes CLI with real Qimeng stdio tools and Go.

Only the external model is scripted. The upstream release's isolated profile
and recording provider make the assertions about what Hermes actually sends
back to the model, including its provider-safe translation of dotted tool names.
"""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

from tests.e2e.core.mcp_plugins._helpers import (
    FINAL, build_home, provider, run_chat_q, script, tool_names, tool_results,
)
from tests.e2e.core.mcp_plugins._plugin_helpers import reap_tagged
from tools.mcp_tool_schema import mcp_prefixed_tool_name


def wait_until_ready(process, origin):
    for _ in range(60):
        if process.poll() is not None:
            raise RuntimeError("Synthetic Go instance stopped before readiness")
        try:
            with urllib.request.urlopen(origin + "/api/v1/instance", timeout=1) as response:
                return json.load(response)["id"]
        except (OSError, urllib.error.URLError):
            time.sleep(0.25)
    raise RuntimeError("Timed out waiting for the isolated Go instance")


def run():
    if os.environ.get("GITHUB_ACTIONS") != "true":
        raise SystemExit("The Hermes integration is a cloud-only check")
    workspace = Path(os.environ["GITHUB_WORKSPACE"])
    binary = Path(os.environ["RUNNER_TEMP"]) / "server-artifact/build/qimeng-next"
    binary.chmod(0o700)
    origin = "http://127.0.0.1:18489"
    reader = "synthetic-hermes-cloud-isolated-reader"
    with tempfile.TemporaryDirectory(prefix="qimeng-hermes-") as directory:
        root = Path(directory)
        environment = {
            "PATH": os.environ["PATH"], "QIMENG_DATA_DIR": str(root / "library"),
            "QIMENG_LISTEN": "127.0.0.1:18489", "QIMENG_AI_TOKEN": reader,
            "QIMENG_ADMIN_TOKEN": "synthetic-hermes-cloud-isolated-owner",
        }
        backend = subprocess.Popen([binary], env=environment)
        try:
            instance_id = wait_until_ready(backend, origin)
            wanted = ["platform.instance", "library.query", "plan.commit"]
            host_names = [mcp_prefixed_tool_name("qimeng", name) for name in wanted]
            server = {
                "command": os.environ["QIMENG_MCP_PYTHON"],
                "args": ["-m", "qimeng_ai.cli", "mcp"],
                "env": {
                    "PYTHONPATH": str(workspace / "ai/src"), "QIMENG_API_BASE": origin,
                    "QIMENG_API_TOKEN": reader, "QIMENG_AI_STATE_DIR": str(root / "executor"),
                    "QIMENG_AI_PERMISSIONS": "read,organize",
                },
            }
            refusal = {"id": "synthetic-hermes-refusal", "Idempotency-Key": "synthetic-hermes-key",
                       "body": {"fingerprint": "synthetic"}}
            calls = zip(host_names, [{}, {}, refusal], strict=True)
            with provider(script(*calls)) as recorder:
                profile = build_home(root / "hermes", recorder.base_url, mcp_servers={"qimeng": server},
                                     extra={"tools": {"tool_search": {"enabled": "off"}}})
                try:
                    cli = run_chat_q(profile, "Use the three specified library tools and report their results.")
                finally:
                    reap_tagged(profile)
                requests = recorder.main_requests()
                if not requests:
                    raise RuntimeError(f"Hermes did not contact the loopback model (exit {cli.returncode})")
                offered = tool_names(requests[0])
                if not set(host_names) <= offered:
                    raise RuntimeError(f"Missing Hermes tools: {sorted(set(host_names) - offered)}")
                results = tool_results(recorder)
                if len(results) < 3 or instance_id not in results[0] or "items" not in results[1] or "scope_denied" not in results[2]:
                    raise RuntimeError("Hermes did not deliver the instance, library list and server scope refusal to the model")
                if cli.returncode or FINAL not in cli.stdout:
                    raise RuntimeError(f"Hermes did not complete its tool turn (exit {cli.returncode})")
        finally:
            backend.terminate()
            try:
                backend.wait(timeout=15)
            except subprocess.TimeoutExpired:
                backend.kill()
                backend.wait()
    print("Hermes 0.23.2 CLI passed discovery, model-visible results and denied write against the real Go instance")


if __name__ == "__main__":
    run()
