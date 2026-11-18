import { expect, test } from "@playwright/test";
import { mockLibrary } from "./fixture";

test("upload keeps the original position, optional author and queue workflow", async ({ page }, testInfo) => {
  await mockLibrary(page);
  await page.goto("/manage/upload");
  const add = page.getByRole("button", { name: "选择文件并立即上传", exact: true });
  await expect(add).toBeDisabled();
  await page.getByRole("group", { name: "目标库" }).getByRole("button", { name: "合成媒体库" }).click();
  await expect(add).toBeEnabled();
  await expect(page.getByRole("region", { name: "上传任务" })).toContainText("暂无上传任务");
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 800 });
    const box = await page.locator(".upload-intake").boundingBox();
    expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
    await page.screenshot({ path: testInfo.outputPath("parity-upload-" + width + ".png"), fullPage: true });
  }
  await page.locator(".upload-metadata-card summary").click();
  const input = page.getByLabel("批次作者（可选，联想选择）");
  await input.fill("合成");
  await expect(add).toBeDisabled();
  await page.getByRole("group", { name: "作者联想" }).getByText("合成作者", { exact: true }).click();
  await page.getByRole("button", { name: "synthetic-forum", exact: true }).click();
  await page.locator(".upload-metadata-card summary").click();
  await expect(page.getByRole("group", { name: "本批上传摘要" })).toContainText("来源：synthetic-forum");
  await expect(add).toBeEnabled();
});
