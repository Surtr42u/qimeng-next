import { expect, test } from '@playwright/test';
import type { components } from '../src/api/generated/schema';
import { mockLibrary } from './fixture';

test('the original manifest includes unreceived and unnamed inputs after reload', async ({ page }) => {
  await mockLibrary(page);
  const batch: components['schemas']['Batch'] = {
    id: 'synthetic-manifest', libraryId: 'synthetic-library', mode: 'ordinary', label: '完整原始清单', maxResearchCalls: 0,
    state: 'partial', revision: 3, manifestState: 'sealed', manifestRevision: 2, manifestFingerprint: 'a'.repeat(64), expectedInputs: 2,
    researchCalls: 0, backupState: 'not_covered', cleanupState: 'retained', createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z',
  };
  const inputs: components['schemas']['BatchInput'][] = [
    { id: 'known-input', nameState: 'known', originalFileName: 'original.jpg', size: 1024, ordinal: 0, state: 'ingested', assetId: 'synthetic-0' },
    { id: 'unknown-input', nameState: 'unavailable', ordinal: 1, state: 'pending' },
  ];
  const cancellations: unknown[] = [];
  await page.route('**/api/v1/batches/synthetic-manifest**', async route => {
    if (route.request().method() === 'POST') {
      cancellations.push(route.request().postDataJSON());
      inputs[1]!.state = 'cancelled';
      return route.fulfill({ json: inputs[1] });
    }
    return route.fulfill({ json: { batch, inputs, items: [], plans: [] } });
  });
  await page.goto('/manage/tasks/synthetic-manifest');
  await expect(page.getByText('第2项（名称读取失败）', { exact: true })).toBeVisible();
  await expect(page.getByText('尚未收到', { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByText('第2项（名称读取失败）', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: '取消本项', exact: true }).click();
  await expect(page.getByText('已取消', { exact: true })).toBeVisible();
  await expect(page.getByText('第2项（名称读取失败）', { exact: true })).toBeVisible();
  expect(cancellations).toEqual([{ manifestFingerprint: batch.manifestFingerprint }]);
  expect(inputs[1]!.originalFileName).toBeUndefined();
});

test('device storage upgrade preserves existing metadata and adds the batch store', async ({ page }) => {
  await mockLibrary(page);
  await page.goto('/icon.svg');
  await page.evaluate(() => new Promise<void>((resolve, reject) => {
    const request = indexedDB.open('qimeng-device-state', 1);
    request.onupgradeneeded = () => { request.result.createObjectStore('uploads', { keyPath: 'id' }); request.result.createObjectStore('events', { keyPath: 'id' }); };
    request.onerror = () => reject(request.error);
    request.onsuccess = () => {
      const transaction = request.result.transaction('uploads', 'readwrite');
      transaction.objectStore('uploads').put({ id: 'preserved-other-scope', scope: 'previous-instance:previous-device', phase: 'ingested' });
      transaction.oncomplete = () => { request.result.close(); resolve(); };
      transaction.onabort = () => reject(transaction.error);
    };
  }));
  await page.goto('/');
  await expect(page.getByRole('heading', { name: '为你推荐', exact: true })).toBeVisible();
  await expect.poll(() => page.evaluate(() => new Promise<{ version: number; stores: string[]; record: boolean }>((resolve, reject) => {
    const request = indexedDB.open('qimeng-device-state');
    request.onerror = () => reject(request.error);
    request.onsuccess = () => {
      const database = request.result;
      const transaction = database.transaction('uploads');
      const found = transaction.objectStore('uploads').get('preserved-other-scope');
      transaction.oncomplete = () => { resolve({ version: database.version, stores: [...database.objectStoreNames], record: !!found.result }); database.close(); };
    };
  }))).toEqual({ version: 3, stores: ['events', 'maintenance_folders', 'upload_batches', 'uploads'], record: true });
});
