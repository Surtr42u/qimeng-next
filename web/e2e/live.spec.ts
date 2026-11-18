import { test, expect } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { execFileSync } from "node:child_process";

test("same-commit service: login, upload, edit, recommend and play persist through reload", async ({
  page, browser,
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
  const pictureName = "CI 合成图片 " + testInfo.retry + ".png";
  const videoName = "CI 合成视频 " + testInfo.retry + ".mp4";
  const tagName = "CI 合成标签 " + testInfo.retry;
  const picture = resolve(directory, pictureName);
  const video = resolve(directory, videoName);
  execFileSync(
    "ffmpeg",
    [
      "-hide_banner",
      "-loglevel",
      "error",
      "-f",
      "lavfi",
      "-i",
      testInfo.retry ? "color=c=orange:s=96x128" : "color=c=blue:s=96x128",
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
      testInfo.retry ? "color=c=yellow:s=160x120:r=10" : "color=c=purple:s=160x120:r=10",
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
  await page.getByLabel("管理密码", { exact: true }).fill(token!);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "为你推荐" }),
  ).toBeVisible();
  const suffix = `${testInfo.retry}-${Date.now()}`;
  const libraryName = `CI 合成库 ${suffix}`;
  const authorName = `CI 合成作者 ${suffix}`;
  const configuredMediaRoot = process.env.QIMENG_E2E_LIBRARY_ROOT;
  expect(Boolean(configuredMediaRoot), "The cloud fixture media root is required").toBe(true);
  const libraryPath = resolve(configuredMediaRoot!, `library-${suffix}`);
  mkdirSync(libraryPath, { recursive: true });
  await page.goto("/manage/libraries");
  await page.getByLabel("库名称", { exact: true }).fill(libraryName);
  await page.getByLabel("服务器根路径", { exact: true }).fill(libraryPath);
  await page.getByRole("button", { name: "注册并扫描", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: libraryName, exact: true }),
  ).toBeVisible();
  await page.goto("/authors");
  await page.getByRole("button", { name: "新建作者", exact: true }).click();
  await page.getByLabel("作者名称", { exact: true }).fill(authorName);
  await page.getByRole("button", { name: "保存名称", exact: true }).click();
  const authorRow = page.locator("a.author-identity").filter({ has: page.getByText(authorName, { exact: true }) });
  await expect(authorRow).toBeVisible();
  const authorId = decodeURIComponent((await authorRow.getAttribute("href"))!.split("/").at(-1)!);

  await page.goto("/manage/upload");
  await page.getByRole("group", { name: "目标库", exact: true }).getByRole("button", { name: libraryName, exact: true }).click();
  await expect(page.getByRole("button", { name: "选择文件并立即上传", exact: true })).toBeEnabled();
  await page
    .getByLabel("选择要上传的文件", { exact: true })
    .setInputFiles(picture);
  const pictureRow = page
    .locator(".upload-task")
    .filter({ hasText: pictureName });
  await expect(pictureRow.getByText("已完成", { exact: true })).toBeVisible({
    timeout: 30_000,
  });
  await pictureRow.getByRole("link", { name: "查看入库内容" }).click();
  await expect(
    page.getByRole("heading", { name: pictureName, exact: true }),
  ).toBeVisible();
  const assetId = decodeURIComponent(
    new URL(page.url()).pathname.split("/").at(-1)!,
  );
  await page.getByRole("button", { name: "管理标签", exact: true }).click();
  await page.getByLabel("新标签名称").fill(tagName);
  await page.getByRole("dialog").getByRole("button", { name: "新建", exact: true }).click();
  await page.getByRole("button", { name: "保存标签", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "保存标签", exact: true }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  const lookup = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === "/api/v1/authors" && url.searchParams.get("q") === authorName;
  }, { timeout: 10000 });
  const authorField = page.getByLabel("查找作者", { exact: true });
  await authorField.fill(authorName);
  const suggestions = await lookup;
  expect(suggestions.status()).toBe(200);
  expect((await suggestions.json()).items.some((item: { id: string }) => item.id === authorId)).toBe(true);
  await expect(authorField).toHaveAttribute("aria-expanded", "true");
  await page.getByRole("group", { name: "作者联想", exact: true }).getByText(authorName, { exact: true }).click({ timeout: 10000 });
  const newName = "CI 新建作者 " + suffix;
  await page.getByLabel("查找作者", { exact: true }).fill(newName);
  await page.getByRole("button", { name: "新建：" + newName + " 保存时创建" }).click();
  await expect(page.getByText("待新建作者（保存后自动创建并关联）", { exact: true })).toBeVisible();
  const patchReceipt = page.waitForResponse(response => response.request().method() === "PATCH" && new URL(response.url()).pathname === "/api/v1/assets/" + encodeURIComponent(assetId));
  await page.locator("#page-asset-edit").getByRole("button", { name: "保存", exact: true }).click();
  const patchResponse = await patchReceipt;
  const patchData = await patchResponse.json();
  expect(patchResponse.status(), "作者关联回执 " + (patchData.code ?? "ok")).toBe(200);
  expect(patchData.authorIds).toHaveLength(2);
  try { await expect(page.locator("#page-asset-edit")).toHaveCount(0); }
  catch (failure) {
    await page.screenshot({ path: testInfo.outputPath("live-synthetic-author-edit.png") });
    throw failure;
  }
  const base = new URL(page.url()).origin;
  const secondSession = await browser.newContext({ baseURL: base });
  try {
    const second = await secondSession.newPage();
    await second.goto("/");
    await second.getByLabel("管理密码", { exact: true }).fill(token!);
    await second.getByRole("button", { name: "登录", exact: true }).click();
    await expect(second.getByRole("heading", { name: "为你推荐" })).toBeVisible();
    await second.goto(page.url());
    await expect(second.getByRole("button", { name: "收藏", exact: true })).toBeVisible();
    const favoriteResponse = page.waitForResponse(r => r.request().method() === "PUT" && r.url().endsWith("/favorite"));
    await page.getByRole("button", { name: "收藏", exact: true }).click();
    expect((await favoriteResponse).request().postDataJSON()).toEqual({ favorite: true });
    // This second browser context still has its original view of the asset.
    await expect(second.getByRole("button", { name: "收藏", exact: true })).toBeVisible();
    const likeResponse = second.waitForResponse(r => r.request().method() === "PUT" && r.url().endsWith("/like/today"));
    await second.getByRole("button", { name: "点赞 · 0", exact: true }).click();
    const like = await likeResponse;
    expect(like.request().postDataJSON()).toEqual({ liked: true });
    expect(await like.json()).toMatchObject({ favorite: true, liked: true, likeCount: 1 });
    const key = await like.request().headerValue("idempotency-key");
    expect(key).toBeTruthy();
    const replay = () => second.request.put(like.url(), { headers: { "Idempotency-Key": key!, Origin: base }, data: { liked: true } });
    expect(await (await replay()).json()).toMatchObject({ favorite: true, liked: true, likeCount: 1 });
    const readBack = async () => (await page.request.get("/api/v1/assets/" + encodeURIComponent(assetId))).json();
    expect(await readBack()).toMatchObject({ favorite: true, liked: true, likeCount: 1 });
    await second.getByRole("button", { name: "取消今日赞 · 1", exact: true }).click();
    await expect(second.getByRole("button", { name: "点赞 · 0", exact: true })).toBeEnabled();
    expect((await replay()).ok()).toBe(true);
    expect(await readBack()).toMatchObject({ favorite: true, liked: false, likeCount: 0 });
  } finally {
    await secondSession.close();
  }
  await page.reload();
  await expect(
    page.getByRole("button", { name: "已收藏", exact: true }),
  ).toBeVisible();
  await expect(
    page.locator(".detail-tags").getByText(tagName, { exact: true }),
  ).toBeVisible();
  const response = await page.request.get(
    `/api/v1/assets/${encodeURIComponent(assetId)}`,
  );
  expect(response.ok()).toBe(true);
  const saved = await response.json();
  expect(saved.tags).toContain(tagName);
  expect(saved.authorIds).toContain(authorId);
  expect(saved.authorIds).toHaveLength(2);
  const createdAuthorId = saved.authorIds.find((id: string) => id !== authorId);
  const linkedAuthor = await page.request.get("/api/v1/authors/" + encodeURIComponent(createdAuthorId));
  expect((await linkedAuthor.json()).name).toBe(newName);
  expect(saved.favorite).toBe(true);
  await page.goto(`/authors/${encodeURIComponent(authorId)}/profile`);
  await page
    .getByRole("button", { name: "编辑基础资料", exact: true })
    .click();
  await page
    .getByRole("textbox", { name: "整理简介", exact: true })
    .fill("云端真实服务确认的合成作者简介");
  await page.getByRole("button", { name: "保存资料", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "编辑基础资料", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "离线头像", exact: true }).click();
  await page.getByLabel("或选择本地头像").setInputFiles(picture);
  await page.getByRole("button", { name: "保存本地头像", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "离线头像", exact: true }),
  ).toBeEnabled();
  await page.reload();
  await expect(page.locator(".creator-profile-avatar img")).toBeVisible();
  const profileResponse = await page.request.get(
    `/api/v1/authors/${encodeURIComponent(authorId)}/profile`,
  );
  expect(profileResponse.ok()).toBe(true);
  const profile = await profileResponse.json();
  expect(
    profile.blocks.find((block: { block: string }) => block.block === "basic")
      .values.summary,
  ).toBe("云端真实服务确认的合成作者简介");
  const offlineAvatar = profile.blocks.find(
    (block: { block: string }) => block.block === "x",
  ).avatar;
  const avatarResponse = await page.request.get(offlineAvatar.url);
  expect(avatarResponse.ok()).toBe(true);
  expect(avatarResponse.headers()["content-type"]).toBe("image/png");
  await page.goto("/");
  await expect(
    page.getByRole("link", { name: "打开 " + pictureName, exact: true }),
  ).toBeVisible();

  await page.goto("/manage/upload");
  await page.getByRole("group", { name: "目标库", exact: true }).getByRole("button", { name: libraryName, exact: true }).click();
  await expect(page.getByRole("button", { name: "选择文件并立即上传", exact: true })).toBeEnabled();
  await page.locator(".upload-metadata-card summary").click();
  await page.getByLabel("批次作者（可选，联想选择）").fill(authorName);
  await page.getByRole("group", { name: "作者联想" }).getByText(authorName, { exact: true }).click();
  await page.getByPlaceholder("新站点或 URL，回车加入").fill("合成渠道");
  await page.getByPlaceholder("新站点或 URL，回车加入").press("Enter");
  await page
    .getByLabel("选择要上传的文件", { exact: true })
    .setInputFiles(video);
  const videoRow = page
    .locator(".upload-task")
    .filter({ hasText: videoName });
  await expect(videoRow.getByText("已完成", { exact: true })).toBeVisible({
    timeout: 30_000,
  });
  const updatedAuthor = await page.request.get("/api/v1/authors/" + encodeURIComponent(authorId));
  expect((await updatedAuthor.json()).acquisitionSources).toContain("合成渠道");
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
  const videoId = decodeURIComponent(new URL(page.url()).pathname.split("/").at(-1)!);
  const videoData = await (await page.request.get(`/api/v1/assets/${encodeURIComponent(videoId)}`)).json();
  const duration = await page.locator("video").evaluate(el => (el as HTMLVideoElement).duration);
  await page.locator("video").hover();
  const track = await page.locator(".art-control-progress").boundingBox();
  expect(track).toBeTruthy();
  for (const fraction of [0.35, 0.78, 0.52]) {
    const expected = Math.round(duration * 1000 * fraction);
    const receipt = page.waitForResponse(r => {
      if (r.request().method() !== "POST" || !r.url().endsWith("/events")) return false;
      const value = r.request().postDataJSON();
      return value.kind === "progress" && Math.abs(value.positionMs - expected) < 80;
    });
    const y = track!.y + track!.height / 2;
    await page.mouse.move(track!.x + track!.width * 0.1, y);
    await page.mouse.down();
    await page.mouse.move(track!.x + track!.width * fraction, y, { steps: 4 });
    await page.mouse.up();
    expect((await receipt).status()).toBe(200);
  }
  const resumeMs = duration * 1000 * 0.52;
  await expect.poll(async () => {
    const current = await (await page.request.get(`/api/v1/assets/${encodeURIComponent(videoId)}`)).json();
    return current.positionContentVersion === videoData.contentVersion ? Math.abs(current.positionMs - resumeMs) : Infinity;
  }).toBeLessThan(80);
  const restoredDetail = page.waitForResponse(response => new URL(response.url()).pathname === `/api/v1/assets/${encodeURIComponent(videoId)}` && response.request().method() === "GET");
  await page.reload();
  const restoredAsset = await (await restoredDetail).json();
  expect(restoredAsset.positionContentVersion).toBe(videoData.contentVersion);
  expect(Math.abs(restoredAsset.positionMs - resumeMs)).toBeLessThan(80);
  await expect.poll(() => page.locator("video").evaluate((el, expected) =>
    Math.abs((el as HTMLVideoElement).currentTime * 1000 - expected), resumeMs)).toBeLessThan(80);
  // The original Web presents timeline marks on the player. Authoring uses the real
  // service here; do not add a new detail-page panel just to drive a test.
  const markLabel = 'CI 时间轴 <img src="synthetic-missing" onerror="window.syntheticUnsafe=true">';
  const markResponse = await page.request.post(`/api/v1/assets/${encodeURIComponent(videoId)}/timeline`, {
    headers: { "Idempotency-Key": `ci-timeline-${suffix}`, Origin: new URL(page.url()).origin },
    data: { label: markLabel, positionMs: 500, color: "#a855f7", contentVersion: videoData.contentVersion },
  });
  expect(markResponse.ok()).toBe(true);
  const timelineURL = "/api/v1/assets/" + encodeURIComponent(videoId) + "/timeline";
  const storedMarks = await (await page.request.get(timelineURL)).json();
  expect(storedMarks.items.find((item: { label: string }) => item.label === markLabel)?.contentVersion).toBe(videoData.contentVersion);
  const timelineRead = page.waitForResponse(response => new URL(response.url()).pathname === timelineURL && response.request().method() === "GET");
  await page.reload();
  expect((await timelineRead).status()).toBe(200);
  await expect.poll(() => page.locator("video").evaluate(video => (video as HTMLVideoElement).duration)).toBeGreaterThan(0);
  const marker = page.locator('.art-progress-highlight span');
  await expect(marker).toHaveCount(1);
  await expect(marker).toHaveAttribute("data-text", markLabel);
  await expect(page.locator(".art-progress-highlight img")).toHaveCount(0);
  await page.locator(".art-progress").hover();
  await marker.click();
  await expect.poll(() => page.locator("video").evaluate(element => (element as HTMLVideoElement).currentTime)).toBeGreaterThan(0.4);
  await page.screenshot({
    path: testInfo.outputPath("live-synthetic-video.png"),
    fullPage: false,
  });
});
