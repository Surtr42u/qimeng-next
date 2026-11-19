import { expect, test } from "@playwright/test";
import type { Page, Route } from "@playwright/test";
import { mockLibrary } from "./fixture";

async function holdScript(page: Page, file: RegExp) {
  let capture!: (route: Route) => void;
  const requested = new Promise<Route>(resolve => { capture = resolve; });
  await page.route(file, route => { capture(route); }, { times: 1 });
  return { requested };
}

async function profileData(page: Page) {
  await page.route("**/api/v1/authors/synthetic-author/profile", route => route.fulfill({ json: {
    authorId: "synthetic-author", editable: true, blocks: [
      { block: "x", revision: 1, values: { biographyOriginal: "合成作者原文" }, evidence: [] },
      { block: "basic", revision: 1, values: { summary: "已读取的合成资料" }, evidence: [] },
      { block: "accounts", revision: 1, values: {}, evidence: [] },
    ],
  } }));
  await page.route("**/api/v1/authors/synthetic-author/profile/candidates?**", route => route.fulfill({ json: { items: [] } }));
}

test("a signed-out dossier deep link opens login before any protected data or record script", async ({ page }) => {
  const library = await mockLibrary(page);
  await profileData(page);
  let signedIn = false;
  await page.route("**/api/v1/sessions/current", route => signedIn ? route.fallback() :
    route.fulfill({ status: 401, json: { code: "unauthenticated", message: "需要登录" } }));
  await page.route("**/api/v1/sessions", route => {
    signedIn = true;
    return route.fulfill({ status: 201, json: {} });
  });
  const scripts: string[] = [];
  const dossier = /\/assets\/PendingProfileCandidates-[^/]+\.js$/;
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
  await expect(page.locator(".creator-bio-preview")).toHaveText("已读取的合成资料");
  await request.continue();
  await expect(page.getByRole("heading", { name: "作者资料", exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/authors\/synthetic-author\/profile$/);
});

test("a delayed AI record list leaves the new page usable and preserves return focus", async ({ page }) => {
  await mockLibrary(page);
  await page.route("**/api/v1/ai/delegations**", route => route.fulfill({ json: { items: [] } }));
  const asset = /\/assets\/AIDelegationRecords-[^/]+\.js$/;
  const requests: string[] = [];
  page.on("request", request => { if (asset.test(request.url())) requests.push(request.url()); });
  const held = await holdScript(page, asset);
  await page.goto("/settings");
  const link = page.getByRole("link", { name: "AI 接入", exact: true });
  await link.focus();
  expect(requests).toHaveLength(0);
  await link.press("Enter");
  const request = await held.requested;
  const heading = page.getByRole("heading", { name: "AI 接入", exact: true });
  await expect(heading).toBeFocused();
  const filter = page.getByLabel("接入状态");
  await filter.focus();
  await request.continue();
  await expect(page.getByText("尚无有效 AI 接入", { exact: true })).toBeVisible();
  await expect(filter).toBeFocused();
  await page.goBack();
  await expect(link).toBeFocused();
  await link.press("Enter");
  await expect(heading).toBeFocused();
  expect(requests).toHaveLength(1);
});

test("leaving during a delayed import keeps the newer page and its keyboard focus", async ({ page }) => {
  await mockLibrary(page);
  const held = await holdScript(page, /\/assets\/AIDelegationRecords-[^/]+\.js$/);
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

test("the first offline dossier preserves the author's cached fields and draft, and archives keep both return links", async ({ page, context }) => {
  await mockLibrary(page);
  await profileData(page);
  await page.goto("/authors/synthetic-author");
  await expect(page.locator(".creator-profile-bio")).toHaveText("合成作者原文");
  await context.setOffline(true);
  await page.getByRole("link", { name: "详细资料", exact: true }).click();
  await expect(page.locator(".creator-bio-preview")).toHaveText("已读取的合成资料");
  await page.getByRole("button", { name: "编辑基础资料", exact: true }).click();
  const draft = page.getByRole("textbox", { name: "整理简介", exact: true });
  await draft.fill("合成离线草稿");
  await context.setOffline(false);
  await expect(draft).toHaveValue("合成离线草稿");
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await page.getByRole("button", { name: "放弃修改", exact: true }).click();
  await context.setOffline(true);
  await page.getByText("身份管理与历史归档", { exact: true }).click();
  await page.getByRole("link", { name: "历史归档", exact: true }).click();
  await expect(page.getByRole("heading", { name: "作者资料归档", exact: true })).toBeFocused();
  await expect(page.getByRole("link", { name: "作者列表", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "查看当前作者", exact: true })).toBeVisible();
  await context.setOffline(false);
  await page.getByRole("link", { name: "查看当前作者", exact: true }).click();
  await expect(page.locator(".creator-profile-bio")).toHaveText("合成作者原文");
});
