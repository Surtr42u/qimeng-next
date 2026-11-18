import { expect, test } from "@playwright/test";
import { mockLibrary } from "./fixture";

test("old monitoring cards use actual samples, typed diagnostics, paged mounts and recorded tasks", async ({ page }, info) => {
  await mockLibrary(page);
  let sample = 0, cancelled = false, rejectReads = false;
  const reports: unknown[] = [], jobStates: (string | null)[] = [];
  const mount = { id: "one", label: "合成资料目录", mount: "/synthetic/one", available: true, usedBytes: 256, totalBytes: 1024, availableBytes: 768 };
  const date = "2026-01-01T00:00:00Z";
  await page.route("**/api/v1/system/status", route => {
    sample++;
    return route.fulfill({ json: { instanceId: "synthetic-instance", version: "cloud", goVersion: "go", startedAt: date, sampledAt: new Date(Date.parse(date) + sample * 2000).toISOString(), uptimeSeconds: sample * 2, receivedBytes: 2048 * sample, sentBytes: 256 * sample, cpuPercent: 12,
      memoryUsedBytes: 100, memoryTotalBytes: 400, mounts: [mount], nextMountCursor: "remaining", clientErrorCount: 2, trashCount: 7 } });
  });
  await page.route("**/api/v1/system/storage?**", route => { expect(new URL(route.request().url()).searchParams.get("cursor")).toBe("remaining"); return route.fulfill({ json: { items: [{ id: "two", mount: "/synthetic/two", label: "合成离线目录", available: false }] } }); });
  await page.route("**/api/v1/maintenance/jobs?**", route => {
    const state = new URL(route.request().url()).searchParams.get("state"); jobStates.push(state);
    return route.fulfill({ json: { items: [{ id: "job-one", kind: "scan", state: cancelled ? "cancelled" : "running", rawState: cancelled ? "cancelled" : "running", label: "合成检查", createdAt: date, cancellable: !cancelled }] } });
  });
  await page.route("**/api/v1/scans/job-one", route => {
    if (route.request().method() === "DELETE") { expect(route.request().headers()["idempotency-key"]).toBeTruthy(); cancelled = true; }
    return route.fulfill({ json: { id: "job-one", libraryId: "synthetic-library", state: cancelled ? "cancelled" : "running", processed: 19, added: 3, moved: 0, changed: 0, skipped: 0, issueCount: 0, issues: [], createdAt: date, updatedAt: date } });
  });
  await page.route(/\/api\/v1\/system\/client-errors(?:\?.*)?$/, route => {
    if (route.request().method() === "POST") { reports.push(route.request().postDataJSON()); return route.fulfill({ status: 503, json: { code: "unavailable", message: "合成错误" } }); }
    if (rejectReads) return route.fulfill({ status: 503, json: {} });
    const next = new URL(route.request().url()).searchParams.get("cursor");
    return route.fulfill({ json: { items: [{ id: next ? "old" : "new", occurredAt: date, level: "error", code: "request_failed", message: next ? "较早的错误" : "最新错误", screen: "search", httpStatus: 503 }], nextCursor: next ? undefined : "older" } });
  });
  await page.goto("/maintenance?private=synthetic-secret");
  await expect(page.getByRole("heading", { name: "服务与维护" })).toBeVisible();
  await expect(page.getByRole("region", { name: "接收 / 下行" }).locator(".monitor-value")).toHaveText("1 KB/s", { timeout: 10000 });
  await expect(page.getByRole("link", { name: /回收站.*7 条待处理/ })).toBeVisible();
  await expect(page.getByRole("link", { name: /文件管理/ })).toHaveAttribute("href", "/manage/files");
  await page.getByRole("combobox", { name: "按状态筛选任务" }).click();
  await page.getByRole("option", { name: "运行中" }).click();
  await expect.poll(() => jobStates).toContain("running");
  await page.getByRole("button", { name: "查看", exact: true }).click();
  await expect(page.getByLabel("维护任务详情").getByText("19")).toBeVisible();
  await page.getByRole("button", { name: "取消任务", exact: true }).click();
  expect(cancelled).toBe(false);
  await page.getByRole("button", { name: "确认取消任务" }).click();
  await expect.poll(() => cancelled).toBe(true);
  await page.evaluate(() => window.dispatchEvent(new ErrorEvent("error", { error: new Error("synthetic-private-file"), message: "synthetic-private-query" })));
  await expect.poll(() => reports).toEqual([{ level: "error", code: "render_failed", screen: "maintenance" }]);
  await page.screenshot({ path: info.outputPath("parity-maintenance.png"), fullPage: true });
  await page.getByRole("link", { name: /查看存储明细/ }).click();
  await expect(page.getByRole("heading", { name: "已读取目录容量合计" })).toBeVisible();
  await page.getByRole("button", { name: "更多监控目录" }).click();
  await expect(page.getByRole("region", { name: "合成离线目录" }).getByText("采集不可用")).toBeVisible();
  await page.getByRole("link", { name: "服务与维护", exact: true }).click();
  await page.getByRole("link", { name: /客户端异常/ }).click();
  await expect(page.getByText("全部 2 条")).toBeVisible();
  await expect(page.locator(".log-table tbody tr").first()).toContainText("最新错误");
  await expect(page.locator(".log-table tbody tr").last()).toContainText("较早的错误");
  rejectReads = true; await page.reload();
  await expect(page.getByRole("button", { name: "重试日志" })).toBeVisible();
  expect(reports).toHaveLength(1);
}
