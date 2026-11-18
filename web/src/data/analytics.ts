import {
  keepPreviousData,
  useInfiniteQuery,
  useMutation,
  useQuery,
} from "@tanstack/react-query";
import { api, read, operationHeaders } from "./client";
import type { Schema } from "./client";
import { useRemoteKey } from "./session";
import { queryClient } from "./query";
import type { paths } from "../api/generated/schema";
import { deduplicateAssetPages } from "./media";
import type { InfiniteData } from "@tanstack/react-query";
import { ApiError } from "../lib/errors";

export type StatsRange = NonNullable<
  paths["/stats/overview"]["get"]["parameters"]["query"]
>["range"];
export type RankingPeriod = NonNullable<
  paths["/rankings"]["get"]["parameters"]["query"]
>["period"];
export function useRecommendations(
  seed: number,
  kind: "regular" | "cos" | undefined,
  mediaType: Schema<"Asset">["mediaType"] | undefined,
  enabled: boolean,
) {
  const key = useRemoteKey();
  const queryKey = [...key, "recommendations", seed, kind, mediaType];
  const query = useInfiniteQuery({
    queryKey,
    placeholderData: keepPreviousData,
    initialPageParam: undefined as string | undefined,
    enabled,
    queryFn: ({ pageParam, signal }) =>
      read(
        api.GET("/recommendations", {
          params: {
            query: { seed, kind, mediaType, cursor: pageParam, limit: 48 },
          },
          signal,
        }),
      ),
    getNextPageParam: (last) => last.nextCursor || undefined,
    staleTime: 60_000,
    select: (data) => ({ ...data, pages: deduplicateAssetPages(data.pages) }),
  });
  const expired = query.error instanceof ApiError && query.error.status === 409 && query.error.code === "recommendation_expired";
  const restart = () => {
    // Retain the first page while requesting a new order without the expired cursor.
    queryClient.setQueryData<InfiniteData<Schema<"RecommendationPage">>>(queryKey, old => old ? { pages: old.pages.slice(0, 1), pageParams: [undefined] } : old);
    return query.refetch();
  };
  return { ...query, expired, restart };
}
export function useRankings(
  period: RankingPeriod,
  enabled: boolean,
  mediaType?: Schema<"Asset">["mediaType"],
) {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, "rankings", period, mediaType],
    placeholderData: keepPreviousData,
    initialPageParam: undefined as string | undefined,
    enabled,
    queryFn: ({ pageParam, signal }) =>
      read(
        api.GET("/rankings", {
          params: {
            query: { period, mediaType, cursor: pageParam, limit: 48 },
          },
          signal,
        }),
      ),
    getNextPageParam: (last) => last.nextCursor || undefined,
  });
}
export function useOverview(range: StatsRange) {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, "overview", range],
    queryFn: ({ signal }) =>
      read(
        api.GET("/stats/overview", { params: { query: { range } }, signal }),
      ),
  });
}
export function useTrends(range: StatsRange) {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, "trends", range],
    queryFn: ({ signal }) =>
      read(api.GET("/stats/trends", { params: { query: { range } }, signal })),
  });
}
export function useReview(range: StatsRange) {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, "review", range],
    queryFn: ({ signal }) =>
      read(api.GET("/stats/review", { params: { query: { range } }, signal })),
  });
}
export type ActivityRankingKind = "authors" | "tags" | "assets";
export function useActivityRanking(
  kind: ActivityRankingKind,
  range: StatsRange,
  metric: "views" | "seconds" = "views",
  limit = 30,
) {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, "activity-ranking", kind, range, metric, limit],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => {
      const query = { range, cursor: pageParam, limit };
      if (kind === "authors")
        return read(
          api.GET("/stats/top-authors", { params: { query }, signal }),
        );
      if (kind === "tags")
        return read(api.GET("/stats/top-tags", { params: { query }, signal }));
      return read(
        api.GET("/stats/most-viewed", {
          params: { query: { ...query, metric } },
          signal,
        }),
      );
    },
    getNextPageParam: (page) => page.nextCursor || undefined,
  });
}
export function useRecommendationPreferences() {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, "recommendation-preferences"],
    queryFn: ({ signal }) =>
      read(api.GET("/preferences/recommendation", { signal })),
  });
}
export function useSaveRecommendationPreferences() {
  const key = useRemoteKey();
  return useMutation({
    mutationFn: async ({
      body,
      operationId,
    }: {
      body: Schema<"RecommendationPreferencesUpdate">;
      operationId: string;
    }) => {
      await queryClient.cancelQueries({
        queryKey: [...key, "recommendation-preferences"],
      });
      return read(
        api.PUT("/preferences/recommendation", {
          params: { header: operationHeaders(operationId) },
          body,
        }),
      );
    },
    onSuccess: (data) => {
      queryClient.setQueryData([...key, "recommendation-preferences"], data);
      void queryClient.invalidateQueries({
        queryKey: [...key, "recommendations"],
      });
    },
  });
}
