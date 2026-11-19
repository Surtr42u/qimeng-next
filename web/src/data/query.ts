import { QueryClient } from "@tanstack/react-query";
import { ApiError } from "./client";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      gcTime: 10 * 60_000,
      refetchOnWindowFocus: true,
      retry: (attempt, error) =>
        attempt < 1 && (!(error instanceof ApiError) || error.status >= 500),
    },
    mutations: { retry: false },
  },
});
export async function invalidateLibrary() {
  await queryClient.invalidateQueries({ queryKey: ["remote"] });
}

export function invalidatePlaybackDetail(instance: string, assetId: string) {
  const details = queryClient.getQueryCache().findAll({
    queryKey: ["remote", instance],
    predicate: query => query.queryKey[3] === "asset" && query.queryKey[4] === assetId,
  });
  for (const detail of details) {
    const filter = { queryKey: detail.queryKey, exact: true };
    if (detail.state.fetchStatus === "fetching") {
      // A GET already in flight may have read the position before Go accepted this event.
      void queryClient.cancelQueries(filter).then(() => queryClient.invalidateQueries(filter));
    } else {
      // Keep a playing detail mounted. Its next visit reads the authoritative position.
      void queryClient.invalidateQueries({ ...filter, refetchType: "none" });
    }
  }
}
