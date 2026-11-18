import { expect, test } from "@playwright/test";
import { createHash } from "node:crypto";
import { mockLibrary } from "./fixture";

test("maintenance folder freezes real browser files, resumes interrupted rotation and preserves an external edit", async ({ page, browserName }, testInfo) => {
  test.skip(browserName !== "chromium", "Uses the Chromium file-system writable API with a synthetic OPFS root.");
  await mockLibrary(page);
  await page.addInitScript(() => {
    Object.defineProperty(window, "showDirectoryPicker", { configurable: true, value: () => navigator.storage.getDirectory() });
  });
  let revision = "1";
  let created = 0;
  let failNextDownload = false;
  const archive = (rev: string) => "synthetic-qnext-" + rev;
  const hash = (rev: string) => createHash("sha256").update(archive(rev)).digest("hex");
  const backup = (rev: string, ready = false) => ({ id: "backup-" + rev, businessRevision: rev, state: "awaiting_external_backup",
    assetCount: 1, createdAt: "2026-01-01T00:00:00Z", snapshotHash: hash(rev), manifestHash: hash(rev),
    exportState: ready ? "completed" : "not_started", archiveHash: ready ? hash(rev) : undefined });
  await page.route(/\/api\/v1\/backups(?:\?.*)?$/, route => {
    if (route.request().method() === "GET") return route.fulfill({ json: { items: [] } });
    created++; return route.fulfill({ status: 201, json: backup(revision) });
  });
  await page.route("**/api/v1/backups/change-state", route => route.fulfill({ json: { instanceId: "synthetic-instance", businessRevision: revision } }));
  await page.route("**/api/v1/author-lists/master", route => route.fulfill({ contentType: "text/plain", body: "作者总表 " + revision }));
  await page.route(/\/api\/v1\/backups\/backup-(\d+)(?:\/(export|archive|author-documents))?$/, route => {
    const [, rev, part] = new URL(route.request().url()).pathname.match(/backup-(\d+)(?:\/(.*))?$/)!;
    if (part === "author-documents") return route.fulfill({ contentType: "text/plain", body: "# 合成作者资料 " + rev });
    if (part === "archive") {
      if (failNextDownload) { failNextDownload = false; return route.abort("connectionreset"); }
      return route.fulfill({ contentType: "application/octet-stream", body: archive(rev!) });
    }
    return route.fulfill({ json: backup(rev!, part === "export") });
  });
  await page.route("**/api/v1/migration/v2/imports?**", route => route.fulfill({ json: { items: [] } }));
  await page.goto("/manage/backup");
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 800 });
    await page.screenshot({ path: testInfo.outputPath("parity-backup-" + width + ".png"), fullPage: true });
  }
  await page.getByRole("button", { name: "选择根目录并生成资料" }).click();
  const text = (slot: string, category: string, name: string) => page.evaluate(async ([slot, category, name]) => {
    const root = await navigator.storage.getDirectory();
    const dir = await (await root.getDirectoryHandle(slot!)).getDirectoryHandle(category!);
    return (await (await dir.getFileHandle(name!)).getFile()).text();
  }, [slot, category, name]);
  await expect(page.getByText("当前备份与作者资料已更新。", { exact: true })).toBeVisible();
  expect(await text("当前", "作者资料", "作者总表.txt")).toBe("作者总表 1");
  await page.getByRole("button", { name: "更新全部归档" }).click();
  await expect(page.getByText("当前资料已是最新一代。", { exact: true })).toBeVisible();
  expect(created).toBe(1);
  revision = "2"; failNextDownload = true;
  await page.getByRole("button", { name: "更新全部归档" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "没有收到成功确认" })).toBeVisible();
  expect(await text("上次", "备份", "影库备份.qnext")).toBe(archive("1"));
  await page.getByRole("button", { name: "更新全部归档" }).click();
  await expect.poll(() => text("当前", "作者资料", "作者资料.md")).toBe("# 合成作者资料 2");
  await expect(page.getByRole("button", { name: "更新全部归档" })).toBeEnabled();
  expect(await text("当前", "备份", "影库备份.qnext.sha256")).toBe(hash("2") + "\n");
  expect(await text("上次", "作者资料", "作者总表.txt")).toBe("作者总表 1");
  expect(created).toBe(2);
  await page.evaluate(async () => {
    const root = await navigator.storage.getDirectory();
    const dir = await (await root.getDirectoryHandle("上次")).getDirectoryHandle("作者资料");
    const output = await (await dir.getFileHandle("作者总表.txt")).createWritable();
    await output.write("合成手工修改"); await output.close();
  });
  revision = "3";
  await page.getByRole("button", { name: "更新全部归档" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "外部修改" })).toBeVisible();
  expect(await text("当前", "备份", "影库备份.qnext")).toBe(archive("2"));
  expect(await text("上次", "作者资料", "作者总表.txt")).toBe("合成手工修改");
});
