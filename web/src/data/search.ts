import { useQuery } from "@tanstack/react-query";
import { api, read } from "./client";
import { useRemoteKey } from "./session";

const historyKey = "qimeng.search-history";

export function readSearchHistory(): string[] {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(historyKey) ?? "[]");
    return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string").slice(0, 20) : [];
  } catch {
    return [];
  }
}

export function writeSearchHistory(items: string[]) {
  localStorage.setItem(historyKey, JSON.stringify(items));
}

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
