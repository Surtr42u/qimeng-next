import { useInfiniteQuery, useMutation, useQuery } from '@tanstack/react-query';
import { api, read, operationHeaders } from './client';
import type { Schema } from './client';
import { useRemoteKey } from './session';
import { queryClient } from './query';
import type { paths } from '../api/generated/schema';

export type StatsRange = NonNullable<paths['/stats/overview']['get']['parameters']['query']>['range'];
export type RankingPeriod = NonNullable<paths['/rankings']['get']['parameters']['query']>['period'];
export function useRecommendations(seed: number, kind: 'regular' | 'cos' | undefined, mediaType: Schema<'Asset'>['mediaType'] | undefined, enabled: boolean) {
  const key = useRemoteKey();
  return useInfiniteQuery({ queryKey: [...key, 'recommendations', seed, kind, mediaType], initialPageParam: undefined as string | undefined, enabled,
    queryFn: ({ pageParam, signal }) => read(api.GET('/recommendations', { params: { query: { seed, kind, mediaType, cursor: pageParam, limit: 48 } }, signal })),
    getNextPageParam: last => last.nextCursor || undefined, staleTime: 60_000,
  });
}
export function useRankings(period: RankingPeriod, enabled: boolean) {
  const key = useRemoteKey();
  return useInfiniteQuery({ queryKey: [...key, 'rankings', period], initialPageParam: undefined as string | undefined, enabled,
    queryFn: ({ pageParam, signal }) => read(api.GET('/rankings', { params: { query: { period, cursor: pageParam, limit: 48 } }, signal })),
    getNextPageParam: last => last.nextCursor || undefined,
  });
}
export function useOverview(range: StatsRange) {
  const key = useRemoteKey(); return useQuery({ queryKey: [...key, 'overview', range], queryFn: ({ signal }) => read(api.GET('/stats/overview', { params: { query: { range } }, signal })) });
}
export function useTrends(range: StatsRange) {
  const key = useRemoteKey(); return useQuery({ queryKey: [...key, 'trends', range], queryFn: ({ signal }) => read(api.GET('/stats/trends', { params: { query: { range } }, signal })) });
}
export function useReview(range: StatsRange) {
  const key = useRemoteKey(); return useQuery({ queryKey: [...key, 'review', range], queryFn: ({ signal }) => read(api.GET('/stats/review', { params: { query: { range } }, signal })) });
}
export function useRecommendationPreferences() {
  const key = useRemoteKey();
  return useQuery({ queryKey: [...key, 'recommendation-preferences'], queryFn: ({ signal }) => read(api.GET('/preferences/recommendation', { signal })) });
}
export function useSaveRecommendationPreferences() {
  const key = useRemoteKey();
  return useMutation({ mutationFn: async ({ body, operationId }: { body: Schema<'RecommendationPreferencesUpdate'>; operationId: string }) => {
    await queryClient.cancelQueries({ queryKey: [...key, 'recommendation-preferences'] });
    return read(api.PUT('/preferences/recommendation', { params: { header: operationHeaders(operationId) }, body }));
  }, onSuccess: data => {
    queryClient.setQueryData([...key, 'recommendation-preferences'], data);
    void queryClient.invalidateQueries({ queryKey: [...key, 'recommendations'] });
  } });
}
