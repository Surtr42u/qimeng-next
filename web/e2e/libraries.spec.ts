import { test, expect } from "@playwright/test";
import type { components } from "../src/api/generated/schema";
import { mockLibrary } from "./fixture";

test("root registration keeps successful registration when scan acknowledgement fails", async ({ page }, testInfo) => {
  await mockLibrary(page);
  type Library = components["schemas"]["Library"];
  type Scan = components["schemas"]["ScanJob"];
  const libraries: Library[] = [];
  const createKeys: string[] = [];
  const scanKeys: string[] = [];
  let resolutionCount = 0;
  const scan: Scan = { id: "synthetic-scan", libraryId: "registered-library", state: "queued", processed: 0, added: 0, moved: 0, changed: 0, skipped: 0, issueCount: 0, issues: [], createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-01T00:00:00Z" };
  await page.route("**/api/v1/storages/resolve-library-root", async route => {
    resolutionCount++;
    expect(route.request().postDataJSON()).toEqual({ rootPath: "/synthetic/media" });
    await route.fulfill({ json: { storageId: "synthetic-root", rootRelativePath: "media", rootPath: "/synthetic/media" } });
  });
  await page.route(/\/api\/v1\/libraries$/, async route => {
    if (route.request().method() === "GET") return route.fulfill({ json: { items: libraries } });
    createKeys.push(route.request().headers()["idempotency-key"]!);
    const body = route.request().postDataJSON();
    expect(body).toEqual({ name: "合成登记库", kind: "regular", storageId: "synthetic-root", rootRelativePath: "media" });
    if (!libraries.length) libraries.push({ ...body, id: "registered-library", enabled: true, revision: 1, createdAt: "2026-01-01T00:00:00Z" });
    if (createKeys.length === 1) return route.abort("connectionreset");
    return route.fulfill({ status: 201, json: libraries[0] });
  });
  await page.route("**/api/v1/libraries/registered-library/location", route => route.fulfill({ json: { storageId: "synthetic-root", rootRelativePath: "media", rootPath: "/synthetic/media" } }));
  await page.route("**/api/v1/scans?**", route => route.fulfill({ json: { items: scanKeys.length > 1 ? [scan] : [] } }));
  await page.route("**/api/v1/libraries/registered-library/scans", async route => {
    scanKeys.push(route.request().headers()["idempotency-key"]!);
    if (scanKeys.length === 1) return route.abort("connectionreset");
    return route.fulfill({ status: 202, json: scan });
  });
  await page.goto("/manage/libraries");
  await page.getByLabel("库名称", { exact: true }).fill("合成登记库");
  await page.getByLabel("服务器根路径", { exact: true }).fill("/synthetic/media");
  await page.getByRole("button", { name: "注册并扫描", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("没有收到成功确认");
  await expect(page.getByLabel("服务器根路径", { exact: true })).toHaveValue("/synthetic/media");
  await page.getByRole("button", { name: "注册并扫描", exact: true }).click();
  await expect(page.getByRole("heading", { name: "合成登记库", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "重试扫描", exact: true })).toBeVisible();
  await expect(page.getByLabel("服务器根路径", { exact: true })).toHaveValue("");
  expect(createKeys[0]).toBe(createKeys[1]);
  expect(resolutionCount).toBe(1);
  await page.getByRole("button", { name: "重试扫描", exact: true }).click();
  await expect(page.getByRole("button", { name: "重试扫描", exact: true })).toHaveCount(0);
  expect(scanKeys[0]).toBe(scanKeys[1]);
  expect(libraries).toHaveLength(1);
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 800 });
    await page.screenshot({ path: testInfo.outputPath(`parity-libraries-${width}.png`), fullPage: true });
  }
});
