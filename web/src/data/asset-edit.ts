import { useMutation, useQueries } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { api, operationHeaders, read } from "./client";
import type { Schema } from "./client";
import { useRemoteKey } from "./session";
import { invalidateLibrary, queryClient } from "./query";
import { newId } from "../lib/id";

export function useAssetAuthorRecords(ids: string[]) {
  const scope = useRemoteKey();
  return useQueries({ queries: ids.map(id => ({
    queryKey: [...scope, "author", id],
    queryFn: ({ signal }: { signal: AbortSignal }) => read(api.GET("/authors/{id}", { params: { path: { id } }, signal })),
  })) });
}

export type AuthorSourceChange = { author: Schema<"Author">; sources: string[] };
function useDraftRequest() {
  const active = useRef<AbortController | undefined>(undefined);
  useEffect(() => () => active.current?.abort(), []);
  return () => { active.current?.abort(); active.current = new AbortController(); return active.current.signal; };
}
function replaySelection(original: string[], draft: string[], latest: string[]) {
  const removed = new Set(original.filter(id => !draft.includes(id)));
  return [...new Set([...latest.filter(id => !removed.has(id)), ...draft.filter(id => !original.includes(id))])];
}
export function useRebaseAssetAuthors() {
  const scope = useRemoteKey();
  const request = useDraftRequest();
  return useMutation({ gcTime: 0, mutationFn: async ({ id, baseline, selected, sources }: { id: string; baseline: string[]; selected: string[]; sources: Record<string, AuthorSourceChange> }) => {
    const signal = request();
    const asset = await read(api.GET("/assets/{id}", { params: { path: { id } }, signal }));
    const latest = asset.authorStatus === "unknown" ? [] : asset.authorIds;
    const ids = [...new Set([...latest, ...Object.keys(sources)])];
    const authors = await Promise.all(ids.map(id => read(api.GET("/authors/{id}", { params: { path: { id } }, signal }))));
    signal.throwIfAborted();
    const regular = authors.filter(author => latest.includes(author.id) && author.kind !== "cos" && !author.placeholder).map(author => author.id);
    const merged = replaySelection(baseline, selected, regular);
    const changes: Record<string, AuthorSourceChange> = {};
    for (const author of authors) {
      queryClient.setQueryData([...scope, "author", author.id], author);
      const change = sources[author.id];
      if (!change) continue;
      const current = author.acquisitionSources ?? [];
      const values = replaySelection(change.author.acquisitionSources ?? [], change.sources, current);
      if (values.length !== current.length || values.some(value => !current.includes(value))) changes[author.id] = { author, sources: values };
    }
    queryClient.setQueryData([...scope, "asset", id], asset);
    return { asset, regular, merged, changes };
  }});
}
export function useSaveAssetAuthors() {
  const attempts = useRef(new Map<string, string>());
  const request = useDraftRequest();
  return useMutation({
    mutationFn: async ({ asset, authorIds, newAuthorNames, sourceChanges }: {
      asset: Schema<"Asset">;
      authorIds?: string[];
      newAuthorNames?: string[];
      sourceChanges: AuthorSourceChange[];
    }) => {
      const signal = request();
      let savedAsset = asset;
      let linkError: unknown;
      const savedSources: Schema<"Author">[] = [];
      const sourceErrors: { id: string; name: string; error: unknown }[] = [];
      if (authorIds) {
        const body = { expectedRevision: asset.revision, expectedAuthorsRevision: asset.authorsRevision, authorIds, newAuthorNames };
        const signature = JSON.stringify(["asset-authors", asset.id, body]);
        let key = attempts.current.get(signature);
        if (!key) { key = newId(); attempts.current.set(signature, key); }
        try {
          savedAsset = await read(api.PATCH("/assets/{id}", {
            params: { path: { id: asset.id }, header: operationHeaders(key) }, body, signal,
          }));
          attempts.current.delete(signature);
        } catch (error) { linkError = error; }
      }
      if (!linkError) {
        for (const { author, sources } of sourceChanges) {
          signal.throwIfAborted();
          const body = { expectedRevision: author.revision, acquisitionSources: sources };
          const signature = JSON.stringify([author.id, body]);
          let key = attempts.current.get(signature);
          if (!key) { key = newId(); attempts.current.set(signature, key); }
          try {
            savedSources.push(await read(api.PATCH("/authors/{id}", { params: { path: { id: author.id }, header: operationHeaders(key) }, body, signal })));
            attempts.current.delete(signature);
          } catch (error) { sourceErrors.push({ id: author.id, name: author.name, error }); }
        }
      }
      signal.throwIfAborted();
      await invalidateLibrary();
      return { savedAsset, linkError, savedSources, sourceErrors };
    },
  });
}
