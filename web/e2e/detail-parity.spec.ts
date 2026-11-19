import { test, expect } from "@playwright/test";
import { mockLibrary } from "./fixture";

for (const width of [1440, 390]) {
  test(`the filename copies on one line without moving the detail at ${width}px`, async ({ page, browserName, context }, testInfo) => {
    const fixture = await mockLibrary(page);
    const asset = fixture.assets[1]!;
    if (width === 390) asset.fileName = "synthetic-original-".repeat(8) + "01.png";
    asset.cosWorkName = "合成作品";
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
    await page.emulateMedia({ reducedMotion: "reduce" });
    await page.goto("/album");
    await page.locator(".media-card").nth(1).click();
    const copy = page.getByTitle("复制完整文件名", { exact: true });
    await copy.scrollIntoViewIfNeeded();
    await page.locator(".asset-stage img").evaluate(node => (node as HTMLImageElement).decode());
    await page.evaluate(() => document.fonts.ready.then(() => true));
    const shape = await copy.evaluate(node => {
      const text = document.createRange();
      text.selectNodeContents(node.lastChild!);
      const label = text.getBoundingClientRect();
      const icon = node.querySelector("svg")!.getBoundingClientRect();
      const filename = document.createRange();
      filename.selectNodeContents(node.previousElementSibling!);
      return { gap: label.left - icon.right, middle: Math.abs((label.top + label.bottom - icon.top - icon.bottom) / 2),
        labelLines: text.getClientRects().length, filenameLines: filename.getClientRects().length };
    });
    expect(shape.gap).toBeGreaterThanOrEqual(3);
    expect(shape.middle).toBeLessThan(3);
    expect(shape.labelLines).toBe(1);
    if (width === 390) expect(shape.filenameLines).toBeGreaterThan(1);

    if (testInfo.project.name === "chromium") {
      const anchors = page.locator(".asset-stage, .asset-stage img, .asset-pager, .detail-title, .detail-title + div, .detail-title + div > span, .detail-meta, .detail-actions, .detail-side");
      const boxes = () => anchors.evaluateAll(nodes => nodes.map(node => {
        const { x, y, width, height } = node.getBoundingClientRect();
        return [x, y, width, height];
      }));
      const masked = { style: 'button[title="复制完整文件名"] { visibility: hidden !important; }', animations: "disabled" as const };
      await copy.evaluate(node => node.classList.remove("asset-file-copy"));
      const original = await boxes();
      const before = await page.screenshot(masked);
      await copy.evaluate(node => node.classList.add("asset-file-copy"));
      const current = await boxes();
      const drift = Math.max(...current.flatMap((rect, i) => rect.map((n, j) => Math.abs(n - original[i]![j]!))));
      expect(drift).toBeLessThan(0.05);
      expect((await page.screenshot(masked)).equals(before), "pixels outside the copy button").toBe(true);
      await page.screenshot({ path: testInfo.outputPath(`detail-copy-dark-${width}.png`), animations: "disabled" });
    }
    if (browserName === "chromium") {
      await context.grantPermissions(["clipboard-read", "clipboard-write"]);
      await copy.press("Enter");
      await expect(copy).toHaveText("已复制");
      expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(asset.fileName);
      await expect(copy).toHaveText("复制文件名");
    }
    await page.evaluate(() => {
      Object.defineProperty(navigator, "clipboard", {
        configurable: true, value: { writeText: async () => { throw new DOMException("Denied", "NotAllowedError"); } },
      });
    });
    await copy.press("Space");
    await expect(page.getByRole("alert").filter({ hasText: "复制文件名失败，请手动选择复制" })).toBeVisible();
    await page.evaluate(() => { Object.defineProperty(navigator, "clipboard", { value: undefined }); });
    await copy.press("Enter");
    await expect(page.getByRole("alert").filter({ hasText: "当前环境不支持剪贴板" })).toBeVisible();
  });
}

test("a cold detail route leaves its album visible while the packaged script downloads", async ({ page }) => {
  await mockLibrary(page);
  await page.goto("/album");
  const cards = page.locator("#page-albums .media-card");
  await expect(cards).toHaveCount(48);
  let release!: () => void;
  const gate = new Promise<void>(done => { release = done; });
  await page.route("**/assets/*.js", async route => { await gate; await route.continue(); });
  const downloading = page.waitForRequest(request => /^\/assets\/[^/]+\.js$/.test(new URL(request.url()).pathname));
  const opening = cards.nth(1).click();
  try {
    await downloading;
    await expect(cards.nth(1)).toBeVisible();
    await expect(page.locator(".asset-overlay")).toHaveCount(0);
  } finally {
    release();
    await opening;
  }
  await expect(page.locator(".asset-pager__count")).toHaveText("2 / 48");
  await expect(page.locator(".overlay-backdrop")).toHaveJSProperty("inert", true);
  await page.goBack();
  await expect(cards.nth(1)).toBeVisible();
  await expect(page.locator(".overlay-backdrop")).toHaveJSProperty("inert", false);
});

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
    await expect.poll(async () => {
      const stage = await page.locator(".asset-stage").boundingBox();
      const side = await page.locator(".detail-side").boundingBox();
      if (!stage || !side) return false;
      return width === 1440
        ? stage.x + stage.width < side.x
        : Math.abs(stage.x - side.x) < 1 && stage.width > side.width && side.y > stage.y + stage.height;
    }).toBe(true);
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
