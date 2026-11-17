import { useMutation, useQueries } from "@tanstack/react-query";
import { useRef } from "react";
import { api, operationHeaders, read } from "./client";
import type { Schema } from "./client";
import { useRemoteKey } from "./session";
import { invalidateLibrary } from "./query";
import { newId } from "../lib/id";

export function useAssetAuthorRecords(ids: string[]) {
  const scope = useRemoteKey();
  return useQueries({ queries: ids.map(id => ({
    queryKey: [...scope, "author", id],
    queryFn: ({ signal }: { signal: AbortSignal }) => read(api.GET("/authors/{id}", { params: { path: { id } }, signal })),
  })) });
}

export type AuthorSourceChange = { author: Schema<"Author">; sources: string[] };
export function useSaveAssetAuthors() {
  const attempts = useRef(new Map<string, string>());
  return useMutation({
    mutationFn: async ({ asset, authorIds, sourceChanges }: {
      asset: Schema<"Asset">;
      authorIds?: string[];
      sourceChanges: AuthorSourceChange[];
    }) => {
      let savedAsset = asset;
      let linkError: unknown;
      const savedSources: Schema<"Author">[] = [];
      const sourceErrors: { id: string; name: string; error: unknown }[] = [];
      if (authorIds) {
        try {
          savedAsset = await read(api.PATCH("/assets/{id}", {
            params: { path: { id: asset.id } },
            body: { expectedRevision: asset.revision, expectedAuthorsRevision: asset.authorsRevision, authorIds },
          }));
        } catch (error) { linkError = error; }
      }
      if (!linkError) {
        for (const { author, sources } of sourceChanges) {
          const body = { expectedRevision: author.revision, acquisitionSources: sources };
          const signature = JSON.stringify([author.id, body]);
          let key = attempts.current.get(signature);
          if (!key) { key = newId(); attempts.current.set(signature, key); }
          try {
            savedSources.push(await read(api.PATCH("/authors/{id}", { params: { path: { id: author.id }, header: operationHeaders(key) }, body })));
            attempts.current.delete(signature);
          } catch (error) { sourceErrors.push({ id: author.id, name: author.name, error }); }
        }
      }
      await invalidateLibrary();
      return { savedAsset, linkError, savedSources, sourceErrors };
    },
  });
}
