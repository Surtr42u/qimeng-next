import { expect, test } from "@playwright/test";
import type { Page, Request } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { randomBytes, randomUUID } from "node:crypto";
import { mkdirSync, mkdtempSync } from "node:fs";
import { basename, dirname, join } from "node:path";
import type { Schema } from "../src/data/client";

test.use({ screenshot: "off", trace: "off", video: "off" });

function syntheticMedia(path: string) {
  const color = randomBytes(3).toString("hex");
  const format = path.endsWith(".mp4")
    ? ["-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart"]
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
  const headers = () => ({ Origin: origin!, "Idempotency-Key": randomUUID() });
  const created = await page.request.post("/api/v1/libraries", { headers: headers(),
    data: { name: basename(directory), kind: "regular", storageId: "media", rootRelativePath: basename(directory) } });
  expect(created.status()).toBe(201);
  const library: Schema<"Library"> = await created.json();
  const scan = await page.request.post(`/api/v1/libraries/${library.id}/scans`, { headers: headers(), data: { expectedRevision: library.revision } });
  expect(scan.ok()).toBe(true);
  const assets = async (): Promise<Schema<"Asset">[]> => (await (await page.request.get("/api/v1/assets", { params: { libraryId: library.id } })).json()).items;
  await expect.poll(async () => (await assets()).map(asset => asset.fileName).sort(), { timeout: 20_000 }).toEqual(names);
  const saved = await assets();
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
