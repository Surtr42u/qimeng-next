import { useInfiniteQuery, useMutation, useQuery } from "@tanstack/react-query";
import type { paths } from "../api/generated/schema";
import { api, operationHeaders, read } from "./client";
import type { Schema } from "./client";
import { invalidateLibrary } from "./query";
import { useRemoteKey } from "./session";
import type { MediaCardModel } from "../components/MediaGrid";
import { duration } from '../lib/format';
import { useThumbnailPreference } from '../lib/thumbnail-preference';

export type Asset = Schema<"Asset">;
export type AssetFilters = NonNullable<
  paths["/assets"]["get"]["parameters"]["query"]
>;
export function deduplicateAssetPages<T extends { items: Asset[] }>(
  pages: T[],
): T[] {
  const seen = new Set<string>();
  return pages.map((page) => ({
    ...page,
    items: page.items.filter((asset) => {
      if (seen.has(asset.id)) return false;
      seen.add(asset.id);
      return true;
    }),
  }));
}
export function useAssets(filters: AssetFilters, enabled = true) {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, "assets", filters],
    placeholderData: (previous, query) => (query?.queryKey.at(-1) as AssetFilters | undefined)?.q === filters.q ? previous : undefined,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      read(
        api.GET("/assets", {
          params: { query: { ...filters, cursor: pageParam, limit: 48 } },
          signal,
        }),
      ),
    getNextPageParam: (last) => last.nextCursor || undefined,
    enabled,
    select: (data) => ({ ...data, pages: deduplicateAssetPages(data.pages) }),
  });
}
export function useAsset(id: string, enabled = true) {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, "asset", id],
    queryFn: ({ signal }) =>
      read(api.GET("/assets/{id}", { params: { path: { id } }, signal })),
    enabled: !!id && enabled,
  });
}
export function useAssetActions(id: string) {
  const reaction = useMutation({
    mutationFn: (body: Schema<"ReactionUpdate">) =>
      read(
        api.PUT("/assets/{id}/reaction", {
          params: { path: { id }, header: operationHeaders() },
          body,
        }),
      ),
    onSuccess: invalidateLibrary,
  });
  const tags = useMutation({
    mutationFn: ({ body, key }: { body: Schema<"TagsUpdate">; key: string }) =>
      read(
        api.PUT("/assets/{id}/tags", {
          params: { path: { id }, header: operationHeaders(key) },
          body,
        }),
      ),
    onSuccess: invalidateLibrary,
  });
  const update = useMutation({
    mutationFn: (body: Schema<"AssetUpdate">) =>
      read(api.PATCH("/assets/{id}", { params: { path: { id } }, body })),
    onSuccess: invalidateLibrary,
  });
  const trash = useMutation({
    mutationFn: ({ revision, key }: { revision: number; key: string }) =>
      read(
        api.DELETE("/assets/{id}", {
          params: {
            path: { id },
            query: { expectedRevision: revision },
            header: operationHeaders(key),
          },
        }),
      ),
    onSuccess: invalidateLibrary,
  });
  const move = useMutation({
    mutationFn: ({
      relativePath,
      revision,
      key,
    }: {
      relativePath: string;
      revision: number;
      key: string;
    }) =>
      read(
        api.POST("/assets/{id}/move", {
          params: { path: { id }, header: operationHeaders(key) },
          body: { relativePath, expectedRevision: revision },
        }),
      ),
    onSuccess: invalidateLibrary,
  });
  return { reaction, tags, update, trash, move };
}
export function useTags() {
  const key = useRemoteKey();
  const query = useInfiniteQuery({
    queryKey: [...key, "tags"],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      read(
        api.GET("/tags", {
          params: { query: { cursor: pageParam, limit: 100 } },
          signal,
        }),
      ),
    getNextPageParam: (last) => last.nextCursor || undefined,
  });
  return {
    ...query,
    data: query.data
      ? { items: query.data.pages.flatMap((page) => page.items) }
      : undefined,
  };
}
export function useHistory() {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, "history"],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      read(
        api.GET("/history", {
          params: { query: { cursor: pageParam, limit: 50 } },
          signal,
        }),
      ),
    getNextPageParam: (last) => last.nextCursor || undefined,
  });
}
export function originalUrl(asset: Asset) {
  const url = new URL(asset.contentUrl, window.location.origin);
  if (
    url.origin !== window.location.origin ||
    !url.pathname.startsWith("/api/v1/assets/")
  )
    throw new Error("服务器返回了无效媒体地址");
  url.searchParams.set("version", String(asset.contentVersion));
  return `${url.pathname}${url.search}`;
}
export function useThumbnailURL(source?: string) {
  const [preference] = useThumbnailPreference();
  if (!source || preference === 'auto') return source;
  const url = new URL(source, window.location.origin);
  url.searchParams.set('size', preference);
  return `${url.pathname}${url.search}`;
}
export function mediaCards(assets: readonly Asset[]): MediaCardModel[] {
  return assets.map((asset) => ({
    id: asset.id,
    title: asset.fileName,
    caption:
      asset.authorName ? `${asset.authorName}${asset.authorIds.length > 1 ? ` 等${asset.authorIds.length}` : ''}` :
      (asset.authorStatus === "unknown" ? "其他搁置" : "作者资料"),
    date: mediaCardDate(asset.sourceMtime || asset.contentMtime),
    duration: asset.mediaType === 'video' && asset.durationMs ? duration(asset.durationMs) : undefined,
    type: asset.mediaType,
    src: asset.thumbnailUrl
      ? safeMediaUrl(asset.thumbnailUrl, asset.contentVersion)
      : undefined,
    previewHint: asset.probeState === "failed" ? "预览生成失败" : "暂无预览",
    favorite: asset.favorite,
    liked: asset.liked,
  }));
}
function mediaCardDate(value?: string) {
  if (!value) return undefined;
  const date = new Date(value);
  return `${date.getMonth() + 1}-${date.getDate()}`;
}
function safeMediaUrl(value: string, version: number) {
  const url = new URL(value, window.location.origin);
  if (
    url.origin !== window.location.origin ||
    !url.pathname.startsWith("/api/v1/assets/")
  )
    throw new Error("服务器返回了无效预览地址");
  url.searchParams.set("version", String(version));
  return `${url.pathname}${url.search}`;
}
