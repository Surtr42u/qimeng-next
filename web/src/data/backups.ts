import { useInfiniteQuery, useMutation } from "@tanstack/react-query";
import { useRef } from "react";
import { newId } from "../lib/id";
import { api, operationHeaders, read } from "./client";
import { useRemoteKey } from "./session";
import { invalidateLibrary } from "./query";

export function useBackups() {
  const scope = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...scope, "backups"], initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => read(api.GET("/backups", { params: { query: { cursor: pageParam, limit: 25 } }, signal })),
    getNextPageParam: page => page.nextCursor || undefined,
    refetchInterval: query => query.state.data?.pages.some(page => page.items.some(item => item.exportState === "queued" || item.exportState === "running")) ? 2000 : false,
  });
}
export const createCheckpoint = (key: string, signal?: AbortSignal) => read(api.POST("/backups", { params: { header: operationHeaders(key) }, signal }));
export const requestExport = (id: string, key: string, signal?: AbortSignal) => read(api.POST("/backups/{id}/export", { params: { path: { id }, header: operationHeaders(key) }, signal }));
export const getBackup = (id: string, signal?: AbortSignal) => read(api.GET("/backups/{id}", { params: { path: { id } }, signal }));
export const downloadArchiveStream = (id: string, signal: AbortSignal) => read(api.GET("/backups/{id}/archive", { params: { path: { id } }, parseAs: "stream", signal }));
export const backupDownloadURL = (id: string) => "/api/v1/backups/" + encodeURIComponent(id) + "/archive";
export function usePortableExport() {
  const attempt = useRef<{ key: string; id?: string; exportKey: string }>({ key: newId(), exportKey: newId() });
  return useMutation({
    mutationFn: async (retry: { id: string; key: string } | undefined) => {
      if (retry) return requestExport(retry.id, retry.key);
      const current = attempt.current;
      if (!current.id) current.id = (await createCheckpoint(current.key)).id;
      return requestExport(current.id, current.exportKey);
    },
    onSuccess: async () => { attempt.current = { key: newId(), exportKey: newId() }; await invalidateLibrary(); },
  });
}
