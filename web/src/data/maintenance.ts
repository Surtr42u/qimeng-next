import { useEffect } from "react";
import { useInfiniteQuery, useMutation, useQuery } from "@tanstack/react-query";
import { api, operationHeaders, read } from "./client";
import type { Schema } from "./client";
import { useRemoteKey } from "./session";
import { invalidateLibrary } from "./query";

export function useStartScan() {
  return useMutation({
    mutationFn: ({
      id,
      revision,
      key,
    }: {
      id: string;
      revision: number;
      key: string;
    }) =>
      read(
        api.POST("/libraries/{id}/scans", {
          params: { path: { id }, header: operationHeaders(key) },
          body: { expectedRevision: revision },
        }),
      ),
  });
}
export function useScan(id: string) {
  const key = useRemoteKey();
  const query = useQuery({
    queryKey: [...key, "scan", id],
    queryFn: ({ signal }) =>
      read(api.GET("/scans/{id}", { params: { path: { id } }, signal })),
    refetchInterval: (state) =>
      ["queued", "running"].includes(state.state.data?.state ?? "")
        ? 2000
        : false,
  });
  const state = query.data?.state;
  useEffect(() => {
    if (state && !["queued", "running"].includes(state))
      void invalidateLibrary();
  }, [id, state]);
  return query;
}
export function useCancelScan(id: string) {
  return useMutation({
    mutationFn: (key: string) =>
      read(
        api.DELETE("/scans/{id}", {
          params: { path: { id }, header: operationHeaders(key) },
        }),
      ),
    onSuccess: invalidateLibrary,
  });
}
export function useScans(
  libraryId?: string,
  state?: Schema<"ScanJob">["state"],
) {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, "scans", libraryId, state],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      read(
        api.GET("/scans", {
          params: { query: { libraryId, state, cursor: pageParam, limit: 30 } },
          signal,
        }),
      ),
    getNextPageParam: (page) => page.nextCursor || undefined,
    refetchInterval: (query) =>
      query.state.data?.pages.some((page) =>
        page.items.some((item) => ["queued", "running"].includes(item.state)),
      )
        ? 5000
        : false,
  });
}
export function useAuthorTasks(state?: Schema<"AuthorTask">["state"]) {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, "author-tasks", state],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      read(
        api.GET("/author-tasks", {
          params: { query: { state, cursor: pageParam, limit: 30 } },
          signal,
        }),
      ),
    getNextPageParam: (page) => page.nextCursor || undefined,
  });
}
export function useAuthorTask(id: string) {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, "author-task", id],
    queryFn: ({ signal }) =>
      read(api.GET("/author-tasks/{id}", { params: { path: { id } }, signal })),
    refetchInterval: (state) =>
      state.state.data?.task.state === "authorized" ? 5000 : false,
  });
}
export function useCancelAuthorTask(id: string) {
  return useMutation({
    mutationFn: () =>
      read(api.DELETE("/author-tasks/{id}", { params: { path: { id } } })),
    onSuccess: invalidateLibrary,
  });
}
