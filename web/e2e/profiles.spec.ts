import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import type { components } from '../src/api/generated/schema';
import { mockLibrary } from './fixture';

type Schema<K extends keyof components['schemas']> = components['schemas'][K];

async function mockProfiles(page: Page) {
  await mockLibrary(page);
  const stamp = '2026-01-01T00:00:00Z';
  const profile: Schema<'AuthorProfile'> = {
    authorId: 'synthetic-author', editable: true,
    blocks: [
      { authorId: 'synthetic-author', block: 'x', revision: 1, values: { biographyOriginal: 'Synthetic original', biographyZh: '合成译文' }, evidence: [], source: 'human', updatedAt: stamp },
      { authorId: 'synthetic-author', block: 'basic', revision: 1, values: { summary: '已确认简介', note: '已有备注' }, evidence: [], source: 'human', updatedAt: stamp },
      { authorId: 'synthetic-author', block: 'accounts', revision: 0, values: {}, evidence: [], source: 'empty', updatedAt: null },
    ],
  };
  const candidate: Schema<'AuthorProfileCandidate'> = {
    id: 'synthetic-candidate', authorId: 'synthetic-author', block: 'basic', baseRevision: 1,
    values: { summary: '候选提出的简介' }, state: 'pending', createdAt: stamp,
    evidence: [{ url: 'https://example.org/synthetic-author', title: '合成来源', fields: ['summary'], status: 'candidate', note: '仅用于云端测试' }],
  };
  const state = { profile, candidate, conflict: false, writes: [] as Schema<'AuthorProfileWrite'>[] };
  await page.route('**/api/v1/authors/synthetic-author/profile**', async route => {
    const url = new URL(route.request().url());
    const method = route.request().method();
    const send = (body: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
    if (url.pathname.endsWith('/profile')) return send(profile);
    if (url.pathname.endsWith('/candidates')) return send({ items: [candidate] });
    if (url.pathname.endsWith('/archives')) return send({ items: [] });
    if (method === 'PATCH') {
      const block = profile.blocks.find(item => url.pathname.endsWith(`/${item.block}`))!;
      const body = route.request().postDataJSON() as Schema<'AuthorProfileWrite'>;
      state.writes.push(body);
      if (state.conflict) {
        state.conflict = false;
        block.revision++;
        block.values.note = '另一个设备更新的备注';
        return send({ code: 'revision_conflict', message: '另一设备已修改这份资料。', currentRevision: block.revision }, 409);
      }
      if (body.expectedRevision !== block.revision) return send({ code: 'revision_conflict', message: '当前修订已变化', currentRevision: block.revision }, 409);
      if (body.values.biographyOriginal !== undefined && body.values.biographyOriginal !== block.values.biographyOriginal && body.values.biographyZh === undefined) block.values.biographyZh = '';
      block.values = { ...block.values, ...body.values };
      if (body.evidence) block.evidence = body.evidence;
      block.revision++;
      if (body.candidateId) { candidate.state = 'applied'; candidate.appliedRevision = block.revision; }
      return send(block);
    }
    return send({ code: 'not_found', message: '合成夹具未声明此请求' }, 404);
  });
  return state;
}

test('profile conflict retains draft and rebases only explicitly edited fields', async ({ page }) => {
  const state = await mockProfiles(page);
  await page.goto('/authors/synthetic-author');
  await page.getByRole('button', { name: '编辑整理简介与备注', exact: true }).click();
  await page.getByLabel('整理简介', { exact: true }).fill('本次人工草稿');
  state.conflict = true;
  await page.getByRole('button', { name: '保存资料', exact: true }).click();
  await expect(page.getByLabel('整理简介', { exact: true })).toHaveValue('本次人工草稿');
  await page.getByRole('button', { name: '读取当前资料并对照' }).click();
  await expect(page.getByText('服务器当前修订 2', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: '已核对，保留本次改动并采用当前版本' }).click();
  await expect(page.getByLabel('私人备注', { exact: true })).toHaveValue('另一个设备更新的备注');
  await page.getByRole('button', { name: '保存资料', exact: true }).click();
  await expect(page.getByRole('button', { name: '编辑整理简介与备注', exact: true })).toBeVisible();
  expect(state.writes.at(-1)?.values).toEqual({ summary: '本次人工草稿' });
  expect(state.profile.blocks[1].values.note).toBe('另一个设备更新的备注');
});

test('candidate fills a reviewable draft and does not save before explicit confirmation', async ({ page }) => {
  const state = await mockProfiles(page);
  await page.goto('/authors/synthetic-author');
  await page.getByRole('button', { name: '资料候选与处理记录' }).click();
  await page.getByRole('button', { name: '填入草稿并核对' }).click();
  await expect(page.getByLabel('整理简介', { exact: true })).toHaveValue('候选提出的简介');
  expect(state.writes).toHaveLength(0);
  await expect(page.getByRole('button', { name: '保存资料', exact: true })).toBeDisabled();
  await page.getByLabel('我已对照当前资料与候选，确认保存这份草稿').check();
  await page.getByRole('button', { name: '保存资料', exact: true }).click();
  await expect(page.getByText('已保存为修订 2', { exact: true })).toBeVisible();
  expect(state.writes[0].candidateId).toBe('synthetic-candidate');
  expect(state.profile.blocks[1].values.note).toBe('已有备注');
});

test('X original and translation remain distinct and changing the original clears unreviewed translation', async ({ page }) => {
  const state = await mockProfiles(page);
  await page.goto('/authors/synthetic-author');
  await expect(page.locator('.profile-copy').getByText('合成译文', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: '查看 X 原文' }).click();
  await expect(page.locator('.profile-copy').getByText('Synthetic original', { exact: true })).toBeVisible();
  await page.locator('.profile-block').filter({ hasText: 'X 公开资料' }).locator('summary').first().click();
  await page.getByRole('button', { name: '编辑X 公开资料' }).click();
  await page.getByLabel('X 简介原文', { exact: true }).fill('Changed original');
  await page.getByRole('button', { name: '保存资料', exact: true }).click();
  await expect(page.getByRole('button', { name: '查看 X 原文' })).toHaveCount(0);
  expect(state.profile.blocks[0].values.biographyZh).toBe('');
});
