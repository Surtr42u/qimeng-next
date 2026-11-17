import { useState } from "react";
import { useLocation, useSearchParams } from "react-router-dom";
import { X } from "lucide-react";
import { useAssets, mediaCards, useTags } from "../data/media";
import type { AssetFilters } from "../data/media";
import { filterURLKeys, filtersFromURL, useAssetFacets } from "../data/browse";
import { ErrorNotice, Loading } from "../components/Feedback";
import { BrowseToolbar } from "../components/LegacyBrowseToolbar";
import { LegacyFacetFilter } from "../components/LegacyFacetFilter";
import {
  LegacyPanelFilters,
  panelActive,
} from "../components/LegacyPanelFilters";
import { ChevronDownIcon } from "../components/LegacyShellIcons";
import { MediaGrid } from "../components/MediaGrid";
import { AutoPageSentinel } from "../components/AutoPageSentinel";
import { SkeletonGrid } from "../components/LegacyLoading";
import { albumGroups } from "../lib/album-presentation";

export function useBrowseSelection(scope: Partial<AssetFilters> = {}) {
  const [params, setParams] = useSearchParams();
  const [tzOffsetMinutes] = useState(() => -new Date().getTimezoneOffset());
  const filters = { ...filtersFromURL(params), ...scope, tzOffsetMinutes };
  const change = (patch: Partial<AssetFilters>) =>
    setParams(
      (current) => {
        const next = new URLSearchParams(current);
        for (const [key, value] of Object.entries(patch)) {
          next.delete(key);
          if (value !== undefined) {
            if (Array.isArray(value))
              value.forEach((item) => next.append(key, item));
            else next.set(key, String(value));
          }
        }
        if ("tags" in patch) next.delete("tag");
        return next;
      },
      { replace: true },
    );
  const reset = () =>
    setParams(
      (current) => {
        const next = new URLSearchParams(current);
        filterURLKeys.forEach((key) => next.delete(key));
        return next;
      },
      { replace: true },
    );
  return { filters, change, reset };
}
export function BrowsePage() {
  return useLocation().pathname === "/search" ? <SearchView /> : <AlbumsView />;
}
function usePanelTags() {
  const tags = useTags();
  return {
    pool: tags.data?.items ?? [],
    actions: (
      <>
        <ErrorNotice
          error={tags.error}
          stale={!!tags.data}
          retry={() => void tags.refetch()}
        />
        {tags.hasNextPage && (
          <button
            type="button"
            className="pill"
            disabled={tags.isFetchingNextPage}
            onClick={() => void tags.fetchNextPage()}
          >
            更多标签
          </button>
        )}
      </>
    ),
  };
}
function AlbumsView() {
  const { filters, change, reset } = useBrowseSelection();
  const query = useAssets({ ...filters, dateCounts: true });
  const partition = useAssetFacets(filters, "partitions");
  const works = useAssetFacets(filters, "works");
  const roles = useAssetFacets(filters, "roles");
  const types = useAssetFacets(filters, "types");
  const tags = usePanelTags();
  const [panelOpen, setPanelOpen] = useState(false);
  const items = query.data?.pages.flatMap((page) => page.items) ?? [];
  const first = query.data?.pages[0];
  const groups = albumGroups(items, first?.dateCounts, filters.tzOffsetMinutes, Date.now(), first?.unknownDateCount);
  const active =
    !!filters.kind ||
    !!filters.mediaType ||
    !!filters.workKeys?.length ||
    !!filters.roleKeys?.length ||
    !!panelActive(filters);
  return (
    <div className="page" id="page-albums">
      <BrowseToolbar
        title="相册"
        summary={
          query.isPlaceholderData
            ? "正在切换筛选…"
            : query.isError
              ? `已加载 ${items.length} 项 · 读取失败`
              : first
                ? `匹配 ${first.totalMatched.toLocaleString("zh-CN")} 项 · 已加载 ${items.length} 项`
                : "按日期浏览媒体"
        }
        actions={
          active && (
            <button
              className="browse-action"
              type="button"
              onClick={() => {
                reset();
                setPanelOpen(false);
              }}
            >
              <X aria-hidden="true" />
              清空筛选
            </button>
          )
        }
      />
      {!query.isFetchNextPageError && (
        <ErrorNotice
          error={query.error}
          stale={items.length > 0}
          retry={() => void query.refetch()}
        />
      )}
      <LegacyFacetFilter
        filters={filters}
        dimensions={[
          { row: "partitions", label: "分区", query: partition },
          { row: "works", label: "作者", query: works },
          { row: "roles", label: "角色", query: roles },
          { row: "types", label: "类型", query: types },
        ]}
        onChange={change}
        panelOpen={panelOpen}
        setPanelOpen={setPanelOpen}
        active={active}
      >
        <LegacyPanelFilters
          hidden={!panelOpen}
          filters={filters}
          tagPool={tags.pool}
          onChange={change}
          tagActions={tags.actions}
        />
      </LegacyFacetFilter>
      {query.isPending ? (
        <SkeletonGrid className="media-grid" />
      ) : (
        groups.map((group) => (
          <section className="album-group" key={group.key || "__unknown__"}>
            {group.label && (
              <h3 className="album-group-title">
                {group.label}
                <span className="album-group-count">
                  {group.total === undefined
                    ? `已加载 ${group.items.length} 项`
                    : `${group.total} 项`}
                </span>
              </h3>
            )}
            <MediaGrid items={mediaCards(group.items)} navigationIds={items.map(item => item.id)} />
          </section>
        ))
      )}
      {!query.isFetching && !query.isError && !items.length && (
        <p className="grid-empty">
          {active
            ? "该筛选组合下暂无内容，可以清空筛选后再试。"
            : "相册里还没有内容。"}
        </p>
      )}
      <BrowseTail query={query} count={items.length} />
    </div>
  );
}
function SearchView() {
  const { filters, change } = useBrowseSelection();
  const [panelOpen, setPanelOpen] = useState(false);
  const [lastQ, setLastQ] = useState(filters.q);
  if (filters.q !== lastQ) {
    setLastQ(filters.q);
    setPanelOpen(false);
  }
  const tags = usePanelTags();
  const types = useAssetFacets({ kind: filters.kind }, "types");
  const query = useAssets(filters, !!filters.q);
  const items = query.data?.pages.flatMap((page) => page.items) ?? [];
  const typeCounts = new Map(
    types.data?.pages
      .flatMap((page) => page.items)
      .map((item) => [item.id, item.count]),
  );
  const options = [
    { value: "", label: "综合" },
    { value: "video", label: "视频" },
    { value: "animated_image", label: "动图" },
    { value: "image", label: "图片" },
  ] as const;
  return (
    <div className="page" id="page-search">
      <h2 className="sr-only" data-page-heading tabIndex={-1}>
        搜索结果
      </h2>
      <div className="stype-row">
        {options.map((option) => (
          <button
            type="button"
            key={option.value}
            className={`stype${(filters.mediaType ?? "") === option.value ? " active" : ""}`}
            data-stype={option.label}
            aria-pressed={(filters.mediaType ?? "") === option.value}
            onClick={() => change({ mediaType: option.value || undefined })}
          >
            {option.label}
            {option.value && typeCounts.has(option.value) && (
              <span className="stype-count">
                {typeCounts.get(option.value)}
              </span>
            )}
          </button>
        ))}
      </div>
      <div
        className="pill-row"
        role="group"
        aria-label="内容分区"
        style={{ padding: "0 24px" }}
      >
        {[
          { value: "", label: "全部" },
          { value: "regular", label: "常规" },
          { value: "cos", label: "COS" },
        ].map((option) => (
          <button
            type="button"
            key={option.value}
            className={`pill${(filters.kind ?? "") === option.value ? " active" : ""}`}
            aria-pressed={(filters.kind ?? "") === option.value}
            onClick={() =>
              change({
                kind: (option.value as AssetFilters["kind"]) || undefined,
              })
            }
          >
            {option.label}
          </button>
        ))}
        <button
          type="button"
          className={`more-filter${panelOpen ? " open" : ""}${panelActive(filters) ? " active" : ""}`}
          aria-expanded={panelOpen}
          onClick={() => setPanelOpen(!panelOpen)}
        >
          更多筛选
          <ChevronDownIcon />
        </button>
      </div>
      <LegacyPanelFilters
        hidden={!panelOpen}
        filters={filters}
        tagPool={tags.pool}
        onChange={change}
        tagActions={tags.actions}
      />
      <ErrorNotice
        error={types.error}
        stale={!!types.data}
        retry={() => void types.refetch()}
      />
      {!query.isFetchNextPageError && (
        <ErrorNotice
          error={query.error}
          stale={items.length > 0}
          retry={() => void query.refetch()}
        />
      )}
      {!filters.q ? (
        <p className="grid-empty">在顶部搜索框输入关键词开始搜索</p>
      ) : query.isPending ? (
        <SkeletonGrid />
      ) : items.length ? (
        <MediaGrid items={mediaCards(items)} search />
      ) : (
        !query.isError && (
          <p className="grid-empty">没有匹配的内容，放宽一点筛选条件试试。</p>
        )
      )}
      {query.isPlaceholderData && (
        <p className="sr-only" role="status">
          正在切换筛选…
        </p>
      )}
      {filters.q && <BrowseTail query={query} count={items.length} />}
    </div>
  );
}
export function BrowseTail({
  query,
  count,
}: {
  query: ReturnType<typeof useAssets>;
  count: number;
}) {
  return (
    <>
      {query.isFetchNextPageError && (
        <ErrorNotice
          error={query.error}
          stale
          retry={() => void query.fetchNextPage({ cancelRefetch: false })}
        />
      )}
      {query.isFetchingNextPage && <Loading />}
      {!query.hasNextPage && count > 0 && !query.isPlaceholderData && (
        <p className="grid-empty">共 {count} 项 · 到底了</p>
      )}
      <AutoPageSentinel
        enabled={query.hasNextPage && !query.isPlaceholderData}
        loading={query.isFetchingNextPage}
        failed={query.isFetchNextPageError}
        load={() => query.fetchNextPage({ cancelRefetch: false })}
      />
      {query.hasNextPage && (
        <button
          className="pill sr-only-focusable"
          disabled={query.isFetchingNextPage || query.isPlaceholderData}
          onClick={() => void query.fetchNextPage({ cancelRefetch: false })}
        >
          加载更多
        </button>
      )}
    </>
  );
}
