import { Link, useSearchParams } from "react-router-dom";
import { SegmentedControl } from "@ui/GlassNavigation";
import { useAssets, useHistory, mediaCards } from "../data/media";
import { AuthorsList } from "./AuthorsPage";
import {
  EmptyState,
  ErrorNotice,
  Loading,
  PageHeading,
} from "../components/Feedback";
import { MediaGrid } from "../components/MediaGrid";
import { dateTime, duration } from "../lib/format";
import { Icon } from "../components/Icon";

export function MinePage() {
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") ?? "favorites";
  return (
    <>
      <PageHeading
        title="我的内容"
        subtitle="关注、收藏与那些值得再次打开的瞬间。"
      />
      <SegmentedControl
        label="我的内容分区"
        value={tab}
        options={[
          { value: "following", label: "关注" },
          { value: "favorites", label: "收藏" },
          { value: "history", label: "历史" },
        ]}
        onChange={(value) => setParams({ tab: value }, { replace: true })}
      />
      <div className="section-space">
        {tab === "following" ? (
          <AuthorsList following />
        ) : tab === "history" ? (
          <HistoryList />
        ) : (
          <FavoriteList />
        )}
      </div>
    </>
  );
}
function FavoriteList() {
  const query = useAssets({ favorite: true });
  const items = query.data?.pages.flatMap((page) => page.items) ?? [];
  return (
    <>
      <ErrorNotice
        error={query.error}
        stale={items.length > 0}
        retry={() => void query.refetch()}
      />
      {query.isPending ? (
        <Loading />
      ) : items.length ? (
        <MediaGrid items={mediaCards(items)} />
      ) : (
        !query.isError && (
          <EmptyState title="还没有收藏">
            在详情里点亮收藏，喜欢的内容就会留在这里。
          </EmptyState>
        )
      )}
      {query.hasNextPage && (
        <button
          className="button"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
        >
          加载更多收藏
        </button>
      )}
    </>
  );
}
function HistoryList() {
  const query = useHistory();
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const events = query.data?.pages.flatMap((page) => page.items) ?? [];
  const visible = events.filter(
    (event) =>
      !q || event.fileName?.toLocaleLowerCase().includes(q.toLocaleLowerCase()),
  );
  return (
    <>
      <label className="search-field">
        查找已加载的历史
        <input
          value={q}
          placeholder="按已加载文件名筛选"
          onChange={(event) =>
            setParams(
              { tab: "history", q: event.target.value },
              { replace: true },
            )
          }
        />
      </label>
      <p className="muted">
        已加载 {events.length} 条记录{q && `，其中 ${visible.length} 条匹配`}
      </p>
      <ErrorNotice
        error={query.error}
        stale={events.length > 0}
        retry={() => void query.refetch()}
      />
      {query.isPending ? (
        <Loading />
      ) : visible.length ? (
        <div className="list-panel">
          {visible.map((event) => (
            <Link
              key={event.eventId}
              to={`/asset/${encodeURIComponent(event.assetId)}`}
              className="list-row"
            >
              <span className="row-icon">
                <Icon name={event.kind === "play" ? "play" : "image"} />
              </span>
              <div className="row-main">
                <strong>{event.fileName ?? "媒体记录"}</strong>
                <span>
                  {dateTime(event.createdAt)} ·{" "}
                  {event.kind === "progress"
                    ? `播放进度 ${duration(event.positionMs ?? 0)}`
                    : event.kind === "play"
                      ? "播放"
                      : "浏览"}
                </span>
              </div>
            </Link>
          ))}
        </div>
      ) : (
        !query.isError && (
          <EmptyState
            title={q ? "已加载的历史中没有匹配内容" : "还没有浏览记录"}
          />
        )
      )}
      {query.hasNextPage && (
        <div className="load-more">
          <button
            className="button"
            disabled={query.isFetchingNextPage}
            onClick={() => void query.fetchNextPage()}
          >
            加载更多历史
          </button>
        </div>
      )}
    </>
  );
}
