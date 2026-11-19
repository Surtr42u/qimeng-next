import { test, expect } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { components } from "../src/api/generated/schema";

test.use({ screenshot: "off", trace: "off", video: "off" });

test("browser grants single-frame research only after consent within the selected scope", async ({ page, request }) => {
  expect([process.env.CI, process.env.QIMENG_E2E_BASE_URL]).toEqual(["true", "http://127.0.0.1:18481"]);
  const token = process.env.QIMENG_E2E_TOKEN!;
  const label = "浏览器单帧接入 " + randomUUID().slice(0, 8);
  const library = await request.post("/api/v1/libraries", {
    headers: { Authorization: "Bearer " + token, "Idempotency-Key": randomUUID() },
    data: { name: label, kind: "regular" },
  });
  expect(library.ok()).toBe(true);
  const libraryId: string = (await library.json()).id;
  await page.goto("/");
  await page.getByLabel("管理密码", { exact: true }).fill(token);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "为你推荐" })).toBeVisible();

  await page.goto("/settings/ai");
  await page.getByRole("button", { name: "添加 AI 接入" }).click();
  await page.getByLabel("接入名称").fill(label);
  await page.getByRole("checkbox", { name: "研究未知身份与来源" }).check();
  const next = page.getByRole("button", { name: "下一步", exact: true });
  await next.click();
  const externalFrame = page.getByRole("checkbox", { name: "允许单帧识图" });
  const libraryOption = page.getByRole("checkbox", { name: label, exact: true });
  const existingMedia = page.getByRole("checkbox", { name: "研究已入库作品的媒体预览" });
  await expect(externalFrame).toBeDisabled();
  await libraryOption.check();
  await expect(externalFrame).toBeDisabled();
  await existingMedia.check();
  await expect(externalFrame).toBeEnabled();
  await expect(externalFrame).not.toBeChecked();
  await externalFrame.check();
  const budget = page.getByLabel("每任务研究调用上限");
  await budget.fill("0");
  await expect(externalFrame).toBeDisabled();
  await expect(externalFrame).not.toBeChecked();
  await budget.fill("3");
  await expect(externalFrame).toBeEnabled();
  await expect(externalFrame).not.toBeChecked();
  await externalFrame.check();
  await libraryOption.uncheck();
  await expect(externalFrame).toBeDisabled();
  await expect(externalFrame).not.toBeChecked();
  await libraryOption.check();
  await existingMedia.check();
  await expect(externalFrame).not.toBeChecked();
  await externalFrame.check();
  await next.click();
  const permissionText = "允许将派生的单帧预览发送至已配置的识图服务";
  await expect(page.getByText(permissionText, { exact: true })).toBeVisible();

  let grantId: string | undefined;
  try {
    const created = page.waitForResponse(response => response.request().method() === "POST" &&
      new URL(response.url()).pathname === "/api/v1/ai/delegations");
    await page.getByRole("button", { name: "建立此范围的接入" }).click();
    const response = await created;
    expect(response.status()).toBe(201);
    const { delegation } = (await response.json()) as { delegation: components["schemas"]["AIDelegation"] };
    grantId = delegation.id;
    await page.getByRole("button", { name: "已保存到执行器，清除本页凭据" }).click();
    await page.reload();
    const grant = page.getByRole("article").filter({ has: page.getByRole("heading", { name: label, exact: true }) });
    await grant.getByText("权限详情", { exact: true }).click();
    await expect(grant.getByText(permissionText, { exact: true })).toBeVisible();
    const readback = await page.request.get("/api/v1/ai/delegations?state=active");
    expect(readback.ok()).toBe(true);
    const records: components["schemas"]["AIDelegation"][] = (await readback.json()).items;
    expect(records.find(record => record.id === grantId)).toMatchObject({
      canLookupImages: true, canResearch: true, canResearchAssets: true, canIngest: false,
      canOrganize: false, libraryIds: [libraryId], maxResearchCalls: 3,
    });
  } finally {
    if (grantId) await page.request.delete("/api/v1/ai/delegations/" + grantId);
  }
});
