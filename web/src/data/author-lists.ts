import { useInfiniteQuery, useMutation, useQuery } from "@tanstack/react-query";
import { api, operationHeaders, read } from "./client";
import type { Schema } from "./client";
import { useRemoteKey } from "./session";
import { invalidateLibrary } from "./query";

export function useAuthorLists() {
  const scope = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...scope, "author-lists"], initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => read(api.GET("/author-lists", { params: { query: { cursor: pageParam, limit: 50 } }, signal })),
    getNextPageParam: page => page.nextCursor || undefined,
  });
}
export async function findAuthorList(name: string, signal: AbortSignal) {
  let cursor: string | undefined;
  do {
    const page: Schema<"AuthorListPage"> = await read(api.GET("/author-lists", { params: { query: { cursor, limit: 100 } }, signal }));
    const found = page.items.find(list => list.name === name);
    if (found) return found;
    cursor = page.nextCursor;
  } while (cursor);
}
export async function readListInput(file: File) {
  if (file.size > 1024 * 1024) throw new Error("作者清单最多 1 MiB，请分成较小的 TXT。");
  return new TextDecoder("utf-8", { fatal: true }).decode(await file.arrayBuffer());
}
export type ListInput = { name: string; content: string; expectedRevision: number; conflictResolution?: "keep" | "remove" };
export function useAuthorListActions() {
  const importList = useMutation({
    gcTime: 0, mutationFn: ({ body, key }: { body: ListInput; key: string }) => read(api.POST("/author-lists", { params: { header: operationHeaders(key) }, body })),
    onSuccess: invalidateLibrary,
  });
  const rebuild = useMutation({
    mutationFn: (key: string) => read(api.POST("/author-lists/rebuild", { params: { header: operationHeaders(key) } })), onSuccess: invalidateLibrary,
  });
  const remove = useMutation({
    mutationFn: ({ list, key }: { list: Schema<"AuthorList">; key: string }) => read(api.DELETE("/author-lists/{id}", { params: { path: { id: list.id }, query: { expectedRevision: list.revision }, header: operationHeaders(key) } })), onSuccess: invalidateLibrary,
  });
  return { importList, rebuild, remove };
}
export function exportAuthorList(list?: Schema<"AuthorList">, signal?: AbortSignal) {
  return list ? read(api.GET("/author-lists/{id}/content", { params: { path: { id: list.id } }, parseAs: "text", signal })) : read(api.GET("/author-lists/master", { parseAs: "text", signal }));
}
export function downloadText(content: string, name: string) {
  const url = URL.createObjectURL(new Blob([content], { type: "text/plain;charset=utf-8" }));
  const link = document.createElement("a"); link.href = url; link.download = name; link.click();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}
export function useAuthorMirror() {
  const scope = useRemoteKey();
  return useQuery({
    queryKey: [...scope, "author-list-mirror"], gcTime: 0,
    queryFn: ({ signal }) => read(api.GET("/author-lists/mirror", { signal })),
    refetchInterval: query => query.state.data?.state === "pending" ? 2000 : false,
  });
}
export function useUpdateAuthorMirror() {
  return useMutation({
    gcTime: 0, mutationFn: ({ body, key }: { body: Schema<"AuthorListMirrorUpdate">; key: string }) =>
      read(api.PUT("/author-lists/mirror", { params: { header: operationHeaders(key) }, body })), onSuccess: invalidateLibrary,
  });
}
