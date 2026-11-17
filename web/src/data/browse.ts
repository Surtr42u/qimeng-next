import { useInfiniteQuery } from '@tanstack/react-query';
import type { paths } from '../api/generated/schema';
import { api, read } from './client';
import type { AssetFilters } from './media';
import { useRemoteKey } from './session';

export type FacetQuery = NonNullable<paths['/assets/facets']['get']['parameters']['query']>;
export type FacetRow = FacetQuery['row'];

// Omit only the current row's own selection. A fixed author/favorite/history scope survives.
export function facetFilters(filters: AssetFilters, row: FacetRow): FacetQuery {
  const { dateCounts: _dateCounts, cursor: _cursor, limit: _limit, sort: _sort, order: _order, tag: _tag, ...remaining } = filters;
  const query: FacetQuery = { ...remaining, row };
  if (row === 'partitions') delete query.kind;
  if (row === 'works') delete query.workKeys;
  if (row === 'roles') delete query.roleKeys;
  if (row === 'types') delete query.mediaType;
  return query;
}
export function useAssetFacets(filters: AssetFilters, row: FacetRow) {
  const remote = useRemoteKey();
  const query = facetFilters(filters, row);
  return useInfiniteQuery({
    queryKey: [...remote, 'asset-facets', query],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => read(api.GET('/assets/facets', { params: { query: { ...query, cursor: pageParam, limit: 100 } }, signal })),
    getNextPageParam: page => page.nextCursor || undefined,
  });
}

function choice<T extends string>(value: string | null, values: readonly T[]): T | undefined {
  return values.find(item => item === value);
}
export function filtersFromURL(params: URLSearchParams): AssetFilters {
  const list = (key: string) => { const values = params.getAll(key).filter(Boolean); return values.length ? values : undefined; };
  const year = (key: string) => { const raw = params.get(key); return raw && /^\d{4}$/.test(raw) ? Number(raw) : undefined; };
  return {
    q: params.get('q') || undefined,
    libraryId: params.get('libraryId') || undefined,
    authorId: params.get('authorId') || undefined,
    collectionId: params.get('collectionId') || undefined,
    kind: choice(params.get('kind'), ['regular', 'cos']),
    mediaType: choice(params.get('mediaType'), ['image', 'animated_image', 'video']),
    workKeys: list('workKeys'), roleKeys: list('roleKeys'),
    authorIds: list('authorIds'), sourceIds: list('sourceIds'), characterIds: list('characterIds'), cosWorkIds: list('cosWorkIds'),
    tags: list('tags') ?? list('tag'), tagMode: choice(params.get('tagMode'), ['any', 'all']),
    sort: choice(params.get('sort'), ['default', 'fileDate', 'addedDate', 'views', 'plays', 'size', 'name', 'favorited', 'added_desc', 'updated_desc', 'name_asc']) ?? 'default',
    order: choice(params.get('order'), ['asc', 'desc']) ?? 'desc',
    playRange: choice(params.get('playRange'), ['all', 'none', 'low', 'mid', 'high']),
    sizeRange: choice(params.get('sizeRange'), ['all', 'lt1m', 'm1to10', 'm10to50', 'gt50m']),
    timeRange: choice(params.get('timeRange'), ['all', 'today', 'week', 'month', 'three_months', 'year', 'year_range']),
    fromYear: year('fromYear'), toYear: year('toYear'),
    favorite: params.get('favorite') === 'true' || undefined,
    history: params.get('history') === 'true' || undefined,
  };
}
export const filterURLKeys = ['kind', 'mediaType', 'workKeys', 'roleKeys', 'authorIds', 'sourceIds', 'characterIds', 'cosWorkIds', 'tags', 'tag', 'tagMode', 'sort', 'order', 'playRange', 'sizeRange', 'timeRange', 'fromYear', 'toYear'] as const;
