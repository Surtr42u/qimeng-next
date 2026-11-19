import { expect, test } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import type { components } from '../src/api/generated/schema';

for (const mode of ['ordinary', 'intelligent'] as const) {
  test(`browser selection keeps every mixed ${mode} input and uploads only the good image`, async ({ page, request }) => {
    expect([process.env.CI, process.env.QIMENG_E2E_BASE_URL]).toEqual(['true', 'http://127.0.0.1:18481']);
    const token = process.env.QIMENG_E2E_TOKEN!;
    const name = `混合上传 ${randomUUID()}`;
    const created = await request.post('/api/v1/libraries', { headers: { Authorization: 'Bearer ' + token, 'Idempotency-Key': randomUUID() },
      data: { name, kind: 'regular' } });
    expect(created.ok()).toBe(true);
    const library = await created.json();
    await page.goto('/');
    await page.getByLabel('管理密码', { exact: true }).fill(token);
    await page.getByRole('button', { name: '登录', exact: true }).click();
    await expect(page.getByRole('heading', { name: '为你推荐' })).toBeVisible();
    await page.goto('/manage/upload');
    await page.getByRole('group', { name: '目标库' }).getByRole('button', { name, exact: true }).click();
    await page.getByText('上传方式与作品命名', { exact: true }).click();
    const family = '合成作品';
    let expectedPath: string | undefined;
    if (mode === 'ordinary') {
      await expect(page.getByLabel('上传方式', { exact: true })).toHaveValue('ordinary');
      await page.getByLabel('按作品基名自动编号').check();
      await page.getByLabel('作品基名', { exact: true }).fill(family);
      const preview = await page.request.post(`/api/v1/libraries/${library.id}/naming/preview`, {
        data: { intent: 'new_work', family, items: [{ fileName: 'good.png' }] } });
      expect(preview.ok()).toBe(true);
      expectedPath = (await preview.json()).items[0].relativePath;
    } else await page.getByLabel('上传方式', { exact: true }).selectOption('intelligent');

    const png = await page.evaluate(label => {
      const canvas = document.createElement('canvas');
      canvas.width = 320; canvas.height = 64;
      const ctx = canvas.getContext('2d')!;
      ctx.fillStyle = 'teal'; ctx.fillRect(0, 0, 320, 64);
      ctx.fillStyle = 'white'; ctx.fillText(label, 8, 35);
      return canvas.toDataURL('image/png').split(',')[1]!;
    }, name);
    const uploads: string[] = [];
    const aiRequests: string[] = [];
    page.on('request', event => {
      const path = new URL(event.url()).pathname;
      if (path === '/api/v1/uploads' && event.method() === 'POST') uploads.push(event.postDataJSON().inputId);
      if (path.startsWith('/api/v1/ai/') || path.startsWith('/api/v1/research/')) aiRequests.push(path);
    });
    const picker = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: '选择文件并立即上传', exact: true }).click();
    await (await picker).setFiles([
      { name: 'empty.png', mimeType: 'image/png', buffer: Buffer.alloc(0) },
      { name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('synthetic text') },
      { name: 'good.png', mimeType: 'image/png', buffer: Buffer.from(png, 'base64') },
    ]);
    const rows = page.locator('.upload-task');
    await expect(rows).toHaveCount(3);
    await expect(rows.filter({ hasText: 'good.png' })).toContainText(mode === 'ordinary' ? '已完成' : '已接收，等待整理', { timeout: 30_000 });
    await page.reload();
    await expect(rows.filter({ hasText: 'empty.png' })).toContainText('文件为空');
    await expect(rows.filter({ hasText: 'notes.txt' })).toContainText('不支持此文件格式');
    await expect(rows.getByRole('button', { name: /重试|重选|取消/ })).toHaveCount(0);
    await rows.filter({ hasText: 'empty.png' }).getByRole('link', { name: '本批报告' }).click();
    await expect(page.getByRole('heading', { name: '原始输入', exact: true })).toBeVisible();
    const batchId = new URL(page.url()).pathname.split('/').at(-1)!;
    const reportURL = '/api/v1/batches/' + batchId;
    await expect(page.getByText('文件为空，未创建上传。', { exact: true })).toBeVisible();
    await expect(page.getByText('不支持此文件格式，未创建上传。', { exact: true })).toBeVisible();
    const report: components['schemas']['BatchDetail'] = await (await page.request.get(reportURL)).json();
    expect(report.inputs.map(item => [item.originalFileName, item.state, item.errorCode])).toEqual([
      ['empty.png', 'failed', 'empty_input'], ['notes.txt', 'failed', 'unsupported_media'],
      ['good.png', mode === 'ordinary' ? 'ingested' : 'staged', undefined],
    ]);
    expect(report.inputs.slice(0, 2).every(item => !item.uploadId && !item.assetId)).toBe(true);
    expect(report.inputs[0]!.size).toBe(0);
    expect(uploads).toEqual([report.inputs[2]!.id]);
    expect(report.items.map(item => item.upload.id)).toEqual([report.inputs[2]!.uploadId]);
    if (mode === 'ordinary') {
      const asset = await page.request.get('/api/v1/assets/' + report.inputs[2]!.assetId);
      expect(asset.ok()).toBe(true);
      expect(await asset.json()).toMatchObject({ libraryId: library.id, originalFileName: 'good.png', relativePath: expectedPath });
      expect(report.batch.researchCalls).toBe(0);
      expect(aiRequests).toEqual([]);
    } else expect(report.executorStatus).toMatchObject({ state: 'awaiting_claim', source: 'queue' });

    await page.goto('/manage/upload');
    await page.getByRole('button', { name: '清理已结束记录' }).click();
    await expect(rows).toHaveCount(0);
    expect((await (await page.request.get(reportURL)).json()).inputs).toEqual(report.inputs);
  });
}
