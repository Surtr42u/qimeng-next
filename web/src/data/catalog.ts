import { useInfiniteQuery, useMutation } from "@tanstack/react-query";
import { api, operationHeaders, read } from "./client";
import type { Schema } from "./client";
import { useRemoteKey } from "./session";
import { invalidateLibrary } from "./query";

export function useUpdateLibrary() {
  return useMutation({
    mutationFn: ({
      id,
      body,
      key,
    }: {
      id: string;
      body: Schema<"LibraryUpdate">;
      key: string;
    }) =>
      read(
        api.PATCH("/libraries/{id}", {
          params: { path: { id }, header: operationHeaders(key) },
          body,
        }),
      ),
    onSuccess: invalidateLibrary,
  });
}
export function useDirectories(id: string, parent: string, enabled: boolean) {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, "directories", id, parent],
    initialPageParam: undefined as string | undefined,
    enabled: enabled && !!id,
    queryFn: ({ pageParam, signal }) =>
      read(
        api.GET("/libraries/{id}/directories", {
          params: {
            path: { id },
            query: { parent, cursor: pageParam, limit: 50 },
          },
          signal,
        }),
      ),
    getNextPageParam: (last) => last.nextCursor || undefined,
  });
}
export function useCreateDirectory(id: string) {
  return useMutation({
    mutationFn: ({
      relativePath,
      key,
    }: {
      relativePath: string;
      key: string;
    }) =>
      read(
        api.POST("/libraries/{id}/directories", {
          params: { path: { id }, header: operationHeaders(key) },
          body: { relativePath },
        }),
      ),
    onSuccess: invalidateLibrary,
  });
}
export function useTagVocabulary() {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, "tag-vocabulary"],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      read(
        api.GET("/tag-vocabulary", {
          params: { query: { cursor: pageParam, limit: 100 } },
          signal,
        }),
      ),
    getNextPageParam: (last) => last.nextCursor || undefined,
  });
}
export function useEditTag() {
  return useMutation({
    mutationFn: async ({
      previous,
      name,
      key,
      remove,
    }: {
      previous?: Schema<"Tag">;
      name: string;
      key: string;
      remove?: boolean;
    }) => {
      if (remove && previous)
        return read(
          api.DELETE("/tags/{tag}", {
            params: {
              path: { tag: previous.name },
              header: operationHeaders(key),
              query: { expectedRevision: previous.revision },
            },
          }),
        );
      if (previous)
        return read(
          api.PATCH("/tags/{tag}", {
            params: {
              path: { tag: previous.name },
              header: operationHeaders(key),
            },
            body: { name, expectedRevision: previous.revision },
          }),
        );
      return read(
        api.POST("/tags", {
          params: { header: operationHeaders(key) },
          body: { name },
        }),
      );
    },
    onSuccess: invalidateLibrary,
  });
}
export function useCreateTag() {
  return useMutation({
    mutationFn: ({ name, key }: { name: string; key: string }) =>
      read(
        api.POST("/tags", {
          params: { header: operationHeaders(key) },
          body: { name },
        }),
      ),
    onSuccess: invalidateLibrary,
  });
}
export function useTerms(kind: "source" | "character", q = "") {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, kind, q],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({
      pageParam,
      signal,
    }): Promise<{
      items: (Schema<"Source"> | Schema<"Character">)[];
      nextCursor?: string;
    }> =>
      kind === "source"
        ? read(
            api.GET("/sources", {
              params: { query: { q, cursor: pageParam, limit: 50 } },
              signal,
            }),
          )
        : read(
            api.GET("/characters", {
              params: { query: { q, cursor: pageParam, limit: 50 } },
              signal,
            }),
          ),
    getNextPageParam: (last) => last.nextCursor || undefined,
  });
}
export function useEditTerm(kind: "source" | "character") {
  return useMutation({
    mutationFn: async ({
      id,
      body,
      key,
    }: {
      id?: string;
      body: Schema<"TermInput">;
      key: string;
    }) => {
      if (kind === "source")
        return id
          ? read(
              api.PUT("/sources/{id}", {
                params: { path: { id }, header: operationHeaders(key) },
                body,
              }),
            )
          : read(
              api.POST("/sources", {
                params: { header: operationHeaders(key) },
                body,
              }),
            );
      return id
        ? read(
            api.PUT("/characters/{id}", {
              params: { path: { id }, header: operationHeaders(key) },
              body,
            }),
          )
        : read(
            api.POST("/characters", {
              params: { header: operationHeaders(key) },
              body,
            }),
          );
    },
    onSuccess: invalidateLibrary,
  });
}
export function useTimeline(id: string) {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, "timeline", id],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      read(
        api.GET("/assets/{id}/timeline", {
          params: { path: { id }, query: { cursor: pageParam, limit: 100 } },
          signal,
        }),
      ),
    getNextPageParam: (last) => last.nextCursor || undefined,
  });
}
export function useEditTimeline(id: string) {
  return useMutation({
    mutationFn: async ({
      mark,
      body,
      key,
      remove,
    }: {
      mark?: Schema<"TimelineMark">;
      body: Schema<"TimelineInput">;
      key: string;
      remove?: boolean;
    }) => {
      if (remove && mark)
        return read(
          api.DELETE("/assets/{id}/timeline/{markId}", {
            params: {
              path: { id, markId: mark.id },
              header: operationHeaders(key),
              query: { expectedRevision: mark.revision },
            },
          }),
        );
      if (mark)
        return read(
          api.PUT("/assets/{id}/timeline/{markId}", {
            params: {
              path: { id, markId: mark.id },
              header: operationHeaders(key),
            },
            body: { ...body, expectedRevision: mark.revision },
          }),
        );
      return read(
        api.POST("/assets/{id}/timeline", {
          params: { path: { id }, header: operationHeaders(key) },
          body,
        }),
      );
    },
    onSuccess: invalidateLibrary,
  });
}
