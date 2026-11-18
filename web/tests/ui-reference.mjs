import { mkdir, writeFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { join } from "node:path";
import { chromium, expect } from "@playwright/test";
import { fixedTime, makeInventory } from "./ui-reference-data.mjs";
import { serve, traffic } from "./ui-reference-server.mjs";
import { compareImages, writeReport } from "./ui-reference-report.mjs";

const out = process.env.COMPARISON_DIR;
const installed = "745974081716a9d69a95002a41220e35e743d764";
if (process.env.REFERENCE_SHA !== installed || !process.env.GITHUB_SHA || !out) throw Error("Missing cloud comparison identity");
await mkdir(out, { recursive: true });
const browser = await chromium.launch({ args: ["--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1"] });
const servers = {};
const evidence = {
  installed, candidate: process.env.GITHUB_SHA, browser: browser.version(), time: fixedTime, locale: "zh-CN", timezone: "Asia/Shanghai", dpr: 1,
  font: execFileSync("fc-match", ["-f", "%{family}", "sans-serif:lang=zh-cn"], { encoding: "utf8" }),
  cases: [], pairs: [], errors: [],
};

async function settle(page, scene) {
  const scope = scene === "home" ? "#page-home" : scene === "album" ? "#page-albums" : "#page-asset";
  await page.mouse.move(0, 0);
  await page.locator(scope).waitFor({ state: "visible" });
  await page.evaluate(() => document.fonts.ready.then(() => true));
  await page.waitForFunction(selector => {
    const root = document.querySelector(selector);
    if (!root) return false;
    const images = [...root.querySelectorAll("img")].filter(img => {
      const r = img.getBoundingClientRect();
      return r.width > 0 && r.height > 0 && r.bottom > 0 && r.top < innerHeight && r.right > 0 && r.left < innerWidth;
    });
    const subject = root.querySelector(selector === "#page-asset" ? ".asset-img-open img" : ".card--cover img");
    return subject?.naturalWidth === 640 && images.every(img => img.complete && img.naturalWidth > 0) && !document.getAnimations().some(anim =>
      anim.playState === "running" && Number.isFinite(anim.effect?.getComputedTiming().endTime));
  }, scope, { timeout: 12000 });
  await page.evaluate(() => new Promise(done => requestAnimationFrame(() => requestAnimationFrame(done))));
}

function measure(scene) {
  const selector = scene === "home" ? "#page-home" : scene === "album" ? "#page-albums" : "#page-asset";
  const active = document.querySelector(selector);
  const list = document.querySelector(scene === "home" ? "#page-home" : "#page-albums");
  const text = (root, css) => [...(root?.querySelectorAll(css) ?? [])].map(el => el.textContent.trim());
  const nodes = {
    sidebar: ".sidebar", header: ".header", search: ".header .search", activeTab: ".tabs .tab.active",
    toolbar: selector + " .browse-toolbar", filters: selector + " .filter-card",
    firstDate: selector + " .album-group-title", firstCover: selector + " .card--cover",
    overlay: ".asset-overlay", stage: ".asset-stage", image: ".asset-img-open img",
    pager: ".asset-pager", title: ".detail-title", meta: ".detail-meta", actions: ".detail-actions",
    detailSide: ".detail-side", author: ".author-card", upNext: ".upnext-card",
  };
  const anchors = Object.fromEntries(Object.entries(nodes).map(([name, css]) => {
    const element = document.querySelector(css);
    if (!element?.getClientRects().length) return [name, null];
    const rect = element.getBoundingClientRect();
    return [name, [rect.x, rect.y, rect.width, rect.height].map(value => Math.round(value * 10) / 10)];
  }));
  const overlay = document.querySelector(".asset-overlay");
  const backdrop = document.querySelector(".overlay-backdrop");
  const style = overlay && getComputedStyle(overlay);
  const midpoint = document.elementFromPoint(innerWidth / 2, innerHeight / 2);
  const html = document.documentElement;
  const stageImage = active?.querySelector(".asset-img-open img");
  return {
    path: location.pathname, viewport: [innerWidth, innerHeight], dpr: devicePixelRatio, zoom: getComputedStyle(html).zoom,
    dark: html.classList.contains("dark"), font: getComputedStyle(document.body).fontFamily, anchors,
    ids: [...(list?.querySelectorAll(".card") ?? [])].map(el => el.dataset.id ?? el.getAttribute("href")?.split("/").pop()),
    titles: text(list, ".card--title"), captions: text(list, ".card--up"), cardDates: text(list, ".card--date"),
    groups: text(list, ".album-group-title"), toolbar: text(active, ".browse-toolbar"), filters: text(active, ".filter-card .pill-row-item .pill"),
    heading: text(active, ".detail-title"), pager: text(active, ".asset-pager__count"), authors: text(active, ".author-name"),
    nextTitles: text(active, ".upnext-title"), detailMeta: text(active, ".detail-meta"), tags: text(active, ".detail-tags .pill"),
    imageSize: stageImage ? [stageImage.naturalWidth, stageImage.naturalHeight] : null,
    layer: { backdropList: list?.id ?? null, inert: Boolean(backdrop?.inert), fixed: style?.position ?? null,
      zIndex: style?.zIndex ?? null, background: style?.backgroundColor ?? null, hitOverlay: !!overlay?.contains(midpoint) },
    scroll: { window: scrollY, main: document.querySelector(".main")?.scrollTop ?? null, content: document.querySelector(".content")?.scrollTop ?? null },
  };
}

async function captureSide(side, view, viewport, inventory) {
  const server = servers[side];
  const context = await browser.newContext({ viewport, deviceScaleFactor: 1, locale: evidence.locale, timezoneId: evidence.timezone,
    colorScheme: "dark", reducedMotion: "no-preference", serviceWorkers: "block" });
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", error => errors.push(error.message.slice(0, 300)));
  page.on("request", request => {
    if (/^https?:/.test(request.url()) && new URL(request.url()).origin !== server.origin) errors.push("请求了测试原点以外的资源");
  });
  let mark = server.mark();
  let scene = "home";
  try {
    await page.clock.setFixedTime(new Date(fixedTime));
    await context.addInitScript(side => {
      if (side === "installed") {
        localStorage.setItem("qimeng_token", "fixture-only-session");
        localStorage.setItem("qimeng_theme", "dark");
      } else localStorage.setItem("qimeng.theme", "dark");
    }, side);
    await page.goto(server.origin + (side === "installed" ? "/app/home" : "/"), { waitUntil: "domcontentloaded" });
    const take = async () => {
      await settle(page, scene);
      const metrics = await page.evaluate(measure, scene);
      const file = `${view}-${scene}-${side}.png`;
      await page.screenshot({ path: join(out, file) });
      const net = traffic(server, mark);
      mark = net.to;
      delete net.to;
      evidence.cases.push({ side, view, scene, file, metrics, net });
      const expected = inventory.map(a => a.id);
      if (JSON.stringify(metrics.ids) !== JSON.stringify(expected)) errors.push(`${scene} 的可见列表身份或顺序不同于合成输入`);
      if (!metrics.dark || metrics.dpr !== 1 || metrics.viewport.join("x") !== `${viewport.width}x${viewport.height}`) errors.push(`${scene} 主题、DPR 或视口不符`);
    };
    await expect(page.locator("#page-home .card")).toHaveCount(inventory.length, { timeout: 16000 });
    await take();
    scene = "album";
    await page.locator(".sidebar--pages .nav-item").filter({ hasText: "相册" }).click();
    const cards = page.locator("#page-albums .card");
    await expect(cards).toHaveCount(inventory.length);
    await expect(page.locator("#page-albums .album-group-title")).toHaveCount(2);
    await take();
    scene = "detail";
    const selected = 4;
    // The live list is the only entry into detail. Scroll into view exactly as a card click does.
    await cards.nth(selected).scrollIntoViewIfNeeded();
    const before = await page.evaluate(() => ({ window: scrollY, main: document.querySelector(".main")?.scrollTop, content: document.querySelector(".content")?.scrollTop }));
    await cards.nth(selected).click();
    await expect(page.locator("#page-asset .detail-title")).toHaveText(inventory[selected].title);
    await expect(page.locator("#page-asset .asset-img-open img")).toBeVisible();
    await expect(page.locator("#page-asset .asset-pager__count")).toHaveText(`${selected + 1} / ${inventory.length}`);
    await expect(page.locator("#page-asset .upnext-row")).toHaveCount(inventory.length - 1);
    await expect(page.locator("#page-asset .author-name")).toHaveText("合成作者");
    await take();
    const captured = evidence.cases.at(-1);
    if (!captured.metrics.layer.inert || !captured.metrics.layer.hitOverlay || captured.metrics.imageSize?.join("x") !== "640x360") errors.push("详情底衬、中心遮挡或原图尺寸不匹配");
    await page.goBack();
    await page.locator(".asset-overlay").waitFor({ state: "hidden" });
    await expect(cards).toHaveCount(inventory.length);
    const after = await page.evaluate(() => ({ window: scrollY, main: document.querySelector(".main")?.scrollTop, content: document.querySelector(".content")?.scrollTop }));
    captured.returnToAlbum = { before, after, path: new URL(page.url()).pathname };
    captured.returnNet = traffic(server, mark);
    delete captured.returnNet.to;
  } catch (error) {
    errors.push(`${scene}: ${error.message.slice(0, 1100)}`);
    await page.screenshot({ path: join(out, `${view}-${side}-failure.png`), timeout: 5000 }).catch(() => {});
  } finally {
    await context.close();
    for (const error of errors) evidence.errors.push(`${side}/${view} ${error}`);
  }
}

try {
  const inventory = await makeInventory(browser);
  evidence.inventory = inventory.map(({ bytes, ...item }) => ({ ...item, size: bytes.length }));
  for (const [side, dist] of Object.entries({ installed: process.env.REFERENCE_DIST, candidate: process.env.CANDIDATE_DIST })) servers[side] = await serve(side, dist, inventory);
  for (const [view, viewport] of Object.entries({ desktop: { width: 1440, height: 900 }, narrow: { width: 390, height: 844 } })) {
    for (const side of Object.keys(servers)) await captureSide(side, view, viewport, inventory);
    for (const scene of ["home", "album", "detail"]) {
      const pair = evidence.cases.filter(row => row.view === view && row.scene === scene);
      if (pair.length === 2) evidence.pairs.push(await compareImages(browser, out, pair[0], pair[1]));
    }
  }
} catch (error) {
  evidence.errors.push(error.message.slice(0, 900));
} finally {
  evidence.unmapped = Object.fromEntries(Object.entries(servers).map(([side, server]) => [side, [...server.missing]]));
  for (const [side, paths] of Object.entries(evidence.unmapped)) if (paths.length) evidence.errors.push(`${side} 未映射请求：${paths.join(", ")}`);
  if (evidence.pairs.length !== 6) evidence.errors.push(`只形成 ${evidence.pairs.length}/6 组有效对照`);
  await Promise.all(Object.values(servers).map(s => s.close()));
  await browser.close();
  await writeFile(join(out, "measurements.json"), JSON.stringify(evidence, null, 2));
  await writeReport(out, evidence);
}
if (evidence.errors.length) {
  console.error(evidence.errors.join("\n"));
  process.exitCode = 1;
}
