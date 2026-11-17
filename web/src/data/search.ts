import { useQuery } from "@tanstack/react-query";
import { api, read } from "./client";
import { useRemoteKey } from "./session";

export function useSearchSuggestions(
  q: string,
  opening: number,
  enabled: boolean,
) {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, "search-suggestions", q, q ? undefined : opening],
    enabled,
    queryFn: ({ signal }) =>
      read(
        api.GET("/search/suggestions", {
          params: { query: { q, recommend: !q, limit: 10 } },
          signal,
        }),
      ),
    staleTime: q ? 30_000 : 0,
  });
}
