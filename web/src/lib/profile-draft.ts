import type { Schema } from '../data/client';

export type ProfileValues = Schema<'AuthorProfileValues'>;
export const profileFieldLabels: Record<keyof ProfileValues, string> = {
  handle: 'X 账号', profileUrl: 'X 账号主页', displayName: 'X 显示名称', biographyOriginal: 'X 简介原文',
  biographyZh: 'X 简介中文译文', avatarSourceUrl: '头像来源网址',
  publicName: '公开昵称', summary: '整理简介', remark: '特殊情况备注', note: '私人备注', accounts: '公开账号与发布入口',
};
export const profileBlockTitles = { x: 'X 公开资料', basic: '整理简介与备注', accounts: '账号与发布入口' };
export const profileFieldNames = {
  x: ['handle', 'profileUrl', 'displayName', 'biographyOriginal', 'biographyZh', 'avatarSourceUrl'],
  basic: ['publicName', 'summary', 'remark', 'note'],
  accounts: ['accounts'],
} satisfies Record<string, (keyof ProfileValues)[]>;

const equal = (left: unknown, right: unknown) => JSON.stringify(left) === JSON.stringify(right);
const empty = (field: keyof ProfileValues) => field === 'accounts' ? [] : '';

// Omitted fields remain untouched. A defined empty string or array is a
// deliberate clear; comparing against the opened block preserves that intent.
export function profilePatch(base: ProfileValues, draft: ProfileValues): ProfileValues {
  return Object.fromEntries(Object.entries(draft).filter(([name, value]) => {
    const field = name as keyof ProfileValues;
    return !equal(base[field] ?? empty(field), value);
  }));
}

export function rebaseProfileDraft(base: ProfileValues, draft: ProfileValues, current: ProfileValues): ProfileValues {
  return { ...current, ...profilePatch(base, draft) };
}

export function profileAvatarURL(value: string | undefined): string | undefined {
  if (!value) return;
  try {
    const url = new URL(value, window.location.origin);
    if (url.origin === window.location.origin && /^\/api\/v1\/profile-avatars\/[a-f0-9]{64}$/.test(url.pathname) && !url.search && !url.hash) return url.href;
  } catch { /* A malformed URL is shown as unavailable, never fetched externally. */ }
}

export function publicProfileLink(value: string): string | undefined {
  try {
    const url = new URL(value);
    if (['https:', 'http:'].includes(url.protocol) && !url.username && !url.password) return url.href;
  } catch { /* Stored legacy text may not be a usable external link. */ }
}
