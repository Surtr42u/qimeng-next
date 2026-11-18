import { useInfiniteQuery, useMutation } from "@tanstack/react-query";
import { useRef } from "react";
import { newId } from "../lib/id";
import { api, operationHeaders, read } from "./client";
import type { Schema } from "./client";
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

export function useV2Inputs() {
  const scope = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...scope, "v2-inputs"], initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => read(api.GET("/migration/v2/imports", { params: { query: { cursor: pageParam, limit: 25 } }, signal })),
    getNextPageParam: page => page.nextCursor || undefined,
  });
}
export function stageV2(file: File, sourceId: string, key: string, signal: AbortSignal) {
  if (file.size > 64 * 1024 * 1024) throw new Error("旧 v2 JSON 超过 64 MiB，未发送。");
  return read(api.POST("/migration/v2/imports", {
    params: { query: { sourceId }, header: operationHeaders(key) },
    body: "", bodySerializer: () => file, headers: { "Content-Type": "application/octet-stream" }, signal,
  }));
}
export const planV2 = (id: string, key: string, body: Schema<"V2PlanCreate">, signal: AbortSignal) =>
  read(api.POST("/migration/v2/imports/{id}/plans", { params: { path: { id }, header: operationHeaders(key) }, body, signal }));
export function useV2Review(id: string) {
  const scope = useRemoteKey();
  const media = useInfiniteQuery({
    queryKey: [...scope, "v2-plan-items", id], enabled: !!id, initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => read(api.GET("/migration/v2/plans/{id}/items", { params: { path: { id }, query: { cursor: pageParam, limit: 30 } }, signal })),
    getNextPageParam: page => page.nextCursor || undefined,
  });
  const authors = useInfiniteQuery({
    queryKey: [...scope, "v2-plan-authors", id], enabled: !!id, initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => read(api.GET("/migration/v2/plans/{id}/authors", { params: { path: { id }, query: { cursor: pageParam, limit: 30 } }, signal })),
    getNextPageParam: page => page.nextCursor || undefined,
  });
  return { media, authors };
}
export function useV2Commit() {
  return useMutation({
    mutationFn: ({ plan, acceptSkipped, key }: { plan: Schema<"V2MigrationPlan">; acceptSkipped: boolean; key: string }) =>
      read(api.POST("/migration/v2/plans/{id}/commit", { params: { path: { id: plan.id }, header: operationHeaders(key) }, body: { fingerprint: plan.fingerprint, acceptSkipped } })),
    onSuccess: invalidateLibrary,
  });
}
