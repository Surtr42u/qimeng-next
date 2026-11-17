import { test, expect } from '@playwright/test';
import { mockLibrary } from './fixture';

test('settings keep the old action placement on desktop and narrow screens', async ({ page }, testInfo) => {
  await mockLibrary(page);
  await page.setViewportSize({ width: 1440, height: 800 });
  await page.goto('/settings');
  const actions = page.getByRole('group', { name: '设置操作', exact: true });
  const recommendation = page.locator('.settings-preference-column > .settings-card');
  await expect(recommendation.getByRole('heading', { name: '推荐偏好' })).toBeVisible();
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 800 });
    await actions.scrollIntoViewIfNeeded();
    const box = await actions.boundingBox();
    const above = await recommendation.boundingBox();
    expect(box!.y).toBeGreaterThanOrEqual(above!.y + above!.height - 1);
    await expect(actions.getByRole('button', { name: '保存设置', exact: true })).toBeInViewport();
    await expect(actions.getByRole('button', { name: '退出登录', exact: true })).toBeInViewport();
    await page.screenshot({ path: testInfo.outputPath(`parity-settings-${width}.png`), animations: 'disabled' });
  }
});

test('a completed settings snapshot does not erase edits made while the request was pending', async ({ page }) => {
  const fixture = await mockLibrary(page);
  await page.goto('/settings');
  const edge = page.getByLabel('缩略图长边', { exact: true });
  await edge.fill('512');
  fixture.holdSettings = true;
  await page.getByRole('button', { name: '保存设置', exact: true }).click();
  await expect.poll(() => !!fixture.releaseSettings).toBe(true);
  await edge.fill('1024');
  fixture.releaseSettings?.();
  await expect(page.getByRole('button', { name: '保存设置', exact: true })).toBeEnabled();
  await expect(edge).toHaveValue('1024');
  expect(fixture.settings.scan.thumbEdge).toBe(512);
  fixture.holdSettings = false;
  await page.getByRole('button', { name: '保存设置', exact: true }).click();
  await expect.poll(() => fixture.settings.scan.thumbEdge).toBe(1024);
  expect(fixture.settingsBodies.at(-1)?.expectedRevision).toBe(2);
});
