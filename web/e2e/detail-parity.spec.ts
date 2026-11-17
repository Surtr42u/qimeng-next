import { test, expect } from "@playwright/test";
import { mockLibrary } from "./fixture";

test("detail keeps the original two-column frame and image navigation semantics", async ({ page }, testInfo) => {
  await mockLibrary(page);
  await page.goto("/album");
  await expect(page.locator(".media-card")).toHaveCount(48);
  await page.locator(".media-card").nth(1).click();
  await expect(page.locator(".asset-pager__count")).toHaveText("2 / 48");
  await expect(page.locator(".detail-title")).toHaveText("合成场景 1.jpg");
  await expect(page.locator(".detail-main > .asset-stage")).toHaveCount(1);
  await expect(page.locator(".detail-side .author-card")).toContainText("合成作者");
  await expect(page.locator(".detail-side .upnext-card h3")).toHaveText("接下来播放");
  await expect(page.getByRole("button", { name: "返回浏览" })).toHaveCount(0);
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 800 });
    await page.screenshot({ path: testInfo.outputPath(`parity-detail-${width}.png`), fullPage: false });
  }
  await page.getByRole("button", { name: "查看原图并缩放" }).click();
  const dialog = page.getByRole("dialog", { name: "图片查看器" });
  await expect(dialog).toBeVisible();
  await expect(page.locator("#root")).toHaveJSProperty("inert", true);
  await page.keyboard.press("ArrowRight");
  await expect(dialog.locator(".img-viewer__counter")).toHaveText("3 / 48");
  await expect(dialog.locator(".img-viewer__img")).toHaveAttribute("src", /synthetic-2\/content/);
  await page.keyboard.press("ArrowLeft");
  await expect(dialog.locator(".img-viewer__counter")).toHaveText("2 / 48");
  await expect(dialog.locator(".img-viewer__img")).toHaveAttribute("src", /synthetic-1\/content/);
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(page.locator("#root")).toHaveJSProperty("inert", false);
  await expect(page.getByRole("button", { name: "查看原图并缩放" })).toBeFocused();
});

test("mixed media retains image-viewer intent without keeping an old player or image layer", async ({ page }) => {
  const fixture = await mockLibrary(page);
  fixture.assets[1]!.mediaType = "video";
  fixture.assets[1]!.mimeType = "video/mp4";
  await page.goto("/album");
  await page.locator(".media-card").first().click();
  await page.getByRole("button", { name: "查看原图并缩放" }).click();
  await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("dialog", { name: "图片查看器" })).toHaveCount(0);
  await expect(page.locator(".art-video-player")).toHaveCount(1);
  await page.getByRole("button", { name: "下一件", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "图片查看器" })).toBeVisible();
  await expect(page.locator(".art-video-player")).toHaveCount(0);
});
