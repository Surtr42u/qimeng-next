"""Read real representation headers and bytes from the verified reference Linux ZIP in CI."""
from contextlib import contextmanager
import hashlib
from html.parser import HTMLParser
import http.client
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import time

from smoke_server_package import unpack

BASELINE = "6d191737ae07e520aaa96020ee92aad917237d39"


class CoreEntries(HTMLParser):
    def __init__(self):
        super().__init__()
        self.urls = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        path = attrs.get("src") if tag == "script" and attrs.get("type") == "module" else (
            attrs.get("href") if tag == "link" and attrs.get("rel") == "stylesheet" else None)
        if path and path.startswith("/assets/"):
            self.urls.append(path)


def package(directory, dest, sha):
    archive, = directory.glob("qimeng-next-linux-amd64-*.zip")
    old = os.environ["GITHUB_SHA"]
    try:
        os.environ["GITHUB_SHA"] = sha
        return archive, unpack(archive, dest, "linux-amd64")
    finally:
        os.environ["GITHUB_SHA"] = old


@contextmanager
def running(app, state, port, admin):
    assert os.environ.get("GITHUB_ACTIONS") == "true" and 18000 <= port < 19000
    env = {k: v for k, v in os.environ.items() if not k.startswith("QIMENG_")}
    env.update(QIMENG_LISTEN=f"127.0.0.1:{port}", QIMENG_WEB_DIR=str(app / "web"),
               QIMENG_DATA_DIR=str(state), QIMENG_ADMIN_TOKEN=admin)
    process = subprocess.Popen([str(app / "qimeng-next")], env=env)
    try:
        for _ in range(100):
            if process.poll() is not None:
                raise RuntimeError("Packaged service exited early")
            try:
                if request(port, "/healthz")[0] == 200:
                    break
            except OSError:
                pass
            time.sleep(0.1)
        else:
            raise TimeoutError("Packaged service did not become ready")
        yield
    finally:
        process.terminate()
        process.wait(timeout=8)


def request(port, path, *, method="GET", headers=None, body=None):
    client = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
    try:
        client.request(method, path, body=body, headers=headers or {})
        response = client.getresponse()
        return response.status, dict((k.lower(), v) for k, v in response.getheaders()), response.read()
    finally:
        client.close()


def core(app):
    parser = CoreEntries()
    parser.feed((app / "web/index.html").read_text("utf-8"))
    assert len(parser.urls) == 2
    return parser.urls


def baseline():
    with tempfile.TemporaryDirectory() as temporary:
        work = Path(temporary)
        archive, app = package(Path("build/baseline"), work / "pkg", BASELINE)
        port = 18493
        with running(app, work / "data", port, secrets.token_hex(32)):
            resources = []
            for path in core(app):
                source = (app / "web" / path.lstrip("/")).read_bytes()
                for method, encoding in (("GET", "gzip, deflate, br"), ("HEAD", "gzip"), ("GET", "identity")):
                    status, headers, body = request(port, path, method=method, headers={"Accept-Encoding": encoding})
                    assert status == 200 and "content-encoding" not in headers and int(headers["content-length"]) == len(source)
                    assert body == (source if method == "GET" else b"")
                    resources.append(dict(path=path, method=method, accept=encoding, status=status, headers=headers,
                                          receivedBodyBytes=len(body), sha256=hashlib.sha256(source).hexdigest()))
            result = dict(source=BASELINE, zipBytes=archive.stat().st_size, webFiles=sum(p.is_file() for p in (app/"web").rglob("*")),
                          sidecars=len(list((app/"web").rglob("*.gz"))), resources=resources)
            print(json.dumps(result, indent=2))
            Path("build/baseline-http.json").write_text(json.dumps(result, indent=2)+"\n")


if __name__ == "__main__":
    baseline()
