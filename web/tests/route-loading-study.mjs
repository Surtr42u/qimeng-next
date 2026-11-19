import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { chromium, expect } from "@playwright/test";
import { fixedTime, makeInventory } from "./ui-reference-data.mjs";
import { serve, traffic } from "./ui-reference-server.mjs";

const author = "/authors/20000000-0000-4000-8000-000000000001";
const profile = author + "/profile", archives = author + "/archives", detail = archives + "/sample-archive";
const ai = "/settings/ai";
const samples = [], errors = [], servers = {};
const out = process.env.COMPARISON_DIR;
const browser = await chromium.launch({ args: ["--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1"] });
const network = { offline: false, latency: 40, downloadThroughput: 1024 * 1024, uploadThroughput: 512 * 1024 };
const version = { installed: process.env.REFERENCE_SHA, baseline: "b6b3a62b0ecc3049e27dc50e6ae9a663021d6e10",
  candidate: process.env.GITHUB_SHA };
const rounds = 5;

// Timers run inside the page. Driver waiting and the later network-settling
// period are excluded. SPA timers start at the capture phase of the link click.
function instrument({ author, profile, archives, detail, ai, side }) {
  if (side === "installed") {
    localStorage.setItem("qimeng_token", "fixture-only-session");
    localStorage.setItem("qimeng_theme", "dark");
  } else localStorage.setItem("qimeng.theme", "dark");
  let sequence = 0;
  const ready = {
    "/": ["#page-home .card"], "/app/home": ["#page-home .card"],
    "/settings": ['#page-settings a[href="/settings/ai"]'],
    [ai]: ['main .compact-field select', "main .empty-state h2"],
    [author]: ['#page-collection a[href$="/profile"]'],
    [profile]: ['button[aria-label="编辑基础资料"]:not(:disabled)'],
    [archives]: ['main a[href$="/archives/sample-archive"]'],
    [detail]: ['section[aria-label="归档详情"] h2', 'main a[href$="/archives/sample-archive"]'],
  };
  window.armStudy = (target, start) => {
    const serial = ++sequence;
    window.sample = null;
    const check = () => {
      if (sequence !== serial) return;
      const required = ready[target];
      if (location.pathname === target && required && required.every(selector => {
        const el = document.querySelector(selector);
        return el?.getClientRects().length && !el.closest("[inert]") && !el.matches(":disabled");
      }) && (!target.endsWith("/home") && target !== "/" || document.querySelectorAll("#page-home .card").length === 10)) {
        window.sample = { target, ms: Math.round(performance.now() - start) };
      } else requestAnimationFrame(check);
    };
    requestAnimationFrame(check);
  };
  document.addEventListener("click", event => {
    const link = event.target.closest?.("a[href]");
    if (link) window.armStudy(new URL(link.href).pathname, performance.now());
  }, true);
  window.armStudy(location.pathname, 0);
}

async function runTrajectory(name, round, journey) {
  const server = servers[name];
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1,
    locale: "zh-CN", timezoneId: "Asia/Shanghai", colorScheme: "dark", serviceWorkers: "block" });
  const page = await context.newPage();
  page.on("pageerror", error => errors.push(name + "/" + journey + ": " + error.message));
  page.on("request", request => {
    if (/^https?:/.test(request.url()) && new URL(request.url()).origin !== server.origin) errors.push("unexpected off-origin request");
  });
  await context.addInitScript(instrument, { author, profile, archives, detail, ai, side: name });
  await page.clock.setFixedTime(new Date(fixedTime));
  const networkSession = await context.newCDPSession(page);
  await networkSession.send("Network.enable");
  await networkSession.send("Network.emulateNetworkConditions", network);
  const start = server.mark();
  let mark = start;
  async function visit(stage, path, action) {
    if (action) await action();
    else await page.goto(server.origin + path, { waitUntil: "domcontentloaded" });
    await page.waitForFunction(path => window.sample?.target === path, path, { timeout: 12000 });
    const ms = await page.evaluate(() => window.sample.ms);
    if (path === "/" || path === "/app/home") {
      await expect(page.locator("#page-home .card")).toHaveCount(10);
      await page.evaluate(() => document.fonts.ready.then(() => true));
      await page.waitForFunction(() => [...document.querySelectorAll("#page-home .card--cover img")].every(img => {
        const r = img.getBoundingClientRect();
        return !r.width || !r.height || r.top >= innerHeight || r.left >= innerWidth || img.complete && img.naturalWidth === 640;
      }));
      await page.locator("#page-home .card").first().click({ trial: true });
    }
    if (name !== "installed") await page.waitForLoadState("networkidle");
    const segment = traffic(server, mark);
    mark = segment.to;
    const sum = requests => {
      const js = requests.filter(r => r.category === "app" && r.path.endsWith(".js"));
      return { all: requests.length, js: js.length, bytes: js.reduce((n, r) => n + r.outBytes, 0),
        totalBytes: requests.reduce((n, r) => n + r.outBytes, 0) };
    };
    const whole = traffic(server, start);
    const row = { name, round, journey, stage, ms,
      delta: { ...sum(segment.requests), wireOut: segment.wireOut, wireIn: segment.wireIn },
      cumulative: { ...sum(whole.requests), wireOut: whole.wireOut, wireIn: whole.wireIn },
      scripts: segment.requests.filter(r => r.path.endsWith(".js")).map(r => ({ path: r.path, bytes: r.outBytes })) };
    samples.push(row);
  }
  const link = title => () => page.getByRole("link", { name: title, exact: true }).click();
  const back = path => async () => {
    await page.evaluate(target => window.armStudy(target, performance.now()), path);
    await page.goBack();
  };
  const openArchives = async () => {
    await page.getByText("身份管理与历史归档", { exact: true }).click();
    await page.getByRole("link", { name: "历史归档", exact: true }).click();
  };
  try {
    if (journey === "home") {
      await visit("cold-home", name === "installed" ? "/app/home" : "/");
      if (name === "installed") return;
      await visit("settings", "/settings", link("设置"));
      await visit("first-ai", ai, link("AI 接入"));
      await visit("back-settings", "/settings", back("/settings"));
      await visit("repeat-ai", ai, link("AI 接入"));
    } else {
      await visit("cold-author", author);
      await visit("first-profile", profile, link("详细资料"));
      await visit("first-archives", archives, openArchives);
      await visit("first-detail", detail, () => page.locator('main a[href$="/archives/sample-archive"]').click());
      await visit("back-archives", archives, back(archives));
      await visit("back-profile", profile, back(profile));
      await visit("back-author", author, back(author));
      await visit("repeat-profile", profile, link("详细资料"));
      await visit("repeat-archives", archives, openArchives);
      await visit("repeat-detail", detail, () => page.locator('main a[href$="/archives/sample-archive"]').click());
      await visit("then-settings", "/settings", link("设置"));
      await visit("also-ai", ai, link("AI 接入"));
    }
  } finally { await context.close(); }
}

try {
  await mkdir(out, { recursive: true });
  const inventory = await makeInventory(browser);
  for (const [name, dist] of Object.entries({installed: process.env.REFERENCE_DIST, baseline: process.env.BASELINE_DIST, candidate: process.env.CANDIDATE_DIST})) {
    servers[name] = await serve(name === "installed" ? "installed" : "candidate", dist, inventory);
  }
  for (let round = 1; round <= rounds; round++) {
    const order = round % 2 ? ["baseline", "candidate"] : ["candidate", "baseline"];
    if (round % 2) order.unshift("installed"); else order.push("installed");
    for (const name of order) {
      console.log("Round " + round + ", " + name);
      await runTrajectory(name, round, "home");
      if (name !== "installed") await runTrajectory(name, round, "author");
    }
  }
} catch (error) { errors.push(error.stack); }
finally {
  for (const [name, server] of Object.entries(servers)) {
    if (server.missing.size) errors.push(name + " unmapped: " + [...server.missing].join(", "));
    await server.close();
  }
  const chromiumVersion = browser.version();
  await browser.close();
  const median = numbers => [...numbers].sort((a, b) => a - b)[Math.floor(numbers.length / 2)];
  const rows = [];
  for (const k of new Set(samples.map(r => [r.name, r.journey, r.stage].join(" ")))) {
    const batch = samples.filter(r => [r.name, r.journey, r.stage].join(" ") === k);
    rows.push([k, batch.length, median(batch.map(r => r.delta.all)), median(batch.map(r => r.delta.js)), median(batch.map(r => r.delta.bytes)),
      median(batch.map(r => r.cumulative.js)), median(batch.map(r => r.cumulative.bytes)), median(batch.map(r => r.ms)), batch.map(r => r.ms).join(", ")]);
  }
  await writeFile(join(out, "route-loading.json"), JSON.stringify({version, chromiumVersion, network, rounds, samples, errors}, null, 2));
  const report = ["## Secondary route loading", "", "Chromium " + chromiumVersion + ", Ubuntu 24.04, one machine. 1440x900, dark, DPR 1, 10 synthetic images. No SW/compression. Each trajectory starts in a fresh browser context; links and history reuse it. 40 ms simulated latency, 1 MiB/s down, 512 KiB/s up, no CPU throttle. Five alternating-order samples per build.",
    "", "Interaction time is from navigationStart or captured link click to the first animation frame with the actual usable page control and its fixture data. Detail waits for the archive content and the return link. Home waits for all ten cards; click actionability is separately checked. Script body bytes are counted at the same-origin fixture server. All requests includes API and media; totals shown here are medians.",
    "", "| Build / trajectory / stage | n | Requests | JS req | JS bytes | Cumulative JS req | Cumulative JS bytes | Ready ms | All ready samples (ms) |",
    "|---|---:|---:|---:|---:|---:|---:|---:|---|", ...rows.map(c => "| " + c.join(" | ") + " |"), "", ...errors];
  await writeFile(join(out, "route-loading.md"), report.join("\n") + "\n");
  console.log(report.join("\n"));
}
if (errors.length) process.exitCode = 1;
