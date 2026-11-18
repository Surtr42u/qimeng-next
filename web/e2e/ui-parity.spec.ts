import { test, expect } from '@playwright/test';
import { mockLibrary } from './fixture';

test('author source replacement keeps a failed clear draft and saves an explicit empty array', async ({ page }) => {
  const state = await mockLibrary(page);
  await page.goto('/authors');
  await page.getByRole('button', { name: '编辑资料', exact: true }).click();
  const editor = page.getByRole('dialog', { name: '编辑作者资料' });
  await editor.getByRole('button', { name: '来源 / 出处', exact: true }).click();
  await editor.getByRole('button', { name: 'synthetic-forum', exact: true }).click();
  state.authorSourceError = true;
  await editor.getByRole('button', { name: '保存来源', exact: true }).click();
  await expect(editor.getByRole('alert')).toContainText('合成来源保存失败');
  await expect(editor.getByRole('button', { name: 'synthetic-forum', exact: true })).toHaveAttribute('aria-pressed', 'false');
  state.authorSourceError = false;
  await editor.getByRole('button', { name: '保存来源', exact: true }).click();
  await expect(editor.getByRole('button', { name: '保存来源', exact: true })).toBeDisabled();
  await editor.getByRole('button', { name: '关闭', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('来源已保存');
  expect(state.authorBodies.map(item => item.acquisitionSources)).toEqual([[], []]);
});

test('facets retain the fixed author scope and omit only the selected row', async ({ page }) => {
  const state = await mockLibrary(page);
  await page.goto('/album?authorId=synthetic-author&favorite=true&workKeys=opaque%3Asynthetic-work&roleKeys=opaque%3Asynthetic-role');
  await expect.poll(() => state.facetRequests.length).toBeGreaterThanOrEqual(4);
  const work = state.facetRequests.find(url => url.searchParams.get('row') === 'works')!;
  const role = state.facetRequests.find(url => url.searchParams.get('row') === 'roles')!;
  expect(work.searchParams.get('authorId')).toBe('synthetic-author');
  expect(work.searchParams.get('favorite')).toBe('true');
  expect(work.searchParams.getAll('workKeys')).toEqual([]);
  expect(work.searchParams.getAll('roleKeys')).toEqual(['opaque:synthetic-role']);
  expect(role.searchParams.getAll('workKeys')).toEqual(['opaque:synthetic-work']);
  expect(role.searchParams.getAll('roleKeys')).toEqual([]);
});

test('album date headers keep the matching total before a continuation page', async ({ page }) => {
  await mockLibrary(page);
  await page.goto('/album');
  await expect(page.locator('.media-card')).toHaveCount(48);
  await expect(page.locator('.album-group-count')).toHaveText('72 项');
  await page.locator('.media-card').last().scrollIntoViewIfNeeded();
  await expect(page.locator('.media-card')).toHaveCount(72);
  await expect(page.locator('.album-group-count')).toHaveText('72 项');
});

test('the existing four-link sidebar and 16:9 covers remain at desktop and narrow widths', async ({ page }, testInfo) => {
  await mockLibrary(page);
  for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport);
    await page.goto('/');
    const categories = page.getByRole('navigation', { name: '首页内容', exact: true });
    await expect(categories.getByRole('button')).toHaveText(['推荐', 'COS', '排行榜']);
    await expect(categories.getByRole('button', { name: '推荐', exact: true })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#page-home .card').first()).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath(`parity-home-${viewport.width}.png`), animations: 'disabled' });
    await page.goto('/album');
    const nav = page.getByRole('navigation', { name: '主导航', exact: true });
    await expect(nav.getByRole('link')).toHaveText(['首页', '相册', '我的', '数据']);
    const sidebar = await page.locator('.sidebar').boundingBox();
    expect(sidebar!.width).toBeCloseTo(72 * 1.1, 0);
    const cover = await page.locator('.card--cover').first().boundingBox();
    expect(cover!.width / cover!.height).toBeCloseTo(16 / 9, 1);
    await expect(page.locator('.bottom-navigation')).toHaveCount(0);
    await page.screenshot({ path: testInfo.outputPath(`parity-album-${viewport.width}.png`), animations: 'disabled' });
    await page.goto('/authors');
    await expect(page.locator('.a-list li')).toHaveCount(1);
    await page.screenshot({ path: testInfo.outputPath(`parity-authors-${viewport.width}.png`), animations: 'disabled' });
  }
});
