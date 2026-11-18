import { useMutation, useQuery } from "@tanstack/react-query";
import { api, operationHeaders, read } from "./client";
import type { Schema } from "./client";
import { useRemoteKey } from "./session";
import { queryClient } from "./query";

export function useAuthorUpdates(id: string, enabled = true) {
  const scope = useRemoteKey();
  return useQuery({ queryKey: [...scope, "author-updates", id], enabled: !!id && enabled,
    queryFn: ({ signal }) => read(api.GET("/authors/{id}/updates", { params: { path: { id } }, signal })) });
}
export function useFeedActions(id: string) {
  const scope = useRemoteKey();
  const saved = (value: Schema<"AuthorUpdates">) => queryClient.setQueryData([...scope, "author-updates", id], value);
  const save = useMutation({ gcTime: 0, mutationFn: ({ body, key }: { body: Schema<"AuthorUpdatesInput">; key: string }) =>
    read(api.PUT("/authors/{id}/updates", { params: { path: { id }, header: operationHeaders(key) }, body })), onSuccess: saved });
  const check = useMutation({ gcTime: 0, mutationFn: ({ revision, key }: { revision: number; key: string }) =>
    read(api.POST("/authors/{id}/updates/check", { params: { path: { id }, header: operationHeaders(key) }, body: { expectedRevision: revision } })), onSuccess: saved });
  return { save, check };
}
