import { expect, test } from "@playwright/test";

test.use({ screenshot: "off", trace: "off", video: "off" });

test("an uncached PWA page recovers on reconnect and a cached page opens offline in a fresh document", async ({ page, context }) => {
  expect([process.env.CI, process.env.QIMENG_E2E_BASE_URL]).toEqual(["true", "http://127.0.0.1:18481"]);
  const password = process.env.QIMENG_E2E_TOKEN;
  if (!password) throw new Error("缺少云端临时库的测试口令");
  await page.goto("/settings");
  await page.getByLabel("管理密码").fill(password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const link = page.getByRole("link", { name: "AI 接入", exact: true });
  await expect(link).toBeVisible();
  await page.evaluate(() => navigator.serviceWorker.ready.then(() => true));
  await page.reload();
  await expect(link).toBeVisible();
  expect(await page.evaluate(() => !!navigator.serviceWorker.controller)).toBe(true);
  const asset = /\/assets\/AIAccessPage-[^/]+\.js$/;
  const cachePaths = () => page.evaluate(async () => {
    const paths: string[] = [];
    for (const name of await caches.keys()) {
      for (const request of await (await caches.open(name)).keys()) paths.push(new URL(request.url).pathname);
    }
    return paths;
  });
  expect((await cachePaths()).some(path => asset.test(path))).toBe(false);
  await context.setOffline(true);
  const warning = page.getByText("网络已断开。", { exact: false });
  await expect(warning).toBeVisible();
  await link.click();
  await expect(page.getByText("页面文件未能载入。", { exact: false })).toBeVisible();
  await expect(warning).toBeVisible();
  const grants = page.waitForResponse(response => new URL(response.url()).pathname === "/api/v1/ai/delegations");
  await context.setOffline(false);
  expect((await grants).status()).toBe(200);
  await expect(page.getByRole("button", { name: "添加 AI 接入" })).toBeEnabled();
  await expect.poll(async () => (await cachePaths()).some(path => asset.test(path))).toBe(true);
  await page.goto("/settings");
  await expect(link).toBeVisible();
  await context.setOffline(true);
  const cached = page.waitForResponse(response => asset.test(response.url()));
  await link.click();
  expect((await cached).fromServiceWorker()).toBe(true);
  await expect(page.getByRole("heading", { name: "AI 接入", exact: true })).toBeVisible();
  await expect(warning).toBeVisible();
  expect((await cachePaths()).filter(path => path.startsWith("/api/"))).toEqual([]);
  await page.goBack();
  await expect(link).toBeFocused();
});
