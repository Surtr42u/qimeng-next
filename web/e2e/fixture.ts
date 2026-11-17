import type { Page, Route } from '@playwright/test';
import type { components } from '../src/api/generated/schema';
type Asset = components['schemas']['Asset'];
export function syntheticAsset(index: number): Asset {
  return { id: `synthetic-${index}`, libraryId: 'synthetic-library', fileName: `合成场景 ${index}.jpg`, originalFileName: `合成场景 ${index}.jpg`, relativePath: `合成场景 ${index}.jpg`, mediaType: 'image', mimeType: 'image/jpeg', size: 1024, hash: 'synthetic-hash', revision: 1, tagsRevision: 1, authorsRevision: 1, contentVersion: 1, authorId: 'synthetic-author', authorIds: ['synthetic-author'], authorName: '合成作者', authorStatus: 'confirmed', tags: ['晨光'], favorite: false, liked: false, likeCount: 0, note: '', createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', contentUrl: `/api/v1/assets/synthetic-${index}/content`, thumbnailUrl: `/api/v1/assets/synthetic-${index}/thumbnail`, probeState: 'ready', trashed: false, positionMs: 0, positionContentVersion: 1 };
}
export async function mockLibrary(page: Page) {
  const weights = { tagRelevance: .15, tagCollection: .15, engagement: .1, recency: .1, likeScore: .1, discovery: .1, freshness: .1, browseDepth: .1, maxRandom: .1 };
  const state = { assets: Array.from({ length: 72 }, (_, index) => syntheticAsset(index)), assetError: false, tagConflict: false, tagBodies: [] as { expectedRevision: number; tags: string[] }[], requests: [] as string[], eventBodies: [] as components['schemas']['EventCreate'][], weights, preferenceRevision: 1, holdPreference: false, releasePreference: undefined as (() => void) | undefined };
  const respond = (route: Route, body: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()); const path = url.pathname.slice('/api/v1'.length); const method = route.request().method(); state.requests.push(`${method} ${path}`);
    if (path === '/instance') return respond(route, { id: 'synthetic-instance', name: '合成验证影库', apiVersion: '0.1.0', capabilities: { aiConfigured: false, recommendations: true, rankings: true, stats: true, backups: true, migration: false } });
    if (path === '/sessions/current') return respond(route, { id: 'synthetic-session', label: '云端验证浏览器', scopes: ['admin'], expiresAt: '2027-01-01T00:00:00Z' });
    if (path === '/settings') return respond(route, { maxChunkBytes: 8 * 1024 * 1024, maxActiveUploads: 4, maxBatchItems: 100, sourceCleanupEnabled: false });
    if (path === '/libraries') return respond(route, { items: [{ id: 'synthetic-library', name: '合成媒体库', kind: 'regular', enabled: true, revision: 1, createdAt: '2026-01-01T00:00:00Z' }] });
    if (path === '/tags') return respond(route, method === 'POST' ? { name: route.request().postDataJSON().name, revision: 1, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' } : { items: ['晨光', '星空', '海岸'] });
    if (path === '/authors' || path === '/authors/synthetic-author') { const author = { id: 'synthetic-author', name: '合成作者', kind: 'regular', biography: '只用于云端验证的合成档案。', aliases: [], revision: 1, placeholder: false, following: false, fieldStatus: {}, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' }; return respond(route, path === '/authors' ? { items: [author] } : author); }
    if (['/collections', '/batches', '/history', '/trash'].includes(path)) return respond(route, { items: [] });
    if (path === '/stats') return respond(route, { assets: state.assets.length, images: state.assets.length, videos: 0, bytes: 73728, favorites: 0, authors: 1, events: 0, trash: 0 });
    if (path === '/preferences/recommendation') {
      if (method === 'PUT') { const body = route.request().postDataJSON(); if (state.holdPreference) await new Promise<void>(resolve => { state.releasePreference = resolve; }); state.weights = body.weights; state.preferenceRevision++; }
      return respond(route, { revision: state.preferenceRevision, weights: state.weights, presets: { balanced: weights, explore: { ...weights, discovery: .3 } } });
    }
    if (path === '/recommendations') { const offset = Number(url.searchParams.get('cursor') ?? 0); const limit = Number(url.searchParams.get('limit') ?? 48); return respond(route, { items: state.assets.slice(offset, offset + limit), snapshotId: 'synthetic-snapshot', ...(offset + limit < state.assets.length ? { nextCursor: String(offset + limit) } : {}) }); }
    if (path === '/rankings') return respond(route, { items: state.assets.slice(0, 30).map((asset, index) => ({ asset, score: 100 - index })) });
    if (path === '/stats/overview') return respond(route, { range: url.searchParams.get('range'), inventory: { assets: 72, images: 72, videos: 0, bytes: 73728, favorites: 0, authors: 1, events: 1, trash: 0 }, totals: { views: 12, plays: 0, seconds: 120 }, activeDays: 2, viewedAssets: 5, viewedAuthors: 1, viewedTags: 1, averageViewsPerFile: 2.4, mediaTypes: [{ key: 'image', views: 12, plays: 0, seconds: 120, assets: 72, bytes: 73728 }], sources: [{ key: 'regular', views: 12, plays: 0, seconds: 120, assets: 72, bytes: 73728 }] });
    if (path === '/stats/trends') return respond(route, { range: url.searchParams.get('range'), granularity: 'day', items: [{ start: '2026-01-01T00:00:00Z', end: '2026-01-02T00:00:00Z', label: '01-01', views: 12, plays: 0, seconds: 120 }] });
    if (path === '/stats/review') return respond(route, { range: url.searchParams.get('range'), firstViews: 8, returnViews: 4, distinctAssets: 5, activeDays: 2, comparisonAvailable: true, hours: Array.from({ length: 24 }, (_, hour) => ({ hour, views: hour === 10 ? 12 : 0, seconds: hour === 10 ? 120 : 0, imageSeconds: hour === 10 ? 120 : 0, animatedSeconds: 0, videoSeconds: 0 })), changes: [{ kind: 'tag', id: 'synthetic-tag', name: '晨光', currentShare: 50, previousShare: 25, changePoints: 25, currentViews: 6, previousViews: 3 }] });
    if (path === '/assets') {
      if (state.assetError) return respond(route, { code: 'unavailable', message: '合成服务暂不可用' }, 503);
      let assets = state.assets; const q = url.searchParams.get('q'); if (q) assets = assets.filter(asset => asset.fileName.includes(q)); if (url.searchParams.get('favorite') === 'true') assets = assets.filter(asset => asset.favorite);
      const offset = Number(url.searchParams.get('cursor') ?? 0); const limit = Number(url.searchParams.get('limit') ?? 48);
      return respond(route, { items: assets.slice(offset, offset + limit), ...(offset + limit < assets.length ? { nextCursor: String(offset + limit) } : {}) });
    }
    const match = path.match(/^\/assets\/(synthetic-\d+)(?:\/(.*))?$/);
    if (match) {
      const asset = state.assets.find(item => item.id === match[1]); if (!asset) return respond(route, { code: 'not_found', message: '不存在' }, 404);
      if (match[2] === 'content' || match[2] === 'thumbnail') return route.fulfill({ contentType: 'image/svg+xml', body: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 240 320"><defs><linearGradient id="g" x2="1" y2="1"><stop stop-color="#5c4881"/><stop offset="1" stop-color="#2d7377"/></linearGradient></defs><rect width="240" height="320" fill="url(#g)"/><circle cx="150" cy="100" r="48" fill="#d8c8ee" opacity=".55"/><path d="m0 300 80-160 80 95 80-45v130H0" fill="#252237"/><text x="16" y="30" fill="#eee" font-size="12">SYNTHETIC ${asset.id}</text></svg>` });
      if (match[2] === 'events') { const body = route.request().postDataJSON(); state.eventBodies.push(body); return respond(route, { ...body, assetId: asset.id, createdAt: '2026-01-01T00:00:00Z' }); }
      if (match[2] === 'tags') {
        const body = route.request().postDataJSON(); state.tagBodies.push(body);
        if (state.tagConflict) return respond(route, { code: 'revision_conflict', message: '另一设备已修改标签', currentRevision: 2 }, 409);
        asset.tags = body.tags; asset.tagsRevision++; return respond(route, asset);
      }
      if (match[2] === 'reaction') { const body = route.request().postDataJSON(); asset.favorite = body.favorite; if (body.liked && !asset.liked) asset.likeCount++; asset.liked ||= body.liked; return respond(route, asset); }
      if (!match[2] && method === 'GET') return respond(route, asset);
    }
    return respond(route, { code: 'fixture_unhandled', message: `测试夹具尚未实现 ${method} ${path}` }, 501);
  });
  return state;
}
