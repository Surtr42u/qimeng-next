import { expect, test } from "@playwright/test";
import type { APIRequestContext, Page, Request } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { randomBytes, randomUUID } from "node:crypto";
import { mkdirSync, mkdtempSync } from "node:fs";
import { basename, dirname, join } from "node:path";
import type { Schema } from "../src/data/client";

test.use({ screenshot: "off", trace: "off", video: "off" });

function syntheticMedia(path: string, seconds = 2) {
  const color = randomBytes(3).toString("hex");
  const format = path.endsWith(".mp4")
    ? ["-t", String(seconds), "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart"]
    : ["-frames:v", "1"];
  execFileSync("ffmpeg", ["-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i",
    `color=c=0x${color}:s=160x120:r=10`, "-threads", "1", ...format, path], { stdio: "pipe" });
}

async function login(page: Page, password: string) {
  await page.goto("/");
  await page.getByLabel("管理密码", { exact: true }).fill(password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "为你推荐" })).toBeVisible();
}

function eventFor(request: Request, assetId: string, kind: string) {
  return request.method() === "POST" && new URL(request.url()).pathname === `/api/v1/assets/${assetId}/events`
    && request.postDataJSON().kind === kind;
}

async function scanFixture(page: Page, directory: string, names: string[]) {
  const headers = () => ({ Origin: new URL(page.url()).origin, "Idempotency-Key": randomUUID() });
  const created = await page.request.post("/api/v1/libraries", { headers: headers(),
    data: { name: basename(directory), kind: "regular", storageId: "media", rootRelativePath: basename(directory) } });
  expect(created.status()).toBe(201);
  const library: Schema<"Library"> = await created.json();
  const scan = await page.request.post(`/api/v1/libraries/${library.id}/scans`, { headers: headers(), data: { expectedRevision: library.revision } });
  expect(scan.ok()).toBe(true);
  const assets = async (): Promise<Schema<"Asset">[]> => (await (await page.request.get("/api/v1/assets", { params: { libraryId: library.id } })).json()).items;
  await expect.poll(async () => (await assets()).map(asset => asset.fileName).sort(), { timeout: 20_000 }).toEqual(names);
  return { library, saved: await assets() };
}

// Wait for the original identities to reach the actual device store while disconnected.
function queuedViews(page: Page) {
  return page.evaluate(() => new Promise<Record<string, string>>((resolve, reject) => {
    const open = indexedDB.open("qimeng-device-state");
    open.onerror = () => reject(open.error);
    open.onsuccess = () => {
      const db = open.result;
      const tx = db.transaction("events", "readonly");
      const rows = tx.objectStore("events").getAll();
      tx.onabort = () => { db.close(); reject(tx.error); };
      tx.oncomplete = () => {
        db.close();
        const views = rows.result.filter(row => row.body.kind === "view" && !row.confirmed && !row.rejection);
        resolve(Object.fromEntries(views.map(row => [row.assetId, row.body.eventId])));
      };
    };
  }));
}

test("offline views survive another device replacing A, deliver B and C once, and keep A's rejection until cleared", async ({
  page, context, browser,
}, testInfo) => {
  test.setTimeout(90_000);
  const origin = process.env.QIMENG_E2E_BASE_URL;
  expect([process.env.CI, origin]).toEqual(["true", "http://127.0.0.1:18481"]);
  const password = process.env.QIMENG_E2E_TOKEN, root = process.env.QIMENG_E2E_LIBRARY_ROOT;
  if (!root || !password) throw new Error("缺少云端临时库或一次性测试口令");
  const directory = mkdtempSync(join(root, "event-delivery-"));
  const names = ["A.png", "B.png", "C.mp4"];
  for (const name of names) syntheticMedia(join(directory, name));
  const replacement = testInfo.outputPath("replacement.png");
  mkdirSync(dirname(replacement), { recursive: true });
  syntheticMedia(replacement);

  await login(page, password);
  const { library, saved } = await scanFixture(page, directory, names);
  const [a, b, c] = names.map(name => saved.find(asset => asset.fileName === name)!);
  if (!a || !b || !c) throw new Error("未取得本用例的三份合成媒体");

  await page.goto("/album?libraryId=" + library.id);
  const openCard = (name: string) => page.getByRole("link", { name: "打开 " + name, exact: true }).click();
  for (const asset of [a, b]) {
    const receipt = page.waitForResponse(r => eventFor(r.request(), asset.id, "view"));
    await openCard(asset.fileName);
    expect((await receipt).status()).toBe(200);
    await page.goBack();
  }
  await context.setOffline(true);
  await openCard(a.fileName);
  await expect.poll(() => queuedViews(page)).toHaveProperty(a.id);
  await page.goBack();
  await openCard(b.fileName);
  await expect.poll(() => queuedViews(page)).toHaveProperty(b.id);
  const queued = await queuedViews(page);

  const other = await browser.newContext({ baseURL: origin });
  try {
    const second = await other.newPage();
    await login(second, password);
    await second.goto(`/asset/${a.id}/replace`);
    await second.getByLabel("选择用于替换的图片或视频").setInputFiles(replacement);
    await second.getByRole("button", { name: "核对并替换" }).click();
    await second.getByRole("button", { name: "上传并替换" }).click();
    await expect(second.getByText("已完成", { exact: true })).toBeVisible({ timeout: 30_000 });
    const updated = await second.request.get(`/api/v1/assets/${a.id}`);
    expect((await updated.json()).contentVersion).toBeGreaterThan(a.contentVersion);
  } finally {
    await other.close();
  }

  const accepted: string[] = [];
  const refusedIds: string[] = [];
  page.on("response", r => {
    if (r.request().method() !== "POST" || !r.url().endsWith("/events")) return;
    const id = r.request().postDataJSON().eventId;
    if (r.status() === 200) accepted.push(id);
    if (r.status() === 409) refusedIds.push(id);
  });
  const oldView = page.waitForResponse(r => eventFor(r.request(), a.id, "view") && r.request().postDataJSON().eventId === queued[a.id]);
  const nextView = page.waitForResponse(r => eventFor(r.request(), b.id, "view") && r.request().postDataJSON().eventId === queued[b.id]);
  await context.setOffline(false);
  const [refused, delivered] = await Promise.all([oldView, nextView]);
  expect(refused.status()).toBe(409);
  expect(await refused.json()).toMatchObject({ code: "content_changed" });
  expect(await delivered.json()).toMatchObject({ assetId: b.id, eventId: queued[b.id] });
  await page.goBack();
  await page.reload();
  const summary = page.locator("summary").filter({ hasText: "服务器未接收" });
  await summary.click();
  const oldReceipt = page.getByRole("listitem").filter({ hasText: queued[a.id]! });
  await expect(oldReceipt).toContainText("content_changed");
  await expect(oldReceipt).toContainText(a.id);

  const freshView = page.waitForResponse(r => eventFor(r.request(), c.id, "view"));
  await openCard(c.fileName);
  const view = await (await freshView).json();
  const video = page.locator("video");
  await expect.poll(() => video.evaluate(el => (el as HTMLVideoElement).readyState)).toBeGreaterThan(0);
  await video.evaluate(async el => {
    const media = el as HTMLVideoElement;
    media.muted = true;
    await media.play();
    media.pause();
  });
  const seek = page.waitForResponse(r => eventFor(r.request(), c.id, "progress") && Math.abs(r.request().postDataJSON().positionMs - 750) < 80);
  await video.evaluate(el => { (el as HTMLVideoElement).currentTime = 0.75; });
  const progress = await (await seek).json();
  expect(progress).toMatchObject({ assetId: c.id, contentVersion: c.contentVersion, kind: "progress" });
  const detail = await page.request.get(`/api/v1/assets/${c.id}`);
  expect(await detail.json()).toMatchObject({ positionMs: progress.positionMs, positionContentVersion: c.contentVersion });
  const history = await page.request.get("/api/v1/history", { params: { libraryId: library.id } });
  const items: Schema<"HistoryEvent">[] = (await history.json()).items;
  expect(items.map(item => item.eventId)).toEqual(expect.arrayContaining([queued[b.id], view.eventId]));
  expect(items.map(item => item.eventId)).not.toContain(queued[a.id]);
  for (const id of [queued[b.id], view.eventId, progress.eventId]) expect(accepted.filter(eventId => eventId === id)).toHaveLength(1);
  await page.goBack();
  await oldReceipt.getByRole("button", { name: "清除此条记录" }).click();
  await expect(oldReceipt).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole("link", { name: "打开 " + b.fileName })).toBeVisible();
  await expect(page.locator(".connection-warning li").filter({ hasText: queued[a.id]! })).toHaveCount(0);
  expect(refusedIds.filter(id => id === queued[a.id])).toHaveLength(1);
});

type PlaybackGate = {
  path: string; position?: number; held: number; wait: Promise<void>; release: () => void;
};
type PlaybackWindow = Window & { playbackGate?: PlaybackGate };

// Delay delivery of a real Go response to this tab. The independent session sees the committed result.
async function installPlaybackGate(page: Page) {
  await page.addInitScript(() => {
    const send = window.fetch.bind(window);
    window.fetch = async (input, init) => {
      const response = await send(input, init);
      const gate = (window as PlaybackWindow).playbackGate;
      if (gate && response.ok && response.url && new URL(response.url).pathname === gate.path) {
        const body = gate.position === undefined ? undefined : await response.clone().json();
        if (gate.position === undefined || (body.kind === "progress" && Math.abs(body.positionMs - gate.position) < 100)) {
          gate.held++;
          await gate.wait;
        }
      }
      return response;
    };
  });
}

async function holdPlaybackResponse(page: Page, path: string, position?: number) {
  await page.evaluate(({ path, position }) => {
    let release!: () => void;
    const wait = new Promise<void>(resolve => { release = resolve; });
    (window as PlaybackWindow).playbackGate = { path, position, held: 0, wait, release };
  }, { path, position });
  return {
    observed: () => expect.poll(() => page.evaluate(() => (window as PlaybackWindow).playbackGate?.held ?? 0)).toBeGreaterThan(0),
    release: () => page.evaluate(() => {
      const tab = window as PlaybackWindow;
      tab.playbackGate?.release();
      delete tab.playbackGate;
    }),
  };
}

async function playbackFixture(page: Page, observer: APIRequestContext) {
  const origin = process.env.QIMENG_E2E_BASE_URL;
  expect([process.env.CI, origin]).toEqual(["true", "http://127.0.0.1:18481"]);
  const password = process.env.QIMENG_E2E_TOKEN, root = process.env.QIMENG_E2E_LIBRARY_ROOT;
  if (!root || !password) throw new Error("缺少云端临时库或一次性测试口令");
  const directory = mkdtempSync(join(root, "video-resume-"));
  const name = "resume.mp4";
  syntheticMedia(join(directory, name), 40);
  await login(page, password);
  const loginReceipt = await observer.post("/api/v1/sessions", { data: { password, label: "Synthetic playback observer" } });
  expect(loginReceipt.ok()).toBe(true);
  const sessionId = async (client: APIRequestContext) => (await (await client.get("/api/v1/sessions/current")).json()).id;
  expect(await sessionId(observer)).not.toBe(await sessionId(page.request));
  const { library, saved } = await scanFixture(page, directory, [name]);
  const asset = saved[0]!;
  const url = "/api/v1/assets/" + asset.id;
  const detail = async (): Promise<Schema<"Asset">> => (await observer.get(url)).json();
  const seed = async (positionMs: number, occurredAt = new Date().toISOString()) => {
    const accepted = await observer.post(url + "/events", { headers: { Origin: origin! },
      data: { kind: "progress", contentVersion: asset.contentVersion, sessionId: randomUUID(), eventId: randomUUID(), positionMs, occurredAt } });
    expect(accepted.ok()).toBe(true);
  };
  const card = page.getByRole("link", { name: "打开 " + name, exact: true });
  const video = page.locator(".art-video-player video");
  await page.goto("/album?libraryId=" + library.id);
  const open = async () => {
    await card.click();
    await expect.poll(() => video.evaluate(el => (el as HTMLVideoElement).readyState)).toBeGreaterThan(0);
  };
  const back = async () => {
    await page.getByRole("button", { name: "返回", exact: true }).click();
    await expect(card).toBeVisible();
  };
  const position = () => video.evaluate(el => Math.round((el as HTMLVideoElement).currentTime * 1000));
  const seek = async (fraction: number) => {
    await video.hover();
    const track = await page.locator(".art-control-progress").boundingBox();
    if (!track) throw new Error("原播放器进度条未显示");
    const y = track.y + track.height / 2;
    await page.mouse.move(track.x + track.width / 8, y);
    await page.mouse.down();
    await page.mouse.move(fraction === 0 ? track.x - 3 : track.x + track.width * fraction, y);
    await page.mouse.up();
  };
  const toggle = async () => {
    await video.hover();
    await page.locator(".art-control-playAndPause").click();
  };
  return { asset, url, detail, seed, open, back, position, seek, toggle, video };
}

test("album reopens within the cache lifetime at Go's position and a delayed detail cannot rewind active playback", async ({ page, request }) => {
  test.setTimeout(90_000);
  await installPlaybackGate(page);
  const f = await playbackFixture(page, request);
  await f.seed(4_000);
  let reads = 0;
  page.on("request", req => {
    if (req.method() === "GET" && new URL(req.url()).pathname === f.url) reads++;
  });
  await f.open();
  await expect.poll(f.position).toBeCloseTo(4_000, -1);
  const firstReadCount = reads;
  await f.toggle();
  await expect.poll(f.position).toBeGreaterThan(4_100);
  await f.toggle();
  await expect.poll(() => f.video.evaluate(el => (el as HTMLVideoElement).paused)).toBe(true);
  const receipt = await holdPlaybackResponse(page, f.url + "/events", 20_000);
  await f.seek(0.5);
  await receipt.observed();
  const first = await f.detail();
  expect(first.positionContentVersion).toBe(f.asset.contentVersion);
  expect(Math.abs(first.positionMs - 20_000)).toBeLessThan(100);
  expect(reads).toBe(firstReadCount);
  const leftAt = Date.now();
  await f.back();
  await f.open();
  expect(Date.now() - leftAt).toBeLessThan(30_000);
  expect(await f.position()).toBe(0);
  await receipt.release();
  await expect.poll(f.position).toBeCloseTo(first.positionMs, -1);

  // An older event can be accepted without becoming the current resume point.
  await f.seed(27_000, new Date(Date.now() + 60_000).toISOString());
  await f.toggle();
  await expect.poll(f.position).toBeGreaterThan(first.positionMs + 100);
  await f.toggle();
  const late = page.waitForResponse(r => eventFor(r.request(), f.asset.id, "progress") &&
    Math.abs(r.request().postDataJSON().positionMs - 12_000) < 100);
  await f.seek(0.3);
  const saved = await late;
  expect(saved.status()).toBe(200);
  const replay = await request.post(f.url + "/events", { headers: { Origin: new URL(page.url()).origin }, data: saved.request().postDataJSON() });
  expect(await replay.json()).toEqual(await saved.json());
  expect(await f.detail()).toMatchObject({ positionMs: 27_000, positionContentVersion: f.asset.contentVersion, playCount: 2 });
  await f.back();
  const read = await holdPlaybackResponse(page, f.url);
  await f.open();
  await read.observed();
  expect(await f.position()).toBe(0);
  await f.toggle();
  await expect.poll(f.position).toBeGreaterThan(100);
  await read.release();
  await expect.poll(f.position).toBeGreaterThan(700);
  expect(await f.position()).toBeLessThan(10_000);
  await f.back();
  await f.open();
  await expect.poll(f.position).toBeCloseTo(27_000, -1);
  expect(await f.detail()).toMatchObject({ positionMs: 27_000, positionContentVersion: f.asset.contentVersion, playCount: 3 });
});

test("initially paused video saves the progress-bar drag, keyboard seek and explicit zero without a play", async ({ page, request }) => {
  test.setTimeout(90_000);
  await installPlaybackGate(page);
  const f = await playbackFixture(page, request);
  await f.seed(6_000);
  const positions: number[] = [];
  const plays: string[] = [];
  page.on("request", req => {
    if (eventFor(req, f.asset.id, "progress")) positions.push(req.postDataJSON().positionMs);
    if (eventFor(req, f.asset.id, "play")) plays.push(req.postDataJSON().eventId);
  });
  const progressAt = (position: number) => page.waitForResponse(r =>
    eventFor(r.request(), f.asset.id, "progress") && Math.abs(r.request().postDataJSON().positionMs - position) < 100);
  await f.open();
  await expect.poll(f.position).toBeCloseTo(6_000, -1);
  await f.back();
  const nextView = page.waitForResponse(r => eventFor(r.request(), f.asset.id, "view"));
  await f.open();
  expect((await nextView).status()).toBe(200);
  await expect.poll(f.position).toBeCloseTo(6_000, -1);
  expect(positions).toEqual([]);
  expect(await f.detail()).toMatchObject({ positionMs: 6_000, playCount: 0 });

  const dragged = progressAt(25_000);
  await f.seek(0.625);
  await f.back();
  const first = await (await dragged).json();
  expect(first).toMatchObject({ kind: "progress", contentVersion: f.asset.contentVersion });
  expect(await f.detail()).toMatchObject({ positionMs: first.positionMs, positionContentVersion: f.asset.contentVersion, playCount: 0 });
  await f.open();
  await expect.poll(f.position).toBeCloseTo(first.positionMs, -1);
  await f.video.hover();
  await page.locator(".art-control-time").click();
  const key = progressAt(first.positionMs - 5_000);
  await page.keyboard.press("ArrowLeft");
  const moved = await (await key).json();
  expect(await f.detail()).toMatchObject({ positionMs: moved.positionMs, playCount: 0 });
  await f.back();

  const read = await holdPlaybackResponse(page, f.url);
  await f.open();
  await read.observed();
  const manual = progressAt(10_000);
  await f.seek(0.25);
  await read.release();
  const selected = await (await manual).json();
  await expect.poll(f.position).toBeCloseTo(selected.positionMs, -1);
  expect(await f.detail()).toMatchObject({ positionMs: selected.positionMs, playCount: 0 });
  await f.back();
  await f.open();
  await expect.poll(f.position).toBeCloseTo(selected.positionMs, -1);
  const reset = progressAt(0);
  await f.seek(0);
  await f.back();
  const zero = await reset;
  expect(await zero.json()).toMatchObject({ kind: "progress", contentVersion: f.asset.contentVersion, positionMs: 0 });
  await f.open();
  await expect.poll(f.position).toBe(0);
  const replay = await request.post(f.url + "/events", { headers: { Origin: new URL(page.url()).origin }, data: zero.request().postDataJSON() });
  expect(await replay.json()).toEqual(await zero.json());
  expect(await f.detail()).toMatchObject({ positionMs: 0, positionContentVersion: f.asset.contentVersion, playCount: 0 });
  expect(positions.filter(value => value === 0)).toHaveLength(1);
  expect(plays).toEqual([]);
});
