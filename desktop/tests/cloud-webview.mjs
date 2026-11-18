import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { createServer } from "node:http";
import { createServer as tcpServer } from "node:net";
import { access, mkdir, mkdtemp, readdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

assert(process.platform === "win32" && process.env.GITHUB_ACTIONS === "true", "An isolated Windows cloud runner is required.");
const evidence = resolve("../build/desktop-evidence");
await mkdir(evidence, { recursive: true });
const temp = await mkdtemp(join(tmpdir(), "qimeng-native-window-"));
const installed = join(temp, "installed");
const bundles = resolve("src-tauri/target/release/bundle/nsis");
const installers = (await readdir(bundles)).filter(file => file.endsWith("-setup.exe"));
assert.equal(installers.length, 1, "Expected one NSIS installer from this build.");
// Launch the same installer the owner receives so its Evergreen prerequisite runs on clean Windows images.
execFileSync(join(bundles, installers[0]), ["/S", "/D=" + installed], { windowsHide: true, stdio: "ignore", timeout: 300000 });
const executable = join(installed, "qimeng-next-desktop.exe");
await access(executable);
const listen = async server => { await new Promise(done => server.listen(0, "127.0.0.1", done)); return server.address().port; };
async function until(check, label) {
  const deadline = Date.now() + 60000;
  while (Date.now() < deadline) { const value = await check(); if (value) return value; await delay(200); }
  throw new Error(label);
}
function site(label) {
  return createServer((req, res) => {
    if (req.url === "/range") {
      const authorized = req.headers.range === "bytes=4-7" && req.headers.cookie?.includes("synthetic_session=ok");
      res.writeHead(authorized ? 206 : 400, { "Content-Range": "bytes 4-7/8", "Content-Type": "application/octet-stream" }); res.end("4567"); return;
    }
    res.setHeader("Content-Type", "text/html;charset=utf-8");
    res.setHeader("Set-Cookie", "synthetic_session=ok; HttpOnly; SameSite=Strict; Path=/");
    if (req.url === "/original") { res.end("<title>synthetic-original</title><p>原件窗口</p>"); return; }
    res.end('<title>' + label + '</title><header data-qimeng-titlebar style="height:60px"><button data-window-action="minimize">最小化</button><button data-window-action="toggleMaximize">最大化</button><button data-window-action="close">关闭</button></header><p>' + label + '</p><a href="/original" target="_blank">打开原件</a>');
  });
}
const first = site("synthetic-one"), second = site("synthetic-two");
const origins = ["http://127.0.0.1:" + await listen(first), "http://127.0.0.1:" + await listen(second)];
const probe = tcpServer(); const port = await listen(probe); await new Promise(done => probe.close(done));
const shell = spawn(executable, ["--setup"], { windowsHide: true, stdio: "ignore", env: { ...process.env,
  WEBVIEW2_USER_DATA_FOLDER: join(temp, "webview"), WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS: "--remote-debugging-port=" + port } });
const exited = new Promise((done, fail) => { shell.once("exit", done); shell.once("error", fail); });
let debugReachable = false;
let targetSummary = [];
async function targets() {
  try {
    const list = await (await fetch("http://127.0.0.1:" + port + "/json/list", { signal: AbortSignal.timeout(2000) })).json();
    debugReachable = true;
    targetSummary = list.map(item => ({ type: /^[a-z_]{1,20}$/.test(item.type) ? item.type : "other", location: isSetup(item.url) ? "setup" : item.url === "about:blank" ? "blank" : origins.some(origin => item.url.startsWith(origin + "/")) ? "synthetic" : "other" }));
    return list;
  }
  catch { if (shell.exitCode !== null) throw new Error("Desktop application exited before its window opened."); return []; }
}
function isSetup(url) { return /^https?:\/\/tauri\.localhost\/connection\.html$/.test(url) || url === "tauri://localhost/connection.html"; }
class Page {
  constructor(socket) {
    this.socket = socket; this.id = 0; this.pending = new Map();
    socket.onmessage = event => { const m = JSON.parse(event.data); if (!m.id) return;
      const p = this.pending.get(m.id); this.pending.delete(m.id); if (m.error) p?.fail(new Error(m.error.message)); else p?.done(m.result); };
  }
  static async connect(predicate) {
    const target = await until(async () => (await targets()).find(t => ["page", "webview"].includes(t.type) && predicate(t.url)), "Expected a WebView2 target.");
    const socket = new WebSocket(target.webSocketDebuggerUrl);
    await new Promise((done, fail) => { socket.onopen = done; socket.onerror = fail; });
    return new Page(socket);
  }
  command(method, params = {}) {
    const id = ++this.id;
    return new Promise((done, fail) => {
      const timer = setTimeout(() => { this.pending.delete(id); fail(new Error("Timed out: " + method)); }, 10000);
      this.pending.set(id, { done: value => { clearTimeout(timer); done(value); }, fail: err => { clearTimeout(timer); fail(err); } });
      this.socket.send(JSON.stringify({ id, method, params }));
    });
  }
  async value(expression) {
    const result = await this.command("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true });
    if (result.exceptionDetails) throw new Error("A hosted WebView2 fixture action failed.");
    return result.result.value;
  }
  async click(selector) {
    const point = await this.value("(() => { const b=document.querySelector(" + JSON.stringify(selector) + ").getBoundingClientRect(); return {x:b.x+b.width/2,y:b.y+b.height/2}; })()");
    await this.command("Input.dispatchMouseEvent", { type: "mousePressed", button: "left", buttons: 1, clickCount: 1, ...point });
    await this.command("Input.dispatchMouseEvent", { type: "mouseReleased", button: "left", buttons: 0, clickCount: 1, ...point });
  }
}
const state = (restore=false) => JSON.parse(execFileSync("pwsh.exe", ["-NoProfile", "-NonInteractive", "-File", resolve("tests/window-state.ps1"), "-OwnerPid", String(shell.pid), ...(restore ? ["-Restore"] : [])], { encoding: "utf8", windowsHide: true, timeout: 10000 }));
const pages = [];
try {
  const setup = await Page.connect(isSetup); pages.push(setup);
  await until(() => setup.value("!!document.querySelector('#save') && !!window.__TAURI__?.core"), "Bundled setup did not initialize.");
  const screen = await setup.command("Page.captureScreenshot");
  await writeFile(join(evidence, "native-connection.png"), Buffer.from(screen.data, "base64"));
  const connect = origin => setup.value("document.querySelector('#address').value=" + JSON.stringify(origin) + ";document.querySelector('form').requestSubmit()");
  await connect(origins[0]);
  const library = await Page.connect(url => url === origins[0] + "/"); pages.push(library);
  await until(() => library.value("!!window.__QIMENG_DESKTOP__ && !!document.querySelector('[data-window-action]')"), "Desktop title bar did not initialize.");
  assert.deepEqual(await library.value("fetch('/range',{headers:{Range:'bytes=4-7'}}).then(async r=>[r.status,await r.text()])"), [206, "4567"]);
  const before = state();
  await library.click("[data-window-action=toggleMaximize]");
  await until(() => state().maximized !== before.maximized, "Native maximize did not change.");
  await library.click("[data-window-action=minimize]");
  await until(() => state().minimized, "Native minimize did not take effect.");
  state(true);
  await library.click("a");
  const media = await Page.connect(url => url === origins[0] + "/original"); pages.push(media);
  assert.equal(await media.value("document.title"), "synthetic-original");
  await connect(origins[1]);
  const next = await Page.connect(url => url === origins[1] + "/"); pages.push(next);
  await until(async () => !(await targets()).some(t => t.url.startsWith(origins[0] + "/")), "Old media or library window survived a server change.");
  await until(() => next.value("!!document.querySelector('[data-window-action=close]')"), "Replacement library did not initialize.");
  // Closing destroys the target and can disconnect CDP before it acknowledges the mouseup.
  const click = next.click("[data-window-action=close]");
  await Promise.race([click, exited]);
  assert.equal(await Promise.race([exited, delay(20000).then(() => "timeout")]), 0, "Closing the main window must exit the process.");
  const uninstall = (await readdir(installed)).find(file => /^uninstall.*\.exe$/i.test(file));
  assert(uninstall, "Installed desktop must include its uninstaller.");
  execFileSync(join(installed, uninstall), ["/S"], { windowsHide: true, stdio: "ignore", timeout: 60000 });
  await until(async () => { try { await access(executable); return false; } catch { return true; } }, "Desktop uninstaller did not remove its installed executable.");
  await writeFile(join(evidence, "native-window.json"), JSON.stringify({ sourceCommit: process.env.GITHUB_SHA, installedNSIS: true, setup: true, maximize: true, minimize: true, mediaWindow: true, switchClosesOldWindows: true, cookieRange: true, mainCloseExits: true, uninstalled: true }, null, 2) + "\n");
} catch (error) {
  // Keep user/environment paths, CDP URLs and cookie values out of failure evidence.
  console.error("WebView2 diagnostics", JSON.stringify({ debugReachable, targetSummary, exitCode: shell.exitCode }));
  throw error;
} finally {
  for (const page of pages) page.socket.close();
  first.closeAllConnections(); second.closeAllConnections(); first.close(); second.close();
  if (shell.exitCode === null) shell.kill();
}
