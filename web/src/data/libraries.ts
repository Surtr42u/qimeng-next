import { useEffect, useRef } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { api, operationHeaders, read } from "./client";
import type { Schema } from "./client";
import { useRemoteKey } from "./session";
import { invalidateLibrary } from "./query";
import { newId } from "../lib/id";

export function useLibraryLocation(id: string) {
  const scope = useRemoteKey();
  return useQuery({
    queryKey: [...scope, "library-location", id],
    gcTime: 0,
    queryFn: ({ signal }) => read(api.GET("/libraries/{id}/location", { params: { path: { id } }, signal })),
  });
}

export function useRemoveLibrary(onRemoved: () => void) {
  return useMutation({
    mutationFn: ({ id, revision, key }: { id: string; revision: number; key: string }) =>
      read(api.DELETE("/libraries/{id}", { params: { path: { id }, query: { expectedRevision: revision }, header: operationHeaders(key) } })),
    onSuccess: () => { onRemoved(); return invalidateLibrary(); },
  });
}

export function useRegisterLibrary() {
  const scope = useRemoteKey();
  const attempt = useRef<{ signature: string; key: string; location?: Schema<"LibraryLocation">; library?: Schema<"Library"> }>({ signature: "", key: newId() });
  const controller = useRef<AbortController | undefined>(undefined);
  useEffect(() => () => controller.current?.abort(), []);
  return useMutation({
    gcTime: 0,
    mutationFn: async (input: { name: string; rootPath: string; kind: Schema<"LibraryCreate">["kind"] }) => {
      if (controller.current) throw new Error("库登记请求仍在处理中。");
      const abort = new AbortController();
      controller.current = abort;
      const signature = JSON.stringify([scope, input]);
      if (attempt.current.signature !== signature) attempt.current = { signature, key: newId() };
      const current = attempt.current;
      try {
        if (!current.location) current.location = await read(api.POST("/storages/resolve-library-root", { body: { rootPath: input.rootPath }, signal: abort.signal }));
        abort.signal.throwIfAborted();
        if (!current.library) current.library = await read(api.POST("/libraries", {
          params: { header: operationHeaders(current.key) },
          body: { name: input.name, kind: input.kind, storageId: current.location.storageId, rootRelativePath: current.location.rootRelativePath },
          signal: abort.signal,
        }));
        const library = current.library;
        let scan: Schema<"ScanJob"> | undefined;
        let scanError: unknown;
        try {
          abort.signal.throwIfAborted();
          scan = await read(api.POST("/libraries/{id}/scans", { params: { path: { id: library.id }, header: operationHeaders(`${current.key}-scan`) }, body: { expectedRevision: library.revision }, signal: abort.signal }));
        } catch (error) { scanError = error; }
        if (!abort.signal.aborted) await invalidateLibrary();
        return { library, scan, scanError, scanKey: `${current.key}-scan` };
      } finally { controller.current = undefined; }
    },
  });
}
