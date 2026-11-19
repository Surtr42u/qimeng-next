import { expect, test } from "@playwright/test";

test.use({ screenshot: "off", trace: "off", video: "off" });
test.beforeEach(() => {
  expect([process.env.CI, process.env.QIMENG_E2E_BASE_URL]).toEqual(["true", "http://127.0.0.1:18481"]);
});

test("the unmodified instance gate stays on loading after 503 then disconnect", async ({ page, context }) => {
  const path = "/api/v1/instance";
  let reads = 0;
  page.on("request", req => { if (new URL(req.url()).pathname === path) reads++; });
  await page.route("**" + path, route => route.fulfill({
    status: 503, json: { code: "unavailable", message: "合成影库暂不可用" },
  }), { times: 1 });
  await Promise.all([
    page.waitForResponse(r => new URL(r.url()).pathname === path && r.status() === 503).then(() => context.setOffline(true)),
    page.goto("/"),
  ]);
  await expect(page.getByRole("status")).toHaveText("正在连接影库…");
  await page.waitForTimeout(2_500);
  expect(await page.evaluate(() => navigator.onLine)).toBe(false);
  await expect(page.getByRole("status")).toHaveText("正在连接影库…");
  await expect(page.getByRole("button", { name: "重试" })).toHaveCount(0);
  expect(reads).toBe(1);
  const [restored] = await Promise.all([
    page.waitForResponse(r => new URL(r.url()).pathname === path),
    context.setOffline(false),
  ]);
  expect(restored.status()).toBe(200);
  await expect(page.getByLabel("管理密码", { exact: true })).toBeVisible();
  expect(reads).toBe(2);
  console.log("BASELINE controlled instance=503, offline sample=2500ms, instance GETs=1, retry buttons=0, visible=正在连接影库…, reconnect real Go instance=200");
});

test("the unmodified startup gate stays pending with no session GET until the online event", async ({ page, context }) => {
  const password = process.env.QIMENG_E2E_TOKEN;
  if (!password) throw new Error("Missing synthetic service credential");
  await page.goto("/");
  await page.getByLabel("管理密码", { exact: true }).fill(password);
  const sessionPath = "/api/v1/sessions/current";
  const [original] = await Promise.all([
    page.waitForResponse(r => new URL(r.url()).pathname === sessionPath && r.status() === 200),
    page.getByRole("button", { name: "登录", exact: true }).click(),
  ]);
  const id = (await original.json()).id;
  await expect(page.getByRole("navigation", { name: "主导航" })).toBeVisible();
  await page.addInitScript(() => {
    const offline = new Promise<void>(resolve => window.addEventListener("offline", () => resolve(), { once: true }));
    const native = window.fetch.bind(window);
    window.fetch = async (input, init) => {
      const response = await native(input, init);
      if (new URL(response.url).pathname === "/api/v1/instance") await offline;
      return response;
    };
  });
  let reads = 0;
  page.on("request", req => { if (new URL(req.url()).pathname === sessionPath) reads++; });
  const devtools = await context.newCDPSession(page);
  await devtools.send("Network.enable");
  const report = (offline: boolean) => devtools.send("Network.overrideNetworkState", {
    offline, latency: 0, downloadThroughput: -1, uploadThroughput: -1,
  });
  const [instance] = await Promise.all([
    page.waitForResponse(r => new URL(r.url()).pathname === "/api/v1/instance"),
    page.reload(),
  ]);
  expect(instance.status()).toBe(200);
  await report(true);
  await expect(page.getByRole("status")).toHaveText("正在确认设备会话…");
  // Sample beyond the library's default one-second first retry while loopback remains reachable.
  await page.waitForTimeout(2_500);
  expect(await page.evaluate(() => navigator.onLine)).toBe(false);
  await expect(page.getByRole("status")).toHaveText("正在确认设备会话…");
  await expect(page.getByRole("button", { name: "重试" })).toHaveCount(0);
  expect(reads).toBe(0);
  const [restored] = await Promise.all([
    page.waitForResponse(r => new URL(r.url()).pathname === sessionPath),
    report(false),
  ]);
  expect(restored.status()).toBe(200);
  expect((await restored.json()).id).toBe(id);
  expect(reads).toBe(1);
  await expect(page.getByRole("navigation", { name: "主导航" })).toBeVisible();
  console.log("BASELINE Go instance=200, offline sample=2500ms, session GETs=0, retry buttons=0, visible=正在确认设备会话…, reconnect GETs=1, same Go session=200");
});
