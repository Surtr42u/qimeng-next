import { test, expect } from '@playwright/test';
import { mockLibrary } from './fixture';

test('query failure remains distinct from an empty album', async ({ page }) => {
  const fixture = await mockLibrary(page); fixture.assetError = true;
  await page.goto('/album'); await expect(page.getByRole('alert')).toContainText('合成服务暂不可用');
  await expect(page.getByText('这里还没有媒体')).toHaveCount(0);
  fixture.assetError = false; await page.getByRole('button', { name: '重试', exact: true }).click();
  await expect(page.locator('.media-card')).toHaveCount(48);
});

test('cursor pagination and search keep the active query isolated', async ({ page }) => {
  await mockLibrary(page); await page.goto('/album'); await expect(page.locator('.media-card')).toHaveCount(48);
  await page.getByRole('button', { name: '加载更多', exact: true }).click(); await expect(page.locator('.media-card')).toHaveCount(72);
  await page.getByRole('textbox', { name: '搜索媒体' }).fill('合成场景 71'); await page.getByRole('textbox', { name: '搜索媒体' }).press('Enter');
  await expect(page.locator('.media-card')).toHaveCount(1); await expect(page.locator('.media-card')).toContainText('合成场景 71.jpg');
});

test('tag conflicts preserve the draft; a deliberate empty set is submitted', async ({ page }) => {
  const fixture = await mockLibrary(page); fixture.tagConflict = true;
  await page.goto('/asset/synthetic-0'); await page.getByRole('button', { name: '管理标签', exact: true }).click();
  await page.getByRole('button', { name: '清空选择' }).click(); await page.getByRole('button', { name: '保存标签' }).click();
  await expect(page.getByRole('alert')).toContainText('内容已发生变化');
  await expect(page.getByText('已选 0 项', { exact: true })).toBeVisible();
  expect(fixture.tagBodies[0]).toEqual({ expectedRevision: 1, tags: [] });
  fixture.tagConflict = false; await page.getByRole('button', { name: '保存标签' }).click();
  await expect(page.getByRole('button', { name: '保存标签' })).toHaveCount(0);
  expect(fixture.tagBodies[1]).toEqual({ expectedRevision: 1, tags: [] });
});

test('rapid navigation retains one page and the selected navigation semantics', async ({ page, isMobile }, testInfo) => {
  await mockLibrary(page); await page.goto('/album');
  const nav = page.getByRole('navigation', { name: isMobile ? '主导航' : '桌面主导航', exact: true });
  for (const name of ['我的', '数据', '首页', '相册']) await nav.getByRole('link', { name, exact: true }).click();
  await expect(page.locator('[data-route-surface]')).toHaveCount(1);
  await expect(page.getByRole('heading', { name: '相册', exact: true })).toBeVisible();
  await expect(nav.getByRole('link', { name: '相册', exact: true })).toHaveAttribute('aria-current', 'page');
  await page.screenshot({ path: testInfo.outputPath('synthetic-album.png'), fullPage: false });
});

test('return restores browsing position and image viewer restores focus', async ({ page }) => {
  await mockLibrary(page); await page.goto('/album'); await expect(page.locator('.media-card')).toHaveCount(48);
  const card = page.locator('.media-card').nth(30); await card.scrollIntoViewIfNeeded();
  const scrollBefore = await page.evaluate(() => window.scrollY); await card.click();
  await page.getByRole('button', { name: '查看原图并缩放' }).click();
  await expect(page.getByRole('dialog')).toBeVisible(); await page.keyboard.press('Escape');
  await expect(page.getByRole('button', { name: '查看原图并缩放' })).toBeFocused();
  await page.getByRole('button', { name: '返回浏览' }).click();
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(Math.max(0, scrollBefore - 80));
});

test('keyboard selection, reduced motion and light mode remain usable', async ({ page }, testInfo) => {
  await page.emulateMedia({ reducedMotion: 'reduce' }); await mockLibrary(page); await page.goto('/settings');
  const dark = page.getByRole('tab', { name: '暮色玻璃' }); await dark.focus(); await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('tab', { name: '晨雾玻璃' })).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.screenshot({ path: testInfo.outputPath('synthetic-settings-light.png'), fullPage: false });
});
