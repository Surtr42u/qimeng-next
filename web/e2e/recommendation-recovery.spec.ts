import { test, expect } from "@playwright/test";
import { mockLibrary, syntheticAsset } from "./fixture";
test("an expired recommendation cursor keeps the current page and restarts from a fresh first page", async ({ page }) => {
  await mockLibrary(page);
  let screens = 0, expired = 0;
  await page.route("**/api/v1/recommendations?**", route => {
    const cursor = new URL(route.request().url()).searchParams.get("cursor");
    if (cursor === "expired") { expired++; return route.fulfill({ status: 409, json: { code: "recommendation_expired", message: "推荐顺序已更新，请从第一页继续。" } }); }
    if (cursor) return route.fulfill({ json: { items: [syntheticAsset(80)], snapshotId: "current" } });
    screens++;
    return route.fulfill({ json: { items: Array.from({ length: 32 }, (_, i) => syntheticAsset(i + (screens > 1 ? 40 : 0))), nextCursor: screens > 1 ? "next" : "expired", snapshotId: "synthetic-" + screens } });
  });
  await page.goto("/");
  await expect(page.locator(".media-card")).toHaveCount(32);
  // Scrolling the button into view already fires the sentinel in WebKit. A click
  // on the same element could run its new recovery action before the assertion.
  await page.locator("#main-content").evaluate(element => element.scrollTo({ top: element.scrollHeight, behavior: "instant" }));
  await expect(page.getByRole("alert")).toContainText("推荐顺序已更新");
  await expect(page.locator(".media-card")).toHaveCount(32);
  await page.getByRole("button", { name: "从第一页继续", exact: true }).click();
  await expect(page.getByRole("link", { name: "打开 合成场景 40.jpg", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "打开 合成场景 0.jpg", exact: true })).toHaveCount(0);
  expect(expired).toBe(1); expect(screens).toBe(2);
});
