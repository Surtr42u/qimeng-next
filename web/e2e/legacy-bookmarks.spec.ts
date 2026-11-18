import { expect, test } from "@playwright/test";
import { mockLibrary, syntheticAsset } from "./fixture";

test("existing rank and collection bookmarks preserve their single-board and single-tag views", async ({ page }, info) => {
  await mockLibrary(page);
  await page.route("**/api/v1/stats/top-tags?**", route => route.fulfill({ json: { items: [{ id:"tag-one", name:"合成标签", views:7, plays:2, seconds:15, assets:1, value:7 }] } }));
  let tag: string | null = null;
  await page.route(/\/api\/v1\/assets\?/, route => {
    tag = new URL(route.request().url()).searchParams.get("tag");
    return route.fulfill({ json: { items:[syntheticAsset(12)], totalMatched:1 } });
  });
  await page.goto("/app/ranks/tags?range=7d");
  await expect(page.locator("#page-ranks h2")).toHaveText("标签榜");
  await expect(page.locator("#page-ranks")).toContainText("前 50 项");
  await page.getByRole("button", {name:/合成标签/}).click();
  await expect(page.locator("#page-collection h2")).toHaveText("合成标签");
  await expect(page.locator("#page-collection")).toContainText("标签 · 1 个文件");
  expect(tag).toBe("合成标签");
  await expect(page.locator("#page-albums, #page-collection .filter-card")).toHaveCount(0);
  await page.screenshot({ path: info.outputPath("parity-tag-collection.png"), animations:"disabled" });
  await page.goto("/app/collection?tag="+encodeURIComponent("合成标签"));
  await expect(page.locator("#page-collection h2")).toHaveText("合成标签");
});
test("name-only creator bookmarks expose an ambiguous identity without choosing an author", async ({ page }) => {
  await mockLibrary(page);
  await page.route(/\/api\/v1\/authors\?/, route => {
    const url=new URL(route.request().url());
    expect(url.searchParams.get("exactName")).toBe("同名");
    expect(url.searchParams.get("limit")).toBe("2");
    return route.fulfill({ json:{items:[{id:"one",name:"同名"},{id:"two",name:"同名"}],totalMatched:2} });
  });
  await page.goto("/app/collection?author="+encodeURIComponent("同名"));
  await expect(page.getByRole("alert")).toContainText("存在同名作者");
});
