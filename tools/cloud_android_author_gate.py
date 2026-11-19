"""Hold observed author loads and fail the first follow in the cloud UI fixture."""

import json
import time
from http.client import HTTPConnection
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from threading import Lock
from urllib.parse import parse_qs, urlsplit


class AuthorFixtureGate(BaseHTTPRequestHandler):
    seen = set()
    lock = Lock()

    def log_message(self, *_):
        pass

    def first(self, key):
        with self.lock:
            if key in self.seen:
                return False
            self.seen.add(key)
            return True

    def forward(self):
        url = urlsplit(self.path)
        author = parse_qs(url.query).get("authorId", [None])[0]
        if self.command == "GET" and url.path == "/api/v1/assets" and author and self.first(("browse", author)):
            time.sleep(6)

        size = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(size) if size else None
        author_path = url.path.removeprefix("/api/v1/authors/")
        if self.command == "PATCH" and author_path != url.path and "/" not in author_path:
            if body and "following" in json.loads(body) and self.first(("follow", author_path)):
                time.sleep(2)
                self.send_error(503)
                return

        hop_headers = {"connection", "content-length", "keep-alive", "transfer-encoding"}
        headers = {name: value for name, value in self.headers.items() if name.lower() not in hop_headers}
        upstream = HTTPConnection("127.0.0.1", 18481, timeout=30)
        try:
            upstream.request(self.command, self.path, body=body, headers=headers)
            response = upstream.getresponse()
            payload = response.read()
            self.send_response(response.status)
            for name, value in response.getheaders():
                if name.lower() not in hop_headers:
                    self.send_header(name, value)
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
        except (BrokenPipeError, ConnectionResetError):
            # The app can leave a page while a delayed first read is outstanding.
            pass
        finally:
            upstream.close()

    do_GET = do_HEAD = do_POST = do_PUT = do_PATCH = do_DELETE = forward


if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", 18484), AuthorFixtureGate).serve_forever()
