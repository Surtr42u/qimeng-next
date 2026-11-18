"""Owner setup and scoped stdio launch for the isolated cloud Go integration."""
import json
import os
from pathlib import Path
import sys
import urllib.request

from mcp import StdioServerParameters


class CloudGoFixture:
    def request(self, method, path, body=None, *, key=None):
        headers = {"Authorization": "Bearer " + os.environ["QIMENG_E2E_TOKEN"]}
        data = None
        if body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        if key:
            headers["Idempotency-Key"] = key
        request = urllib.request.Request(os.environ["QIMENG_TEST_BASE"] + "/api/v1" + path,
                                         data=data, headers=headers, method=method)
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
