import { Link, useSearchParams } from "react-router-dom";
import { useRecommendations, useRankings } from "../data/analytics";
import type { RankingPeriod } from "../data/analytics";
import { mediaCards } from "../data/media";
import { useSession } from "../data/session";
import { ErrorNotice } from "../components/Feedback";
import { MediaGrid } from "../components/MediaGrid";
import { AutoPageSentinel } from "../components/AutoPageSentinel";
import { BrowseToolbar } from "../components/LegacyBrowseToolbar";
import { RefreshIcon } from "../components/LegacyShellIcons";
import { SkeletonGrid } from "../components/LegacyLoading";

export function HomePage() {
  const [params, setParams] = useSearchParams();
  const { instance } = useSession();
  const tab = params.get("tab") ?? "recommend";
  const ranked = tab === "hot" || tab === "rank";
  const period = (params.get("period") ?? "day") as RankingPeriod;
  const seed = Number(params.get("seed") ?? 0);
  const recommendations = useRecommendations(
    seed,
    tab === "cos" ? "cos" : "regular",
    undefined,
    !ranked && instance.capabilities.recommendations,
  );
  const rankings = useRankings(
    period,
    ranked && instance.capabilities.rankings,
  );
  const query = ranked ? rankings : recommendations;
  const items = ranked
    ? (rankings.data?.pages.flatMap((page) =>
        page.items.map((item) => item.asset),
      ) ?? [])
    : (recommendations.data?.pages.flatMap((page) => page.items) ?? []);
  const unavailable = ranked
    ? !instance.capabilities.rankings
    : !instance.capabilities.recommendations;
  const refresh = () =>
    ranked
      ? void rankings.refetch()
      : setParams(
          (current) => {
            const next = new URLSearchParams(current);
            next.set(
              "seed",
              String(crypto.getRandomValues(new Uint32Array(1))[0]),
            );
            return next;
          },
          { replace: true },
        );
  return (
    <div className="page" id="page-home">
      <BrowseToolbar
        title={ranked ? "内容热榜" : tab === "cos" ? "COS 推荐" : "为你推荐"}
        summary={
          query.isPending
            ? "正在读取…"
            : query.isPlaceholderData
              ? "正在更新这一批…"
              : query.isError
                ? `读取失败 · 已加载 ${items.length} 项`
                : `已加载 ${items.length} 项`
        }
        actions={
          <button
            className="browse-action"
            type="button"
            disabled={query.isFetching || unavailable}
            onClick={refresh}
          >
            <RefreshIcon />
            {ranked ? "刷新榜单" : "换一批"}
          </button>
        }
      />
      <ErrorNotice
        error={query.error}
        stale={items.length > 0}
        retry={() =>
          void (query.isFetchNextPageError
            ? query.fetchNextPage()
            : query.refetch())
        }
      />
      {unavailable ? (
        <p className="grid-empty">当前服务尚未开放此内容入口。</p>
      ) : query.isPending ? (
        <SkeletonGrid />
      ) : items.length ? (
        <MediaGrid items={mediaCards(items)} />
      ) : (
        !query.isError && (
          <p className="grid-empty">
            {ranked
              ? "暂无上榜内容。"
              : tab === "cos"
                ? "暂无 COS 内容。"
                : "还没有内容。"}{" "}
            {!ranked && <Link to="/manage/libraries">前往库管理</Link>}
          </p>
        )
      )}
      <AutoPageSentinel
        enabled={query.hasNextPage && !query.isPlaceholderData}
        loading={query.isFetchingNextPage}
        failed={query.isFetchNextPageError}
        load={() => query.fetchNextPage({ cancelRefetch: false })}
      />
      {query.hasNextPage ? (
        <div className="workspace-load-more">
          <button
            className="pill"
            type="button"
            disabled={query.isFetchingNextPage}
            onClick={() => void query.fetchNextPage({ cancelRefetch: false })}
          >
            {query.isFetchingNextPage ? "加载中…" : "加载更多"}
          </button>
        </div>
      ) : (
        !query.isPending &&
        items.length > 0 && (
          <p className="grid-empty">
            {ranked ? "到底了" : "这一批已浏览完，可以换一批继续发现。"}
          </p>
        )
      )}
    </div>
  );
}
