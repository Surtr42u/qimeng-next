import { chromium } from "@playwright/test";
import { createHash } from "node:crypto";
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { join } from "node:path";
import { execFileSync } from "node:child_process";
import assert from "node:assert/strict";

const { GITHUB_ACTIONS, GITHUB_SHA, QIMENG_PACKAGE_PORT: port, QIMENG_PACKAGE_APP_DIR: app,
  QIMENG_PACKAGE_TEST_DIR: work, QIMENG_PACKAGE_EVIDENCE_DIR: evidence,
  QIMENG_PACKAGE_TARGET: target, QIMENG_PACKAGE_TEST_MODE: mode, QIMENG_PACKAGE_PASSWORD_FILE: passwordFile } = process.env;
assert(GITHUB_ACTIONS === "true" && /^\d{1,5}$/.test(port) && !["8420", "8421"].includes(port));
const base = "http://127.0.0.1:" + port;
const password = await readFile(passwordFile, "utf8");
const source = JSON.parse(await readFile(join(app, "SOURCE.json"), "utf8"));
assert.equal(source.sourceCommit, GITHUB_SHA);
const browser = await chromium.launch(process.platform === "win32" ? { channel: "msedge" } : {});
try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  await page.goto(base);
  const entryURLs = await page.locator('script[type="module"][src^="/assets/"], link[rel="stylesheet"][href^="/assets/"]').evaluateAll(
    nodes => nodes.map(node => node.getAttribute("src") || node.getAttribute("href")));
  assert.equal(entryURLs.length, 2);
  for (const url of entryURLs) {
    const original = await readFile(join(app, "web", url.slice(1)));
    const compressed = await readFile(join(app, "web", url.slice(1) + ".gz"));
    const response = await page.request.get(base + url, { headers: { "Accept-Encoding": "gzip, br" } });
    assert.equal(response.status(), 200);
    assert.equal(response.headers()["content-encoding"], "gzip");
    assert.equal(Number(response.headers()["content-length"]), compressed.length);
    assert.equal(response.headers().vary, "Accept-Encoding");
    assert.deepEqual(await response.body(), original);
    const identity = await page.request.get(base + url, { headers: { "Accept-Encoding": "gzip;q=0" } });
    assert.equal(identity.status(), 200);
    assert.equal(identity.headers()["content-encoding"], undefined);
    assert.deepEqual(await identity.body(), original);
  }
  const home = page.getByRole("heading", { name: "为你推荐", exact: true });
  const passwordField = page.getByLabel("管理密码", { exact: true });
  // 本机桌面包默认免密自动进入；远程/NAS 包仍显示密码表单。
  await Promise.race([
    home.waitFor({ timeout: 20_000 }),
    passwordField.waitFor({ timeout: 20_000 }),
  ]);
  if (await passwordField.isVisible().catch(() => false)) {
    await passwordField.fill(password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
  }
  await home.waitFor();
  await page.getByText("还没有内容——先到「维护 → 文件管理」注册一个媒体库并扫描。", { exact: true }).waitFor();
  await page.screenshot({ path: join(evidence, target + "-" + mode + "-home.png") });
  const api = base + "/api/v1";
  const required = async result => { assert(result.ok(), "Packaged API returned " + result.status()); return result; };
  const key = text => ({ Origin: base, "Idempotency-Key": "package-smoke-" + text });
  let upgradeProof;
  if (mode === "native") {
    const prior = JSON.parse(process.env.QIMENG_PACKAGE_PRIOR_RECORDS);
    const instance = await (await required(await page.request.get(api + "/instance"))).json();
    assert.equal(instance.id, prior.instanceId);
    const author = await (await required(await page.request.get(api + "/authors/" + encodeURIComponent(prior.author.id)))).json();
    for (const [field, value] of Object.entries(prior.author)) assert.equal(author[field], value);
    upgradeProof = { schemaVersionBefore: prior.schemaVersionBefore, schemaVersionAfter: prior.schemaVersionAfter,
      occupiedPortPreservedDatabase: true, originalInstanceAndAuthorReadAfterLogin: true };
  }
  const inventory = await (await required(await page.request.get(api + "/libraries"))).json();
  assert.equal(inventory.totalMatched, 0);
  let checkpoint = await (await required(await page.request.post(api + "/backups", { headers: key("checkpoint") }))).json();
  assert.equal(checkpoint.assetCount, 0);
  const path = api + "/backups/" + encodeURIComponent(checkpoint.id);
  checkpoint = await (await required(await page.request.post(path + "/export", { headers: key("export") }))).json();
  for (let i = 0; i < 100 && ["queued", "running"].includes(checkpoint.exportState); i++) {
    await new Promise(resolve => setTimeout(resolve, 300));
    checkpoint = await (await required(await page.request.get(path))).json();
  }
  assert.equal(checkpoint.exportState, "completed");
  assert(checkpoint.archiveBytes < 32 * 1024 * 1024, "An empty synthetic checkpoint exceeded the smoke budget.");
  const bytes = await (await required(await page.request.get(path + "/archive"))).body();
  const hash = createHash("sha256").update(bytes).digest("hex");
  assert.equal(hash, checkpoint.archiveHash);
  await mkdir(work, { recursive: true });
  const archive = join(work, "fixture.qnext");
  await writeFile(archive, bytes);
  const tool = join(app, process.platform === "win32" ? "qimeng-backup.exe" : "qimeng-backup");
  execFileSync(tool, ["verify", "--archive", archive, "--sha256", hash], { stdio: "pipe", timeout: 30000 });
  execFileSync(tool, ["restore", "--archive", archive, "--sha256", hash, "--destination", join(work, "isolated-restore")], { stdio: "pipe", timeout: 30000 });
  let videoProof;
  if (mode === "compose-media") {
    const video = await readFile(process.env.QIMENG_PACKAGE_VIDEO);
    const sourceHash = createHash("sha256").update(video).digest("hex");
    const library = await (await required(await page.request.post(api + "/libraries", { headers: key("library"), data: { name: "合成视频", kind: "regular" } }))).json();
    const intake = await (await required(await page.request.post(api + "/uploads", { headers: key("video-create"), data: { libraryId: library.id, mode: "ordinary", fileName: "synthetic-preview.mp4", mimeType: "video/mp4", size: video.length, expectedHash: sourceHash } }))).json();
    const upload = api + "/uploads/" + encodeURIComponent(intake.id);
    await required(await page.request.put(upload + "/content", { data: video, headers: { Origin: base, "Content-Type": "application/octet-stream", "Upload-Offset": "0" } }));
    const published = await (await required(await page.request.post(upload + "/complete", { headers: key("video-complete") }))).json();
    assert.equal(published.asset.mediaType, "video");
    assert.equal(published.asset.probeState, "ready");
    assert.equal(published.asset.width, 160); assert.equal(published.asset.height, 120);
    const thumbnail = await (await required(await page.request.get(api + "/assets/" + encodeURIComponent(published.asset.id) + "/thumbnail"))).body();
    assert.equal(thumbnail.subarray(0, 3).toString("hex"), "ffd8ff");
    const materials = await (await required(await page.request.get(upload + "/research-materials", { params: { expectedHash: sourceHash } }))).json();
    assert.equal(materials.sourceSha256, sourceHash); assert.equal(materials.businessWrites, 0);
    assert(materials.artifacts.length > 0);
    for (const frame of materials.artifacts) {
      assert.equal(frame.mimeType, "image/jpeg"); assert.equal(frame.sourceSha256, sourceHash);
      const image = await (await required(await page.request.get(api + "/research-artifacts/" + encodeURIComponent(frame.id) + "/content"))).body();
      assert.equal(image.subarray(0, 3).toString("hex"), "ffd8ff");
      assert.equal(createHash("sha256").update(image).digest("hex"), frame.sha256);
    }
    videoProof = { width: published.asset.width, height: published.asset.height, thumbnailBytes: thumbnail.length, researchFrames: materials.artifacts.length, verifiedOriginalSHA256: true };
  }
  await writeFile(join(evidence, target + "-" + mode + "-smoke.json"),
    JSON.stringify({ sourceCommit: GITHUB_SHA, target, mode, architecture: process.arch, homepage: "为你推荐", registeredLibrariesAtStartup: 0, archiveVerified: true, restoredToNewDirectory: true, upgradeProof, videoProof }, null, 2) + "\n");
} finally { await browser.close(); }
