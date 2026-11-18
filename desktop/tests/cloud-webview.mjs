import assert from "node:assert/strict";
import { execFile, execFileSync, spawn } from "node:child_process";
import { access, mkdir, mkdtemp, readdir, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { promisify } from "node:util";

assert(process.platform === "win32" && process.env.GITHUB_ACTIONS === "true" &&
  process.env.RUNNER_ENVIRONMENT === "github-hosted", "An isolated hosted Windows runner is required.");
const evidence = resolve("../build/desktop-evidence");
const temp = await mkdtemp(join(tmpdir(), "qimeng-native-window-"));
const installed = join(temp, "installed");
const bundles = resolve("src-tauri/target/release/bundle/nsis");
const installers = (await readdir(bundles)).filter(file => file.endsWith("-setup.exe"));
assert.equal(installers.length, 1, "Expected one installer from this build.");
await mkdir(evidence, { recursive: true });

async function site(label) {
  const seen = { range: false, page: false, trustedLinkClick: false, originalRequests: 0, original: false };
  const server = createServer((req, res) => {
    if (req.url === "/range") {
      const ok = req.headers.range === "bytes=4-7" && req.headers.cookie?.includes("synthetic_session=ok");
      res.writeHead(ok ? 206 : 403, { "Content-Range": "bytes 4-7/8" });
      res.end(ok ? "4567" : "");
      return;
    }
    if (req.method === "POST" && req.url === "/range-read") {
      seen.range = true;
      res.writeHead(204).end();
      return;
    }
    if (req.method === "POST" && req.url === "/trusted-link-click") {
      seen.trustedLinkClick = true;
      res.writeHead(204).end();
      return;
    }
    res.setHeader("Content-Type", "text/html;charset=utf-8");
    res.setHeader("Set-Cookie", "synthetic_session=ok; HttpOnly; SameSite=Strict; Path=/");
    if (req.url === "/original") {
      seen.originalRequests++;
      seen.original = req.headers.cookie?.includes("synthetic_session=ok") === true;
      if (!seen.original) { res.writeHead(403).end(); return; }
      res.end('<title>Original</title><button>Original media proof</button>');
      return;
    }
    seen.page = true;
    res.end('<title>' + label + '</title><header data-qimeng-titlebar>' +
      '<button data-window-action="minimize">Minimize ' + label + '</button>' +
      '<button data-window-action="toggleMaximize">Maximize ' + label + '</button>' +
      '<button data-window-action="close">Close ' + label + '</button></header>' +
      '<a href="/original" target="_blank">Open original</a>' +
      '<script>document.querySelector("a").addEventListener("click", e => { if (e.isTrusted) navigator.sendBeacon("/trusted-link-click"); });' +
      '(async()=>{if (!window.__QIMENG_DESKTOP__) return;' +
      'const r=await fetch("/range",{headers:{Range:"bytes=4-7"}});' +
      'if (r.status===206 && await r.text()==="4567") await fetch("/range-read",{method:"POST"});})();</script>');
  });
  await new Promise(done => server.listen(0, "127.0.0.1", done));
  return { server, origin: "http://127.0.0.1:" + server.address().port, seen };
}

const first = await site("one");
const second = await site("two");
let shell;
try {
  execFileSync(join(bundles, installers[0]), ["/S", "/D=" + installed],
    { windowsHide: true, stdio: "ignore", timeout: 300000 });
  const executable = join(installed, "qimeng-next-desktop.exe");
  await access(executable);
  shell = spawn(executable, ["--setup"], { windowsHide: true, stdio: "ignore",
    env: { ...process.env, WEBVIEW2_USER_DATA_FOLDER: join(temp, "webview") } });
  const exited = new Promise((done, fail) => { shell.once("exit", done); shell.once("error", fail); });
  // Windows PowerShell loads the native UI Automation assemblies included in .NET Framework.
  const result = await promisify(execFile)("powershell.exe", ["-NoProfile", "-NonInteractive", "-STA",
    "-File", resolve("tests/window-automation.ps1"), "-OwnerPid", String(shell.pid),
    "-FirstOrigin", first.origin, "-SecondOrigin", second.origin, "-Evidence", evidence],
    { windowsHide: true, timeout: 240000, encoding: "utf8" });
  process.stdout.write(result.stdout);
  assert.equal(await Promise.race([exited, delay(15000).then(() => "timeout")]), 0, "Closing the library should exit the shell.");
  assert(first.seen.page && first.seen.range && first.seen.original && second.seen.page && second.seen.range,
    "Both sites must confirm the desktop session, including a cookie-bearing range request.");
  const uninstaller = (await readdir(installed)).find(name => /^uninstall.*\.exe$/i.test(name));
  assert(uninstaller, "Expected an installed uninstaller.");
  execFileSync(join(installed, uninstaller), ["/S"], { windowsHide: true, stdio: "ignore", timeout: 60000 });
  const deadline = Date.now() + 20000;
  for (;;) {
    try { await access(executable); } catch { break; }
    assert(Date.now() < deadline, "Uninstaller left the executable behind.");
    await delay(150);
  }
  await writeFile(join(evidence, "native-window.json"), JSON.stringify({ sourceCommit: process.env.GITHUB_SHA,
    installedNSIS: true, setup: true, maximize: true, minimize: true, mediaWindow: true,
    switchClosesOldWindows: true, cookieRange: true, mainCloseExits: true, uninstalled: true }, null, 2) + "\n");
} catch (error) {
  console.error("Synthetic site receipts", JSON.stringify({ first: first.seen, second: second.seen }));
  if (error.stdout) process.stderr.write(error.stdout);
  if (error.stderr) process.stderr.write(error.stderr);
  throw error;
} finally {
  for (const { server } of [first, second]) { server.closeAllConnections(); server.close(); }
  if (shell?.exitCode === null) shell.kill();
}
