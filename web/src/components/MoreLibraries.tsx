import type { useLibraries } from "../data/manage";

export function MoreLibraries({ query }: { query: Pick<ReturnType<typeof useLibraries>, "hasNextPage" | "isFetchingNextPage" | "fetchNextPage"> }) {
  return query.hasNextPage ? <button type="button" className="pill" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage({ cancelRefetch: false })}>{query.isFetchingNextPage ? "读取中…" : "更多媒体库"}</button> : null;
}
