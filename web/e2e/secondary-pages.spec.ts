import { expect, test } from "@playwright/test";
import type { Page, Route } from "@playwright/test";
import { mockLibrary } from "./fixture";

async function holdScript(page: Page, file: RegExp) {
  let capture!: (route: Route) => void;
  const requested = new Promise<Route>(resolve => { capture = resolve; });
  await page.route(file, route => { capture(route); }, { times: 1 });
  return { requested };
}

test("a signed-out dossier deep link opens login without fetching its script or protected data", async ({ page }) => {
  const library = await mockLibrary(page);
  let signedIn = false;
  await page.route("**/api/v1/sessions/current", route => signedIn ? route.fallback() :
    route.fulfill({ status: 401, json: { code: "unauthenticated", message: "需要登录" } }));
  await page.route("**/api/v1/sessions", route => {
    signedIn = true;
    return route.fulfill({ status: 201, json: {} });
  });
  const scripts: string[] = [];
  const dossier = /\/assets\/ProfileDetails-[^/]+\.js$/;
  page.on("request", request => { if (dossier.test(request.url())) scripts.push(request.url()); });
  const held = await holdScript(page, dossier);
  await page.goto("/authors/synthetic-author/profile");
  await expect(page.getByLabel("管理密码")).toBeFocused();
  expect(scripts).toHaveLength(0);
  expect(library.requests.some(path => path.includes("/authors"))).toBe(false);
  await page.getByLabel("管理密码").fill("synthetic-only");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const request = await held.requested;
  await expect(page.getByLabel("管理密码")).toHaveCount(0);
  expect(library.requests.some(path => path.includes("/authors"))).toBe(false);
  await request.continue();
  await expect(page.getByRole("heading", { name: "作者资料", exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/authors\/synthetic-author\/profile$/);
});

test("a delayed settings page preserves the source control, return focus and repeat visits", async ({ page }) => {
  await mockLibrary(page);
  await page.route("**/api/v1/ai/delegations**", route => route.fulfill({ json: { items: [] } }));
  const asset = /\/assets\/AIAccessPage-[^/]+\.js$/;
  const requests: string[] = [];
  page.on("request", request => { if (asset.test(request.url())) requests.push(request.url()); });
  const held = await holdScript(page, asset);
  await page.goto("/settings");
  const link = page.getByRole("link", { name: "AI 接入", exact: true });
  await link.focus();
  expect(requests).toHaveLength(0);
  await link.press("Enter");
  const request = await held.requested;
  await expect(page.getByRole("heading", { name: "设置", exact: true })).toBeVisible();
  await expect(link).toBeFocused();
  await request.continue();
  const heading = page.getByRole("heading", { name: "AI 接入", exact: true });
  await expect(heading).toBeFocused();
  await page.goBack();
  await expect(link).toBeFocused();
  await link.press("Enter");
  await expect(heading).toBeFocused();
  expect(requests).toHaveLength(1);
});

test("leaving during a delayed import keeps the newer page and its keyboard focus", async ({ page }) => {
  await mockLibrary(page);
  const held = await holdScript(page, /\/assets\/AIAccessPage-[^/]+\.js$/);
  await page.goto("/settings");
  await page.getByRole("link", { name: "AI 接入", exact: true }).click();
  const request = await held.requested;
  await page.getByRole("link", { name: "相册", exact: true }).click();
  const album = page.getByRole("heading", { name: "相册", exact: true });
  await expect(album).toBeVisible();
  await request.continue();
  await page.waitForLoadState("networkidle");
  await expect(page).toHaveURL(/\/album$/);
  await expect(album).toBeFocused();
  await expect(page.getByRole("button", { name: "添加 AI 接入" })).toHaveCount(0);
});
