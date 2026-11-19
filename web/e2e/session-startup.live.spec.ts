import { expect, test } from "@playwright/test";
import type { Page, Response } from "@playwright/test";
import { createServer, request } from "node:http";
import type { AddressInfo } from "node:net";

const go = "http://127.0.0.1:18481";
const sessionPath = "/api/v1/sessions/current";
const sessionRead = (response: Response) =>
  new URL(response.url()).pathname === sessionPath && response.request().method() === "GET";

test.use({ screenshot: "off", trace: "off", video: "off" });
test.beforeEach(() => {
  expect([process.env.CI, process.env.QIMENG_E2E_BASE_URL]).toEqual(["true", go]);
});

async function submitLogin(page: Page) {
  const password = process.env.QIMENG_E2E_TOKEN;
  if (!password) throw new Error("缺少云端临时服务口令");
  await page.getByLabel("管理密码", { exact: true }).fill(password);
  const [confirmed] = await Promise.all([
    page.waitForResponse(response => sessionRead(response) && response.status() === 200),
    page.getByRole("button", { name: "登录", exact: true }).click(),
  ]);
  await expect(page.getByRole("navigation", { name: "主导航" })).toBeVisible();
  return (await confirmed.json()).id as string;
}

async function login(page: Page) {
  await page.goto("/");
  return submitLogin(page);
}

test("a background session check preserves the login field, its draft and its focus", async ({ page, context }) => {
  await page.addInitScript(() => {
    const release = new Promise<void>(resolve => window.addEventListener("release-session-check", () => resolve(), { once: true }));
    const send = window.fetch.bind(window);
    let reads = 0;
    window.fetch = async (input, init) => {
      const response = await send(input, init);
      if (new URL(response.url).pathname === "/api/v1/sessions/current" && ++reads === 2) await release;
      return response;
    };
  });
  await page.goto("/");
  const field = page.getByLabel("管理密码", { exact: true });
  const draft = "尚未提交的合成文字";
  await field.fill(draft);
  const original = await field.evaluateHandle(el => el);
  await context.setOffline(true);
  const [held] = await Promise.all([page.waitForResponse(sessionRead), context.setOffline(false)]);
  expect(held.status()).toBe(401);
  const checkDraft = async () => {
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));
    expect(await original.evaluate(el => el.isConnected && document.activeElement === el)).toBe(true);
    await expect(field).toHaveValue(draft);
  };
  await checkDraft();
  await page.evaluate(() => window.dispatchEvent(new Event("release-session-check")));
  await checkDraft();
  await submitLogin(page);
});

test("a controlled PWA reload offline offers a new probe and reconnects to the same Go session", async ({ page, context }) => {
  const id = await login(page);
  await page.evaluate(async () => { await navigator.serviceWorker.ready; });
  await page.reload();
  const navigation = page.getByRole("navigation", { name: "主导航" });
  await expect(navigation).toBeVisible();
  expect(await page.evaluate(() => !!navigator.serviceWorker.controller)).toBe(true);

  await context.setOffline(true);
  await expect(page.getByText(/网络已断开。/)).toBeVisible();
  await expect(navigation).toBeVisible();
  const shell = await page.reload();
  expect(shell?.fromServiceWorker()).toBe(true);
  await expect(page.getByRole("alert")).toContainText("无法连接影库");
  await expect(navigation).toHaveCount(0);
  await Promise.all([
    page.waitForEvent("requestfailed", req => new URL(req.url()).pathname === "/api/v1/instance"),
    page.getByRole("button", { name: "重试", exact: true }).click(),
  ]);
  await expect(page.getByRole("alert")).toContainText("无法连接影库");
  const [restored] = await Promise.all([page.waitForResponse(sessionRead), context.setOffline(false)]);
  expect(restored.status()).toBe(200);
  expect((await restored.json()).id).toBe(id);
  await expect(navigation).toBeVisible();
});

test("going offline between Go checks still reads a reachable loopback session, and Go's 401 clears it", async ({ page, context }) => {
  const id = await login(page);
  // Keep the genuine instance response until Chromium has reported offline to this page.
  await page.addInitScript(() => {
    const offline = new Promise<void>(resolve => window.addEventListener("offline", () => resolve(), { once: true }));
    const send = window.fetch.bind(window);
    window.fetch = async (input, init) => {
      const response = await send(input, init);
      if (new URL(response.url).pathname === "/api/v1/instance") await offline;
      return response;
    };
  });
  const devtools = await context.newCDPSession(page);
  await devtools.send("Network.enable");
  const reportOffline = (offline: boolean) => devtools.send("Network.overrideNetworkState", {
    offline, latency: 0, downloadThroughput: -1, uploadThroughput: -1,
  });
  const [instance] = await Promise.all([
    page.waitForResponse(response => new URL(response.url()).pathname === "/api/v1/instance"),
    page.reload(),
  ]);
  expect(instance.status()).toBe(200);
  const [local] = await Promise.all([page.waitForResponse(sessionRead), reportOffline(true)]);
  expect(await page.evaluate(() => navigator.onLine)).toBe(false);
  expect(local.status()).toBe(200);
  expect((await local.json()).id).toBe(id);
  const navigation = page.getByRole("navigation", { name: "主导航" });
  await expect(navigation).toBeVisible();

  const revoked = await page.request.delete(sessionPath, { headers: { Origin: go } });
  expect(revoked.status()).toBe(204);
  const [denied] = await Promise.all([page.waitForResponse(sessionRead), reportOffline(false)]);
  expect(denied.status()).toBe(401);
  await expect(page.getByLabel("管理密码", { exact: true })).toBeVisible();
  await expect(navigation).toHaveCount(0);

  // A transport failure on the next reconnect cannot reinstate the revoked session.
  await devtools.send("Network.setBlockedURLs", { urls: [go + sessionPath] });
  await reportOffline(true);
  await expect.poll(() => page.evaluate(() => navigator.onLine)).toBe(false);
  await reportOffline(false);
  await expect(page.getByRole("alert")).toContainText("无法连接影库");
  await expect(navigation).toHaveCount(0);
  await devtools.send("Network.setBlockedURLs", { urls: [] });
  const [retried] = await Promise.all([
    page.waitForResponse(sessionRead),
    page.getByRole("button", { name: "重试", exact: true }).click(),
  ]);
  expect(retried.status()).toBe(401);
  await expect(page.getByLabel("管理密码", { exact: true })).toBeVisible();
});

for (const path of ["/api/v1/instance", sessionPath]) {
  test(`the startup deadline closes a partial HTTP body at ${path} and retry checks Go`, async ({ page }) => {
    let probes = 0;
    let interrupted = false;
    // Only the first target GET stalls. The application, the retry and the 401 all come from Go.
    const gateway = createServer((incoming, outgoing) => {
      if (incoming.method === "GET" && incoming.url === path && ++probes === 1) {
        outgoing.writeHead(200, { "Content-Type": "application/json" });
        outgoing.write("{");
        outgoing.once("close", () => { interrupted = !outgoing.writableEnded; });
        return;
      }
      const upstream = request(go, { path: incoming.url, method: incoming.method, headers: incoming.headers }, response => {
        outgoing.writeHead(response.statusCode!, response.headers);
        response.pipe(outgoing);
      });
      upstream.on("error", error => outgoing.destroy(error));
      incoming.pipe(upstream);
    });
    await new Promise<void>((resolve, reject) => {
      gateway.once("error", reject);
      gateway.listen(0, "127.0.0.1", resolve);
    });
    try {
      await page.goto(`http://127.0.0.1:${(gateway.address() as AddressInfo).port}`);
      await expect(page.getByRole("alert")).toContainText("等待影库响应超时", { timeout: 16_000 });
      await expect.poll(() => interrupted).toBe(true);
      const [response] = await Promise.all([
        page.waitForResponse(sessionRead),
        page.getByRole("button", { name: "重试", exact: true }).click(),
      ]);
      expect(probes).toBe(2);
      expect(response.status()).toBe(401);
      await expect(page.getByLabel("管理密码", { exact: true })).toBeVisible();
    } finally {
      await new Promise<void>(resolve => {
        gateway.close(() => resolve());
        gateway.closeAllConnections();
      });
    }
  });
}
