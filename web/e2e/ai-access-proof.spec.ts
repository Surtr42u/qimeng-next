import { expect, test } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import type { components } from '../src/api/generated/schema';
import { mockLibrary } from './fixture';

type Grant = components['schemas']['AIDelegation'];
test.use({ locale: 'zh-CN', timezoneId: 'Asia/Shanghai', deviceScaleFactor: 1, screenshot: 'off', trace: 'off' });

const archived: Grant = {
  id: 'sample-archived', label: '公开图录资料核对（合成）', state: 'revoked',
  createdAt: '2026-07-01T00:00:00Z', expiresAt: '2026-07-31T00:00:00Z',
  libraryIds: ['synthetic-library'], authorIds: [], allAuthors: false,
  allowedFields: [], candidateBlocks: [], allowClear: false, canIngest: true,
  canOrganize: false, canResearch: true, researchProviders: ['steam'], canLookupImages: true,
  maxResearchCalls: 2, maxUpdates: 1, maxTasksPerDay: 5, validDays: 30, maxCallsPerTask: 100, taskSeconds: 3600,
};

async function connect(page: Page) {
  await mockLibrary(page);
  const history: Grant[] = [archived, { ...archived, id: 'sample-expired', state: 'expired',
    label: 'SyntheticPublicArchiveCatalogIllustrationBibliographyResearchWorkspaceForExhibit' }];
  const secret = crypto.randomUUID();
  await page.route('**/api/v1/research/context', route => route.fulfill({ json: {
    enabled: true, allowedProviders: [], providers: [{ name: 'steam', operations: ['workshop'], evidenceKind: 'platform_record' }],
  } }));
  await page.route('**/api/v1/ai/delegations**', route => {
    if (route.request().method() === 'POST') {
      const policy = route.request().postDataJSON() as components['schemas']['AIDelegationPolicy'];
      const entry: Grant = { ...policy, id: 'sample-active', state: 'active',
        createdAt: '2026-11-19T00:00:00Z', expiresAt: '2026-12-26T00:00:00Z' };
      history.unshift(entry);
      return route.fulfill({ status: 201, json: { delegation: entry,
        credential: { id: entry.id, label: entry.label, scopes: ['read', 'delegate'], expiresAt: entry.expiresAt, token: secret } } });
    }
    const filter = new URL(route.request().url()).searchParams.get('state') ?? 'active';
    return route.fulfill({ json: { items: history.filter(entry => filter === 'all' || entry.state === filter) } });
  });
  return secret;
}

async function layout(page: Page) {
  return page.locator('[data-scroll-root]').evaluate(main => {
    const box = main.getBoundingClientRect();
    const left = box.left + main.clientLeft;
    const right = left + main.clientWidth;
    const elementsOutside: string[] = [];
    const textClipped: string[] = [];
    const closedDetail = (element: Element) => {
      for (let parent = element.closest('details:not([open])'); parent; parent = parent.parentElement?.closest('details:not([open])') ?? null) {
        if (!parent.querySelector(':scope > summary')?.contains(element)) return true;
      }
      return false;
    };
    for (const el of main.querySelectorAll<HTMLElement>('a,button,select,input,summary,form,fieldset,legend,label,article,h1,h2,h3,h4,dt,dd,p,code')) {
      const rect = el.getBoundingClientRect();
      if (!el.checkVisibility() || closedDetail(el) || rect.width < 2 || rect.height < 2) continue;
      const description = (el.innerText || el.getAttribute('aria-label') || el.tagName).trim().slice(0, 32);
      if (rect.left < left - 1 || rect.right > right + 1) elementsOutside.push(description);
      if (!el.matches('h1,h2,h3,h4,p,dt,dd,legend,button,label,summary,code')) continue;
      const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        const parent = node.parentElement;
        if (!node.textContent?.trim() || !parent?.checkVisibility() || closedDetail(parent) || parent.closest('option,select,[hidden],.sr-only')) continue;
        const range = document.createRange();
        range.selectNodeContents(node);
        for (const fragment of range.getClientRects()) {
          if (!fragment.width || !fragment.height) continue;
          let clipped = fragment.left < left - 1 || fragment.right > right + 1;
          for (let owner: HTMLElement | null = parent; owner && owner !== main; owner = owner.parentElement) {
            const style = getComputedStyle(owner);
            if (!['hidden', 'clip'].includes(style.overflowX) && !['hidden', 'clip'].includes(style.overflowY)) continue;
            const boundary = owner.getBoundingClientRect();
            if (boundary.width <= 1 || boundary.height <= 1) continue;
            if (['hidden', 'clip'].includes(style.overflowX) && (fragment.left < boundary.left - 2 || fragment.right > boundary.right + 2)) clipped = true;
            if (['hidden', 'clip'].includes(style.overflowY) && (fragment.top < boundary.top - 3 || fragment.bottom > boundary.bottom + 3)) clipped = true;
          }
          if (clipped) textClipped.push(node.textContent.trim().slice(0, 32));
        }
      }
    }
    return { viewport: [innerWidth, innerHeight], mainLeft: left, mainTop: box.top, mainWidth: main.clientWidth,
      mainHeight: main.clientHeight, scrollTop: main.scrollTop, scrollHeight: main.scrollHeight,
      mainHorizontalOverflow: main.scrollWidth - main.clientWidth,
      documentHorizontalOverflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
      elementsOutside: [...new Set(elementsOutside)], textClipped: [...new Set(textClipped)] };
  });
}

async function choiceGeometry(input: Locator, name: string) {
  return input.evaluate((element, text) => {
    const label = (element as HTMLInputElement).labels?.[0];
    if (!label) throw new Error('Checkbox has no clickable label');
    const walker = document.createTreeWalker(label, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      if (!node.textContent?.includes(text)) continue;
      const range = document.createRange();
      range.selectNodeContents(node);
      const line = range.getClientRects()[0]!;
      const box = element.getBoundingClientRect();
      return { width: box.width, height: box.height, textHeight: line.height, labelWidth: label.getBoundingClientRect().width,
        startsBesideInput: line.left >= box.right - 1, sameLine: line.top < box.bottom && line.bottom > box.top };
    }
    throw new Error('Checkbox label text missing');
  }, name);
}

for (const width of [1440, 390]) {
  test(`native AI access at ${width} CSS pixels`, async ({ page, browser }, testInfo) => {
    test.setTimeout(60_000);
    const height = width === 1440 ? 800 : 844;
    const theme = width === 1440 ? 'light' : 'dark';
    const output = process.env.AI_LAYOUT_EVIDENCE ?? testInfo.outputDir;
    await mkdir(output, { recursive: true });
    const secret = await connect(page);
    await page.setViewportSize({ width, height });
    await page.goto('/settings');
    await expect(page.getByRole('heading', { name: '设置', exact: true })).toBeVisible();
    if (await page.locator('html').getAttribute('data-theme') !== theme) await page.getByRole('button', { name: '明暗主题' }).click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    const reports: Record<string, unknown> = {};
    await expect(page.getByRole('link', { name: 'AI 接入', exact: true })).toBeVisible();
    await page.evaluate(() => document.fonts.ready);
    const fontClient = await page.context().newCDPSession(page);
    await fontClient.send('DOM.enable');
    await fontClient.send('CSS.enable');
    const { root } = await fontClient.send('DOM.getDocument');
    const { nodeId } = await fontClient.send('DOM.querySelector', { nodeId: root.nodeId, selector: 'main h1' });
    reports.headingFonts = (await fontClient.send('CSS.getPlatformFontsForNode', { nodeId })).fonts;
    await fontClient.detach();
    const writeReport = () => writeFile(join(output, `${width}-measurements.json`), JSON.stringify({
      applicationCommit: process.env.APP_SHA, proofCommit: process.env.GITHUB_SHA, os: process.platform,
      browser: browser.version(), viewport: { width, height }, theme, states: reports,
    }, null, 2));
    const capture = async (name: string, anchor?: Locator) => {
      if (anchor) await anchor.evaluate(el => el.scrollIntoView({ block: 'start', behavior: 'instant' }));
      else await page.locator('[data-scroll-root]').evaluate(el => el.scrollTo({ top: 0, behavior: 'instant' }));
      await page.evaluate(async () => { await document.fonts.ready; await new Promise<void>(done => requestAnimationFrame(() => requestAnimationFrame(() => done()))); });
      const protectedField = page.getByLabel('一次性显示的接入凭据');
      if (await protectedField.count()) {
        const masked = await protectedField.evaluate(el => {
          const field = el as HTMLInputElement;
          return field.type === 'password' && ['disc', 'circle', 'square'].includes(getComputedStyle(field).getPropertyValue('-webkit-text-security'))
            && !!field.value && !document.body.innerText.includes(field.value);
        });
        expect(masked, 'Only capture the actual browser password mask').toBe(true);
      }
      const measurement = await layout(page);
      reports[name] = measurement;
      await page.screenshot({ path: join(output, `${width}x${height}-${theme}-${name}.png`), animations: 'disabled' });
      await writeReport();
      if (name !== 'settings') {
        expect.soft(measurement.mainHorizontalOverflow, name).toBeLessThanOrEqual(1);
        expect.soft(measurement.documentHorizontalOverflow, name).toBeLessThanOrEqual(1);
        expect.soft(measurement.elementsOutside, name).toEqual([]);
        expect.soft(measurement.textClipped, name).toEqual([]);
      }
    };
    await capture('settings');
    await page.getByRole('link', { name: 'AI 接入', exact: true }).click();
    await expect(page.getByText('尚无有效 AI 接入', { exact: true })).toBeVisible();
    await capture('empty');

    const add = page.getByRole('button', { name: '添加 AI 接入' });
    await add.focus();
    await page.keyboard.press('Enter');
    const form = page.locator('form').filter({ has: page.getByLabel('接入名称') });
    const label = page.getByLabel('接入名称');
    await expect(label).toBeFocused();
    await label.fill('公开图录执行器（合成）');
    const research = page.getByLabel('研究未知身份与来源');
    await expect(research).not.toBeChecked();
    const geometry = await choiceGeometry(research, '研究未知身份与来源');
    reports.checkbox = geometry;
    expect.soft(geometry.startsBesideInput && geometry.sameLine).toBe(true);
    expect.soft(geometry.width).toBeLessThanOrEqual(geometry.height * 1.5);
    expect.soft(geometry.height).toBeLessThanOrEqual(geometry.textHeight + 8);
    const choice = research.locator('xpath=ancestor::label[1]');
    const choiceBox = (await choice.boundingBox())!;
    await choice.click({ position: { x: choiceBox.width - 6, y: choiceBox.height / 2 } });
    await expect(research).toBeChecked();
    await capture('actions', form);
    const next = page.getByRole('button', { name: '下一步', exact: true });
    await next.focus();
    await page.keyboard.press('Enter');
    const budget = page.getByLabel('每任务研究调用上限');
    await expect(budget).toBeVisible();
    await expect(page.getByRole('heading', { name: '第 2 步，范围与预算' })).toBeFocused();
    await page.getByLabel('授权有效天数').fill('37');
    await page.getByLabel('Steam 创意工坊').check();
    await capture('scope', page.locator('form').filter({ has: budget }));
    await capture('budget', page.getByLabel('授权有效天数'));
    await next.focus();
    await page.keyboard.press('Enter');
    const create = page.getByRole('button', { name: '建立此范围的接入' });
    const confirmation = page.locator('form').filter({ has: create });
    await expect(page.getByRole('heading', { name: '第 3 步，核对并建立' })).toBeFocused();
    await expect(confirmation.getByText('Steam 创意工坊', { exact: true })).toBeVisible();
    await expect(confirmation.getByText(/37\s*天/)).toBeVisible();
    await capture('review', confirmation);
    await create.focus();
    await page.keyboard.press('Enter');

    const credential = page.getByLabel('一次性显示的接入凭据');
    await expect(credential).toHaveAttribute('type', 'password');
    expect(await credential.inputValue() === secret).toBe(true);
    const result = page.getByRole('region', { name: '新接入凭据' });
    await expect(result.getByRole('heading')).toBeFocused();
    await capture('masked', result);
    await page.getByRole('link', { name: '返回设置' }).click();
    await page.getByRole('alertdialog').getByRole('button', { name: '取消', exact: true }).click();
    await expect(credential).toHaveAttribute('type', 'password');
    await page.getByRole('button', { name: '已保存到执行器，清除本页凭据' }).click();
    await expect(credential).toHaveCount(0);
    await expect(add).toBeFocused();

    await page.getByLabel('接入状态').selectOption('all');
    const row = page.getByRole('article').filter({ has: page.getByRole('heading', { name: archived.label, exact: true }) });
    await expect(row.getByText('已撤销', { exact: true })).toBeVisible();
    const longHeading = page.getByRole('heading', { name: /^SyntheticPublicArchive/ });
    reports.longName = await longHeading.evaluate(el => {
      const text = document.createRange();
      text.selectNodeContents(el);
      const outer = el.getBoundingClientRect();
      const fragments = Array.from(text.getClientRects()).filter(box => box.width > 0);
      return { characters: el.textContent?.length, renderedLines: new Set(fragments.map(box => box.top)).size,
        headingWidth: outer.width, headingHeight: outer.height, wrapsInsideHeading: fragments.every(box => box.left >= outer.left - 1 && box.right <= outer.right + 1 && box.top >= outer.top - 3 && box.bottom <= outer.bottom + 3) };
    });
    await capture('records');
    const disclosure = row.locator('summary').filter({ hasText: '权限详情' });
    const provider = row.getByText('Steam 创意工坊', { exact: true });
    await expect(provider).toBeHidden();
    await disclosure.focus();
    await page.keyboard.press('Enter');
    await expect(provider).toBeVisible();
    await capture('permissions', row);
    await page.keyboard.press('Space');
    await expect(provider).toBeHidden();
    await expect(disclosure).toBeFocused();
    await page.getByRole('link', { name: '返回设置' }).click();
    await expect(page).toHaveURL(/\/settings$/);
    reports.keyboard = 'opened wizard, advanced using Enter, canceled navigation while masked, returned focus after clear, opened and closed historical disclosure, returned to Settings';
    await writeReport();
  });
}
