import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { resolve, extname, sep } from "node:path";
import { adapter } from "./ui-reference-data.mjs";

const mime = { ".html": "text/html; charset=utf-8", ".js": "text/javascript", ".css": "text/css", ".png": "image/png",
  ".svg": "image/svg+xml", ".woff2": "font/woff2", ".woff": "font/woff", ".ico": "image/x-icon", ".webmanifest": "application/manifest+json", ".json": "application/json" };

export async function serve(side, dist, inventory) {
  const root = resolve(dist);
  const api = adapter(side, inventory);
  const requests = [];
  const missing = new Set();
  const sockets = new Set();
  let closedIn = 0, closedOut = 0;
  const server = createServer(async (req, res) => {
    const url = new URL(req.url, "http://fixture.invalid");
    const path = decodeURIComponent(url.pathname);
    const entry = { method: req.method, path, category: "app", status: 0, inBytes: 0, outBytes: 0 };
    requests.push(entry);
    const send = (kind, status, type, bytes, headers = {}) => {
      entry.category = kind;
      entry.status = status;
      entry.outBytes = bytes.length;
      res.writeHead(status, { "Content-Type": type, "Content-Length": bytes.length, "Cache-Control": "no-store", ...headers });
      res.end(bytes);
    };
    const unknown = () => {
      missing.add(`${req.method} ${path}`);
      send("unmapped", 404, "text/plain", Buffer.from("Unmapped synthetic fixture route"));
    };
    try {
      for await (const chunk of req) entry.inBytes += chunk.length;
      const media = side === "installed" ? path.match(/^\/media\/(?:thumb|orig)\/([\w-]+)$/) : path.match(/^\/api\/v1\/assets\/([\w-]+)\/(?:thumbnail|content)$/);
      if (media) {
        const asset = inventory.find(a => a.id === media[1]);
        if (!asset || req.method !== "GET") return unknown();
        return send("media", 200, "image/png", asset.bytes, { "Cache-Control": "private, max-age=3600" });
      }
      if (side === "installed" && path === "/api/v1/events" && req.method === "GET") {
        const line = ": synthetic fixture\n\n";
        Object.assign(entry, { category: "events", status: 200, outBytes: Buffer.byteLength(line) });
        res.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-store" });
        res.write(line);
        return;
      }
      if (path.startsWith("/api/")) {
        if (!path.startsWith("/api/v1/")) return unknown();
        const result = api(path.slice("/api/v1".length), url.searchParams, req.method);
        if (!result) return unknown();
        if (path.endsWith("/recommendations")) entry.limit = url.searchParams.get("limit");
        return send("api", result.status, "application/json; charset=utf-8", Buffer.from(JSON.stringify(result.body)), result.headers);
      }
      const html = req.headers["sec-fetch-mode"] === "navigate" && req.method === "GET";
      const file = resolve(root, "." + (html ? "/index.html" : path));
      if (!file.startsWith(root + sep) || req.method !== "GET") return unknown();
      const bytes = await readFile(file);
      const headers = html
        ? { "Cache-Control": "no-cache", "Content-Security-Policy": "connect-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; font-src 'self' data:" }
        : { "Cache-Control": "private, max-age=3600" };
      send("app", 200, mime[extname(file)] ?? "application/octet-stream", bytes, headers);
    } catch (error) {
      if (error.code === "ENOENT") return unknown();
      missing.add(`${req.method} ${path} [${error.code ?? "server_error"}]`);
      if (!res.headersSent) send("error", 500, "text/plain", Buffer.from("Fixture server error"));
      else res.destroy();
    }
  });
  server.on("connection", socket => {
    sockets.add(socket);
    socket.once("close", () => { closedIn += socket.bytesRead; closedOut += socket.bytesWritten; sockets.delete(socket); });
  });
  await new Promise((resolveListen, reject) => { server.once("error", reject); server.listen(0, "127.0.0.1", resolveListen); });
  return {
    origin: `http://127.0.0.1:${server.address().port}`, requests, missing,
    mark() {
      let wireIn = closedIn, wireOut = closedOut;
      for (const socket of sockets) { wireIn += socket.bytesRead; wireOut += socket.bytesWritten; }
      return { start: requests.length, wireIn, wireOut };
    },
    close() { return new Promise(done => { server.close(done); server.closeAllConnections(); }); },
  };
}

export function traffic(server, from) {
  const to = server.mark();
  const categories = {};
  for (const entry of server.requests.slice(from.start, to.start)) {
    const sum = categories[entry.category] ??= { requests: 0, inBytes: 0, outBytes: 0 };
    sum.requests++;
    sum.inBytes += entry.inBytes;
    sum.outBytes += entry.outBytes;
  }
  return { wireIn: to.wireIn - from.wireIn, wireOut: to.wireOut - from.wireOut, categories,
    requests: server.requests.slice(from.start, to.start), to };
}
