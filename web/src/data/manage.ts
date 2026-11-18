import { useInfiniteQuery, useMutation, useQuery } from "@tanstack/react-query";
import { useRef } from "react";
import { newId } from "../lib/id";
import { api, operationHeaders, read } from "./client";
import type { Schema } from "./client";
import type { paths } from '../api/generated/schema';
import { invalidateLibrary } from "./query";
import { useRemoteKey } from "./session";

export function useLibraries(observeScans = false) {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, "libraries"],
    queryFn: ({ signal }) => read(api.GET("/libraries", { signal })),
    refetchInterval: query => observeScans && query.state.data?.items.some(library => library.scanState === "scanning") ? 5000 : false,
  });
}
export function useStorages() {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, "storages"],
    queryFn: ({ signal }) => read(api.GET("/storages", { signal })),
  });
}
export function useCreateLibrary() {
  return useMutation({
    mutationFn: ({
      body,
      key,
    }: {
      body: Schema<"LibraryCreate">;
      key: string;
    }) =>
      read(
        api.POST("/libraries", {
          body,
          params: { header: operationHeaders(key) },
        }),
      ),
    onSuccess: invalidateLibrary,
  });
}
type AuthorQuery = NonNullable<paths['/authors']['get']['parameters']['query']>;
export function useAuthors(q = "", following?: boolean, enabled = true, filters: Pick<AuthorQuery, 'kind' | 'sort'> = {}) {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, "authors", q, following, filters],
    enabled,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      read(
        api.GET("/authors", {
          params: { query: { ...filters, q, following, cursor: pageParam, limit: 30 } },
          signal,
        }),
      ),
    getNextPageParam: (last) => last.nextCursor || undefined,
  });
}
export function useAuthor(id: string) {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, "author", id],
    queryFn: ({ signal }) =>
      read(api.GET("/authors/{id}", { params: { path: { id } }, signal })),
    enabled: !!id,
  });
}
export function useUpdateAuthor(id: string) {
  const attempt = useRef({ signature: "", key: newId() });
  return useMutation({
    mutationFn: (body: Schema<"AuthorUpdate">) => {
      const signature = JSON.stringify([id, body]);
      if (attempt.current.signature !== signature)
        attempt.current = { signature, key: newId() };
      return read(
        api.PATCH("/authors/{id}", {
          params: {
            path: { id },
            header: operationHeaders(attempt.current.key),
          },
          body,
        }),
      );
    },
    onSuccess: invalidateLibrary,
  });
}
export function useCreateAuthor() {
  return useMutation({
    mutationFn: ({
      body,
      key,
    }: {
      body: Schema<"AuthorCreate">;
      key: string;
    }) =>
      read(
        api.POST("/authors", {
          params: { header: operationHeaders(key) },
          body,
        }),
      ),
    onSuccess: invalidateLibrary,
  });
}
export function useDeleteAuthor() {
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
        api.DELETE("/authors/{id}", {
          params: {
            path: { id },
            query: { expectedRevision: revision },
            header: operationHeaders(key),
          },
        }),
      ),
    onSuccess: invalidateLibrary,
  });
}
export function useMergeAuthors() {
  return useMutation({
    mutationFn: ({
      body,
      key,
    }: {
      body: Schema<"AuthorMergeInput">;
      key: string;
    }) =>
      read(
        api.POST("/authors/merge", {
          params: { header: operationHeaders(key) },
          body,
        }),
      ),
    onSuccess: invalidateLibrary,
  });
}
export function useCollections(libraryId?: string, enabled = true) {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, "collections", libraryId],
    enabled,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      read(
        api.GET("/collections", {
          params: { query: { libraryId, cursor: pageParam, limit: 50 } },
          signal,
        }),
      ),
    getNextPageParam: (last) => last.nextCursor || undefined,
  });
}
export function useCreateCollection() {
  return useMutation({
    mutationFn: ({
      body,
      key,
    }: {
      body: Schema<"CollectionCreate">;
      key: string;
    }) =>
      read(
        api.POST("/collections", {
          params: { header: operationHeaders(key) },
          body,
        }),
      ),
    onSuccess: invalidateLibrary,
  });
}
export function useStats() {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, "stats"],
    queryFn: ({ signal }) => read(api.GET("/stats", { signal })),
  });
}
export function useSettings() {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, "settings"],
    queryFn: ({ signal }) => read(api.GET("/settings", { signal })),
  });
}
export function useBatches() {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, "batches"],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      read(
        api.GET("/batches", {
          params: { query: { cursor: pageParam, limit: 30 } },
          signal,
        }),
      ),
    getNextPageParam: (last) => last.nextCursor || undefined,
    refetchInterval: 15_000,
  });
}
export function useBatch(id: string) {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, "batch", id],
    queryFn: ({ signal }) =>
      read(api.GET("/batches/{id}", { params: { path: { id } }, signal })),
    enabled: !!id,
    refetchInterval: (query) =>
      ["completed", "cancelled"].includes(query.state.data?.batch.state ?? "")
        ? false
        : 5000,
  });
}
export function useCancelBatch(id: string) {
  return useMutation({
    mutationFn: (revision: number) =>
      read(
        api.POST("/batches/{id}/cancel", {
          params: { path: { id } },
          body: { expectedRevision: revision },
        }),
      ),
    onSuccess: invalidateLibrary,
  });
}
export function useCancelBatchInput(id: string) {
  const attempts = useRef(new Map<string, string>());
  return useMutation({
    mutationFn: ({ inputId, fingerprint }: { inputId: string; fingerprint: string }) => {
      const signature = `${id}:${inputId}:${fingerprint}`;
      const key = attempts.current.get(signature) ?? newId();
      attempts.current.set(signature, key);
      return read(api.POST('/batches/{id}/inputs/{inputId}/cancel', {
        params: { path: { id, inputId }, header: operationHeaders(key) },
        body: { manifestFingerprint: fingerprint },
      }));
    },
    onSuccess: invalidateLibrary,
  });
}
export function useTrash() {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, "trash"],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      read(
        api.GET("/trash", {
          params: { query: { cursor: pageParam, limit: 50 } },
          signal,
        }),
      ),
    getNextPageParam: (last) => last.nextCursor || undefined,
  });
}
export function useTrashAction() {
  return useMutation({
    mutationFn: async ({
      id,
      revision,
      key,
      permanent,
    }: {
      id: string;
      revision: number;
      key: string;
      permanent: boolean;
    }) =>
      permanent
        ? read(
            api.DELETE("/trash/{id}", {
              params: {
                path: { id },
                query: { expectedRevision: revision },
                header: operationHeaders(key),
              },
            }),
          )
        : read(
            api.POST("/trash/{id}/restore", {
              params: { path: { id }, header: operationHeaders(key) },
              body: { expectedRevision: revision },
            }),
          ),
    onSuccess: invalidateLibrary,
  });
}
