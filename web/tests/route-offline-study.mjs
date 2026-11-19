import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { chromium, expect } from "@playwright/test";
import { fixedTime, makeInventory } from "./ui-reference-data.mjs";
import { serve } from "./ui-reference-server.mjs";

const browser = await chromium.launch({ args: ["--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1"] });
const out = process.env.COMPARISON_DIR;
const author = "/authors/20000000-0000-4000-8000-000000000001", profile = author + "/profile";
const records = [];
await mkdir(out, { recursive: true });
try {
  const inventory = await makeInventory(browser);
  for (const [version, dist] of Object.entries({ baseline: process.env.BASELINE_DIST, firstProposal: process.env.FIRST_PROPOSAL_DIST, candidate: process.env.CANDIDATE_DIST })) {
    const server = await serve("candidate", dist, inventory);
    try {
      for (const scene of ["profile", "archives", "ai", "offline-from-home"]) {
        const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, serviceWorkers: "block" });
        try {
          const page = await context.newPage();
          await page.clock.setFixedTime(new Date(fixedTime));
          if (scene === "profile") {
            await page.goto(server.origin + author);
            await expect(page.locator(".creator-profile-bio")).toHaveText("合成作者原文");
          } else if (scene === "archives") {
            await page.goto(server.origin + profile);
            await expect(page.locator(".creator-bio-preview")).toHaveText("合成资料");
            await page.getByText("身份管理与历史归档", { exact: true }).click();
          } else if (scene === "ai") {
            await page.goto(server.origin + "/settings");
            await expect(page.getByRole("link", { name: "AI 接入", exact: true })).toBeVisible();
          } else {
            await page.goto(server.origin + "/");
            await expect(page.locator("#page-home .card")).toHaveCount(10);
          }
          await context.setOffline(true);
          if (scene === "offline-from-home") await page.getByRole("link", { name: "设置", exact: true }).click();
          const linkText = scene === "profile" ? "详细资料" : scene === "archives" ? "历史归档" : "AI 接入";
          const link = page.getByRole("link", { name: linkText, exact: true });
          await link.focus();
          await link.press("Enter");
          const failed = page.getByText("页面文件未能载入。", { exact: false });
          const heading = page.getByRole("heading", { name: scene === "profile" ? "作者资料" : scene === "archives" ? "作者资料归档" : "AI 接入", exact: true });
          await expect(heading.or(failed)).toBeVisible();
          const record = { version, scene, heading: await heading.isVisible(), warning: await page.getByText("网络已断开。", { exact: false }).isVisible() };
          let draft;
          if (scene === "profile") {
            record.summary = await page.locator(".creator-bio-preview").textContent({ timeout: 600 }).catch(() => "") === "合成资料";
            const edit = page.getByRole("button", { name: "编辑基础资料", exact: true });
            record.canEdit = await edit.isVisible() && await edit.isEnabled();
            if (record.canEdit) { await edit.click(); draft = page.getByRole("textbox", { name: "整理简介", exact: true }); }
          } else if (scene === "archives") {
            record.authorList = await page.getByRole("link", { name: "作者列表" }).isVisible();
            record.currentAuthor = await page.getByRole("link", { name: "查看当前作者" }).isVisible();
          } else {
            const add = page.getByRole("button", { name: "添加 AI 接入" });
            record.canAdd = await add.isVisible() && await add.isEnabled();
            record.canFilter = await page.getByLabel("接入状态").isVisible();
            if (record.canAdd) { await add.click(); draft = page.getByRole("textbox", { name: "接入名称" }); }
          }
          if (draft) await draft.fill("合成离线草稿");
          record.draftOffline = draft ? await draft.inputValue() === "合成离线草稿" : false;
          await context.setOffline(false);
          await expect(heading).toBeVisible();
          record.draftAfterReconnect = draft ? await draft.inputValue() === "合成离线草稿" : false;
          if (draft) {
            await page.goBack();
            const guard = page.getByRole("alertdialog");
            await expect(guard.or(link)).toBeVisible();
            record.guarded = await guard.isVisible();
            if (record.guarded) await guard.getByRole("button", { name: "放弃并离开" }).click();
          } else await page.goBack();
          await expect(link).toBeVisible();
          record.returnFocus = await link.evaluate(e => e === document.activeElement);
          records.push(record);
          console.log(JSON.stringify(record));
        } finally { await context.close(); }
      }
    } finally { await server.close(); }
  }
} finally {
  await browser.close();
  await writeFile(join(out, "offline-comparison.json"), JSON.stringify(records, null, 2));
}
