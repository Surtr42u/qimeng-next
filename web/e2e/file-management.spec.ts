import { expect, test } from '@playwright/test';
import { mockLibrary } from './fixture';

test('file management replays an uncertain move with its original key and reads the committed name', async ({ page }) => {
  const fixture = await mockLibrary(page);
  fixture.assets.splice(1);
  await page.route('**/api/v1/libraries/synthetic-library/directories**', route => route.fulfill({ json: { items: [], directFileCount: 1 } }));
  const keys: string[] = [];
  await page.route('**/api/v1/assets/synthetic-0/move', async route => {
    keys.push(route.request().headers()['idempotency-key']!);
    const body = route.request().postDataJSON();
    expect(body).toEqual({ expectedRevision: 1, relativePath: 'renamed.jpg' });
    if (keys.length === 1) {
      fixture.assets[0]!.fileName = 'renamed.jpg';
      fixture.assets[0]!.relativePath = 'renamed.jpg';
      fixture.assets[0]!.revision = 2;
      return route.abort('failed');
    }
    return route.fulfill({ json: fixture.assets[0] });
  });
  await page.goto('/manage/files?libraryId=synthetic-library&directory=');
  await expect(page.locator('.dir-file-list li')).toHaveCount(1);
  await page.getByRole('button', { name: '移动/重命名', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '整理文件', exact: true });
  const name = dialog.getByPlaceholder('新文件名（含扩展名）');
  await name.fill('renamed.jpg');
  await dialog.getByRole('button', { name: '保存', exact: true }).click();
  await expect(dialog.getByText('结果尚未确认；重试会沿用同一操作身份核对', { exact: true })).toBeVisible();
  await expect(name).toBeDisabled();
  await dialog.getByRole('button', { name: '重试未确认或失败项', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.locator('.dir-file-name')).toHaveText('renamed.jpg');
  expect(keys).toHaveLength(2);
  expect(keys[1]).toBe(keys[0]);
});
