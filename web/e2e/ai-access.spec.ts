import { test, expect } from '@playwright/test';
import type { components } from '../src/api/generated/schema';
import { mockLibrary } from './fixture';

test('AI setup defaults to read only, shows the credential once and revokes the persistent grant', async ({ page }) => {
  await mockLibrary(page);
  type Delegation = components['schemas']['AIDelegation'];
  let record: Delegation | undefined;
  const syntheticSecret = 'synthetic-ci-only-never-a-real-ai-credential-000000000';
  await page.route('**/api/v1/ai/delegations**', async route => {
    const requestedState = new URL(route.request().url()).searchParams.get('state') ?? 'active';
    const method = route.request().method();
    if (method === 'POST') {
      const policy = route.request().postDataJSON();
      record = { ...policy, id: 'synthetic-delegation', state: 'active', createdAt: '2026-01-01T00:00:00Z', expiresAt: '2026-02-01T00:00:00Z' };
      return route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify({ delegation: record, credential: { id: record!.id, label: record!.label, scopes: ['read', 'delegate'], token: syntheticSecret, expiresAt: record!.expiresAt } }) });
    }
    if (method === 'DELETE') { record = { ...record!, state: 'revoked' }; return route.fulfill({ status: 204 }); }
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ items: record && (requestedState === 'all' || requestedState === record.state) ? [record] : [] }) });
  });
  await page.goto('/settings/ai');
  await expect(page.getByText('所有接入都能读取全库资料。', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: '添加 AI 接入' }).click();
  await page.getByLabel('接入名称').fill('合成执行器');
  await expect(page.getByLabel('接收入库文件')).not.toBeChecked();
  await expect(page.getByLabel('研究未知身份与来源')).not.toBeChecked();
  await page.getByRole('button', { name: '下一步', exact: true }).click();
  await page.getByRole('button', { name: '下一步', exact: true }).click();
  await page.getByRole('button', { name: '建立此范围的接入' }).click();
  await expect(page.getByLabel('一次性显示的接入凭据')).toHaveValue(syntheticSecret);
  expect(record?.canIngest).toBe(false);
  expect(record?.canResearchAssets).toBe(false);
  expect(record?.researchProviders).toEqual([]);
  expect(record?.assetFields).toEqual([]);
  expect(record?.candidateBlocks).toEqual([]);
  expect(record?.allAuthors).toBe(false);
  const storage = await page.evaluate(() => [JSON.stringify(localStorage), JSON.stringify(sessionStorage)].join('\n'));
  expect(storage).not.toContain(syntheticSecret);
  await page.getByRole('button', { name: '已保存到执行器，清除本页凭据' }).click();
  await expect(page.getByLabel('一次性显示的接入凭据')).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole('heading', { name: '合成执行器', exact: true })).toBeVisible();
  await expect(page.getByLabel('一次性显示的接入凭据')).toHaveCount(0);
  await page.getByRole('button', { name: '撤销此接入' }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: '确认', exact: true }).click();
  await page.getByLabel('接入状态').selectOption('revoked');
  await expect(page.getByRole('article').filter({ has: page.getByRole('heading', { name: '合成执行器', exact: true }) }).getByText('已撤销', { exact: true })).toBeVisible();
});

test('public research sends only explicitly checked deployed text sources with a positive budget', async ({ page }) => {
  await mockLibrary(page);
  await page.route('**/api/v1/research/context', route => route.fulfill({ json: { enabled: true, allowedProviders: [], providers: [
    { name: 'fxtwitter', operations: ['author', 'post'], evidenceKind: 'platform_record' },
    { name: 'steam', operations: ['workshop'], evidenceKind: 'platform_record' },
  ] } }));
  await page.route('**/api/v1/ai/delegations**', route => {
    if (route.request().method() !== 'POST') return route.fulfill({ json: { items: [] } });
    const body = route.request().postDataJSON();
    return route.fulfill({ status: 201, json: { delegation: { ...body, id: 'fixture-grant', state: 'active' }, credential: { token: 'fixture-only-text-grant' } } });
  });
  await page.goto('/settings/ai');
  await page.getByRole('button', { name: '添加 AI 接入' }).click();
  await page.getByLabel('接入名称').fill('合成外部文字核验');
  await page.getByLabel('研究未知身份与来源').check();
  await page.getByRole('button', { name: '下一步' }).click();
  const sources = page.getByRole('group', { name: '允许发送文字查询的公开来源' });
  await expect(sources.getByRole('checkbox')).toHaveCount(2);
  const fx = sources.getByLabel('FxTwitter', { exact: false });
  const steam = sources.getByLabel('Steam 创意工坊');
  await expect(fx).not.toBeChecked();
  await expect(steam).not.toBeChecked();
  await steam.check();
  await page.getByLabel('每任务研究调用上限').fill('0');
  await expect(steam).not.toBeChecked();
  await expect(steam).toBeDisabled();
  await page.getByLabel('每任务研究调用上限').fill('2');
  await fx.check();
  await page.getByRole('button', { name: '下一步' }).click();
  await expect(page.getByText('FxTwitter（X 公开账号与帖子）', { exact: true })).toBeVisible();
  const submitted = page.waitForRequest(r => r.method() === 'POST' && new URL(r.url()).pathname.endsWith('/ai/delegations'));
  await page.getByRole('button', { name: '建立此范围的接入' }).click();
  expect((await submitted).postDataJSON()).toMatchObject({ canResearch: true, maxResearchCalls: 2, researchProviders: ['fxtwitter'] });
});
