import { test, expect } from '@playwright/test';
import type { components } from '../src/api/generated/schema';
import { mockLibrary } from './fixture';

type Delegation = components['schemas']['AIDelegation'];

// A failed masking assertion must not publish a screenshot or trace of the field.
test.use({ screenshot: 'off', trace: 'off' });

test('AI setup defaults to read only, shows the credential once and revokes the persistent grant', async ({ page }) => {
  await mockLibrary(page);
  let record: Delegation | undefined;
  const syntheticSecret = crypto.randomUUID();
  let posts = 0;
  await page.route('**/api/v1/ai/delegations**', async route => {
    const requestedState = new URL(route.request().url()).searchParams.get('state') ?? 'active';
    const method = route.request().method();
    if (method === 'POST') {
      posts++;
      const policy = route.request().postDataJSON();
      record = { ...policy, id: 'synthetic-delegation', state: 'active', createdAt: '2026-01-01T00:00:00Z', expiresAt: '2026-02-01T00:00:00Z' };
      return route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify({ delegation: record, credential: { id: record!.id, label: record!.label, scopes: ['read', 'delegate'], token: syntheticSecret, expiresAt: record!.expiresAt } }) });
    }
    if (method === 'DELETE') { record = { ...record!, state: 'revoked' }; return route.fulfill({ status: 204 }); }
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ items: record && (requestedState === 'all' || requestedState === record.state) ? [record] : [] }) });
  });
  await page.goto('/settings/ai');
  await expect(page.getByText('所有接入都能读取全库资料。', { exact: false })).toBeVisible();
  const add = page.getByRole('button', { name: '添加 AI 接入' });
  await add.click();
  const label = page.getByLabel('接入名称');
  await expect(label).toBeFocused();
  await label.fill('合成执行器');
  await expect(page.getByLabel('接收入库文件')).not.toBeChecked();
  await expect(page.getByLabel('研究未知身份与来源')).not.toBeChecked();
  const create = page.getByRole('button', { name: '创建接入凭据' });
  await expect(page.getByRole('region', { name: '本次授权' })).toContainText('仅全库读取，不允许写入或向外部研究服务发送资料。有效期 30 天。');
  await expect(page.getByRole('button', { name: '下一步' })).toHaveCount(0);
  await expect(page.getByLabel('每日最多任务')).not.toBeVisible();
  await label.fill('字'.repeat(27));
  await expect(create).toBeDisabled();
  await label.fill('合成执行器');
  expect(posts).toBe(0);
  await create.click();
  const credential = page.getByLabel('一次性显示的接入凭据');
  await expect(credential).toHaveAttribute('type', 'password');
  expect(await credential.inputValue() === syntheticSecret).toBe(true);
  expect(posts).toBe(1);
  expect(record).toMatchObject({ canIngest: false, canOrganize: false, canRecycleSources: false, maxTasksPerDay: 10, validDays: 30 });
  expect(record?.canResearchAssets).toBe(false);
  expect(record?.canLookupImages).toBe(false);
  expect(record?.researchProviders).toEqual([]);
  expect(record?.assetFields).toEqual([]);
  expect(record?.candidateBlocks).toEqual([]);
  expect(record?.allAuthors).toBe(false);
  const storage = await page.evaluate(() => [JSON.stringify(localStorage), JSON.stringify(sessionStorage)].join('\n'));
  expect(storage.includes(syntheticSecret)).toBe(false);
  await page.getByRole('link', { name: '返回设置' }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: '取消', exact: true }).click();
  await expect(credential).toHaveAttribute('type', 'password');
  await page.getByRole('button', { name: '已保存到执行器，清除本页凭据' }).click();
  await expect(credential).toHaveCount(0);
  await expect(add).toBeFocused();
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
    return route.fulfill({ status: 201, json: { delegation: { ...body, id: 'fixture-grant', state: 'active' }, credential: { token: crypto.randomUUID() } } });
  });
  await page.goto('/settings/ai');
  await page.getByRole('button', { name: '添加 AI 接入' }).click();
  await page.getByLabel('接入名称').fill('合成外部文字核验');
  await page.getByText('研究未知身份与来源', { exact: true }).click();
  await expect(page.getByLabel('研究未知身份与来源')).toBeChecked();
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
  await expect(page.getByRole('region', { name: '本次授权' })).toContainText('FxTwitter（X 公开账号与帖子）');
  const submitted = page.waitForRequest(r => r.method() === 'POST' && new URL(r.url()).pathname.endsWith('/ai/delegations'));
  await page.getByRole('button', { name: '创建接入凭据' }).click();
  expect((await submitted).postDataJSON()).toMatchObject({ canResearch: true, maxResearchCalls: 2, researchProviders: ['fxtwitter'] });
});

test('a narrow history row keeps the entire grant name and opens its permissions from the keyboard', async ({ page }) => {
  await mockLibrary(page);
  await page.setViewportSize({ width: 390, height: 844 });
  const record: Delegation = {
    id: 'synthetic-history', state: 'revoked', label: 'Research'.repeat(10),
    createdAt: '2026-01-01T00:00:00Z', expiresAt: '2026-01-31T00:00:00Z',
    libraryIds: ['synthetic-library'], authorIds: [], allAuthors: false, allowedFields: [], candidateBlocks: [],
    allowClear: false, canIngest: true, canResearch: true, canOrganize: false, canLookupImages: true,
    canResearchAssets: false, canRecycleSources: false,
    maxResearchCalls: 2, maxUpdates: 1, maxTasksPerDay: 10, validDays: 30,
    maxCallsPerTask: 100, taskSeconds: 3600,
  };
  await page.route('**/api/v1/ai/delegations**', route => {
    const state = new URL(route.request().url()).searchParams.get('state');
    return route.fulfill({ json: { items: state === 'revoked' ? [record] : [] } });
  });
  await page.goto('/settings/ai');
  await page.getByLabel('接入状态').selectOption('revoked');
  const heading = page.getByRole('heading', { name: record.label, exact: true });
  const row = page.getByRole('article').filter({ has: heading });
  await expect(row.getByText('已撤销', { exact: true })).toBeVisible();
  expect(await heading.evaluate(element => element.scrollWidth - element.clientWidth)).toBeLessThanOrEqual(1);
  expect(await heading.evaluate(element => element.scrollHeight - element.clientHeight)).toBeLessThanOrEqual(1);
  const disclosure = row.locator('summary').filter({ hasText: '权限详情' });
  const budget = row.getByText('任务预算', { exact: true });
  await expect(budget).toBeHidden();
  await disclosure.focus();
  await page.keyboard.press('Enter');
  await expect(budget).toBeVisible();
  await expect(row.getByText('允许将派生的单帧预览发送至已配置的识图服务', { exact: true })).toBeVisible();
  expect(await page.locator('[data-scroll-root]').evaluate(element => element.scrollWidth - element.clientWidth)).toBeLessThanOrEqual(1);
  await page.keyboard.press('Space');
  await expect(budget).toBeHidden();
  await expect(disclosure).toBeFocused();
});
