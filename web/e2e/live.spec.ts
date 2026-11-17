import { test, expect } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { execFileSync } from "node:child_process";

test("same-commit service: login, upload, edit, recommend and play persist through reload", async ({
  page,
}, testInfo) => {
  test.setTimeout(120_000);
  // This suite is allowed to mutate only its cloud loopback fixture instance.
  expect(process.env.CI, "Live verification must run in cloud CI").toBe("true");
  expect(process.env.QIMENG_E2E_BASE_URL).toBe("http://127.0.0.1:18481");
  const token = process.env.QIMENG_E2E_TOKEN;
  expect(Boolean(token), "The disposable CI service token is required").toBe(
    true,
  );
  const directory = testInfo.outputPath("synthetic-inputs");
  mkdirSync(directory, { recursive: true });
  const picture = resolve(directory, "CI 合成图片.png");
  const video = resolve(directory, "CI 合成视频.mp4");
  execFileSync(
    "ffmpeg",
    [
      "-hide_banner",
      "-loglevel",
      "error",
      "-f",
      "lavfi",
      "-i",
      "color=c=blue:s=96x128",
      "-frames:v",
      "1",
      "-threads",
      "1",
      "-y",
      picture,
    ],
    { stdio: "pipe" },
  );
  execFileSync(
    "ffmpeg",
    [
      "-hide_banner",
      "-loglevel",
      "error",
      "-f",
      "lavfi",
      "-i",
      "color=c=purple:s=160x120:r=10",
      "-t",
      "2",
      "-c:v",
      "libx264",
      "-pix_fmt",
      "yuv420p",
      "-movflags",
      "+faststart",
      "-y",
      video,
    ],
    { stdio: "pipe" },
  );

  await page.goto("/");
  await page.getByLabel("访问令牌", { exact: true }).fill(token!);
  await page.getByRole("button", { name: "连接", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "发现你的绮梦" }),
  ).toBeVisible();
  const suffix = `${testInfo.retry}-${Date.now()}`;
  const libraryName = `CI 合成库 ${suffix}`;
  const authorName = `CI 合成作者 ${suffix}`;
  await page.goto("/manage/libraries");
  await page.getByRole("button", { name: "新建媒体库" }).click();
  await page.getByLabel("名称", { exact: true }).fill(libraryName);
  await page.getByRole("button", { name: "创建", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: libraryName, exact: true }),
  ).toBeVisible();
  await page.goto("/authors");
  await page.getByRole("button", { name: "添加作者" }).click();
  await page.getByLabel("作者名称", { exact: true }).fill(authorName);
  await page.getByRole("button", { name: "保存作者" }).click();
  await expect(
    page.getByRole("heading", { name: authorName, exact: true }),
  ).toBeVisible();

  await page.goto("/manage/upload");
  await page
    .getByLabel("目标媒体库")
    .selectOption({ label: `${libraryName} · 常规` });
  await expect(
    page.getByRole("button", { name: "选择文件", exact: true }),
  ).toBeEnabled();
  await page
    .locator("input[type=file]:not([webkitdirectory])")
    .first()
    .setInputFiles(picture);
  const pictureRow = page
    .locator(".queue-card")
    .filter({ hasText: "CI 合成图片.png" });
  await expect(pictureRow.getByText("已入库", { exact: true })).toBeVisible({
    timeout: 30_000,
  });
  await pictureRow.getByRole("link", { name: "查看入库内容" }).click();
  await expect(
    page.getByRole("heading", { name: "CI 合成图片.png", exact: true }),
  ).toBeVisible();
  const assetId = decodeURIComponent(
    new URL(page.url()).pathname.split("/").at(-1)!,
  );
  await page.getByRole("button", { name: "管理标签", exact: true }).click();
  await page.getByLabel("新标签名称").fill("CI 合成标签");
  await page.getByRole("button", { name: "加入", exact: true }).click();
  await page.getByRole("button", { name: "保存标签", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "保存标签", exact: true }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "编辑资料", exact: true }).click();
  await page.getByLabel("查找作者", { exact: true }).fill(authorName);
  await page
    .getByRole("group", { name: "作者", exact: true })
    .getByRole("button", { name: authorName, exact: true })
    .click();
  await page
    .getByLabel("个人备注", { exact: true })
    .fill("云端合成闭环，非真实媒体。");
  await page.getByRole("button", { name: "保存资料", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "保存资料", exact: true }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "收藏", exact: true }).click();
  await page.getByRole("button", { name: "点赞 · 0", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "今日已赞 · 1", exact: true }),
  ).toBeDisabled();
  await page.reload();
  await expect(
    page.getByRole("button", { name: "已收藏", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "CI 合成标签", exact: true }),
  ).toBeVisible();
  const response = await page.request.get(
    `/api/v1/assets/${encodeURIComponent(assetId)}`,
  );
  expect(response.ok()).toBe(true);
  const saved = await response.json();
  expect(saved.tags).toContain("CI 合成标签");
  expect(saved.note).toBe("云端合成闭环，非真实媒体。");
  expect(saved.authorName).toBe(authorName);
  expect(saved.favorite).toBe(true);
  await page.goto("/");
  await expect(
    page.getByRole("link", { name: "打开 CI 合成图片.png", exact: true }),
  ).toBeVisible();

  await page.goto("/manage/upload");
  await page
    .getByLabel("目标媒体库")
    .selectOption({ label: `${libraryName} · 常规` });
  await expect(
    page.getByRole("button", { name: "选择文件", exact: true }),
  ).toBeEnabled();
  await page
    .locator("input[type=file]:not([webkitdirectory])")
    .first()
    .setInputFiles(video);
  const videoRow = page
    .locator(".queue-card")
    .filter({ hasText: "CI 合成视频.mp4" });
  await expect(videoRow.getByText("已入库", { exact: true })).toBeVisible({
    timeout: 30_000,
  });
  await videoRow.getByRole("link", { name: "查看入库内容" }).click();
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((element) => (element as HTMLVideoElement).readyState),
    )
    .toBeGreaterThan(0);
  await page.locator("video").evaluate(async (element) => {
    const media = element as HTMLVideoElement;
    media.muted = true;
    await media.play();
  });
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((element) => (element as HTMLVideoElement).currentTime),
    )
    .toBeGreaterThan(0.1);
  await page
    .locator("video")
    .evaluate((element) => (element as HTMLVideoElement).pause());
  await page.getByRole("button", { name: "标记当前时间" }).click();
  await page.getByLabel("标记文字").fill("CI 时间轴标记");
  await page.getByRole("button", { name: "保存标记", exact: true }).click();
  await expect(page.getByText("CI 时间轴标记", { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByText("CI 时间轴标记", { exact: true })).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("live-synthetic-video.png"),
    fullPage: false,
  });
});
