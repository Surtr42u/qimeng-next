import { expect, test } from "@playwright/test";
import { createHash } from "node:crypto";
import { mockLibrary } from "./fixture";

test("sending selected native data retries the same request and reads the device receipt", async ({ page }) => {
  await mockLibrary(page);
  const digest = "a".repeat(64);
  const task = { id: "synthetic-task", targetDeviceId: "phone", protocolVersion: 2, revision: 1,
    state: "pending", segments: [{ kind: "nativeSelection", packageId: digest, sha256: digest, bytes: 512 }],
    createdAt: "2026-01-01T00:00:00Z", expiresAt: "2026-01-02T00:00:00Z" };
  const keys: string[] = [];
  const legacyCalls: string[] = [];
  page.on("request", request => { if (request.url().includes("/migration/v2/")) legacyCalls.push(request.url()); });
  await page.route("**/api/v1/author-lists?**", route => route.fulfill({ json: { items: [
    { id: "list-one", name: "作者 01.txt", revision: 4 }, { id: "list-two", name: "作者 02.txt", revision: 7 },
  ] } }));
  await page.route("**/api/v1/device/native-deliveries", async route => {
    keys.push(route.request().headers()["idempotency-key"] ?? "");
    expect(route.request().postDataJSON()).toEqual({ targetDeviceId: "phone", targetInstanceId: "instance-two",
      items: [{ kind: "authorList", sourceObjectId: "list-two", expectedRevision: 7 }] });
    if (keys.length === 1) { await route.abort("connectionreset"); return; }
    await route.fulfill({ status: 201, json: task });
  });
  await page.route("**/api/v1/device/apply-tasks/synthetic-task", route => route.fulfill({
    json: { ...task, state: "applied", results: [{ kind: "nativeSelection", state: "applied", sha256: digest, receiptId: "local-plan" }] },
  }));
  await page.goto("/manage/backup");
  await page.getByText("发送资料到另一台设备", { exact: true }).click();
  await page.getByLabel("目标设备标识").fill("phone");
  await page.getByLabel("接收实例 ID").fill("instance-two");
  await page.getByLabel("选择来源作者 TXT").selectOption("list-two");
  await expect(page.getByRole("list", { name: "待发送资料" })).toContainText("作者 02.txt · 修订 7");
  const send = page.getByRole("button", { name: "发送所选资料" });
  await send.click();
  await expect(page.getByRole("alert")).toContainText("没有收到成功确认");
  await send.click();
  await expect(page.getByText("local-plan", { exact: true })).toBeVisible();
  expect(keys).toHaveLength(2);
  expect(keys[0]).toBeTruthy();
  expect(keys[1]).toBe(keys[0]);
  expect(legacyCalls).toEqual([]);
});

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
  await page.evaluate(async () => {
    const root = await navigator.storage.getDirectory();
    const output = await (await root.getFileHandle(".绮梦手机轮换.json", { create: true })).createWritable();
    await output.write("synthetic-mobile-rotation"); await output.close();
  });
  revision = "2";
  await page.getByRole("button", { name: "更新全部归档" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "请先在手机完成" })).toBeVisible();
  expect(created).toBe(1);
  expect(await text("当前", "备份", "影库备份.qnext")).toBe(archive("1"));
  await page.evaluate(async () => (await navigator.storage.getDirectory()).removeEntry(".绮梦手机轮换.json"));
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
