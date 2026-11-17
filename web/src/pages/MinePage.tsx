import { Link, useSearchParams } from "react-router-dom";
import {
  BarChart3,
  Database,
  Heart,
  History,
  Settings,
  Users,
} from "lucide-react";
import { useAssets, useAsset, useHistory, mediaCards } from "../data/media";
import {
  useAuthors,
  useLibraries,
  useStats,
  useUpdateAuthor,
} from "../data/manage";
import type { Schema } from "../data/client";
import { ErrorNotice, Loading } from "../components/Feedback";
import { MediaGrid, Preview } from "../components/MediaGrid";
import { MineContentTabs } from "../components/LegacyMineTabs";
import { AutoPageSentinel } from "../components/AutoPageSentinel";
import { SearchIcon } from "../components/LegacyShellIcons";
import { bytes, dateTime } from "../lib/format";
import { useNearViewport } from "../lib/visibility";

// Existing MinePage presentation is retained; all reads and mutations use the new data layer.
export function MinePage() {
  const [params, setParams] = useSearchParams();
  const value = params.get("tab");
  const tab =
    value === "fav" || value === "favorites"
      ? "fav"
      : value === "history"
        ? "history"
        : "follow";
  const libraries = useLibraries();
  const stats = useStats();
  return (
    <div className="page personal-workspace mine-workspace" id="page-mine">
      <header className="workspace-heading">
        <div>
          <h2 data-page-heading tabIndex={-1}>
            我的
          </h2>
          <p>关注的作者、收藏的作品与最近的浏览足迹</p>
        </div>
        <div className="workspace-heading-actions">
          <Link className="workspace-link" to="/data">
            <BarChart3 aria-hidden="true" />
            查看数据
          </Link>
          <Link className="workspace-link" to="/settings">
            <Settings aria-hidden="true" />
            设置
          </Link>
        </div>
      </header>
      <section className="profile-card mine-hero">
        <div className="mine-hero-main">
          <span className="mine-hero-icon">
            <Database aria-hidden="true" />
          </span>
          <div>
            <h3>我的媒体空间</h3>
            <p>当前服务器的媒体库与内容</p>
            <Link to="/manage/libraries">管理媒体库</Link>
          </div>
        </div>
        <dl className="profile-stats">
          <div>
            <dd>
              {libraries.data?.items.length.toLocaleString("zh-CN") ?? "—"}
            </dd>
            <dt>已登记媒体库</dt>
          </div>
          <div>
            <dd>{stats.data?.assets.toLocaleString("zh-CN") ?? "—"}</dd>
            <dt>文件总数</dt>
          </div>
          <div>
            <dd>{stats.data ? bytes(stats.data.bytes) : "—"}</dd>
            <dt>总大小</dt>
          </div>
        </dl>
      </section>
      <ErrorNotice
        error={libraries.error ?? stats.error}
        stale={!!libraries.data || !!stats.data}
        retry={() => {
          void libraries.refetch();
          void stats.refetch();
        }}
      />
      <MineContentTabs
        tab={tab}
        onChange={(next) =>
          setParams(
            (current) => {
              const result = new URLSearchParams(current);
              result.set("tab", next);
              return result;
            },
            { replace: true },
          )
        }
      />
      <section
        className="m-pane mine-pane"
        role="tabpanel"
        id={`mine-panel-${tab}`}
        aria-labelledby={`mine-tab-${tab}`}
        tabIndex={0}
      >
        {tab === "follow" ? (
          <Following />
        ) : tab === "fav" ? (
          <Favorites />
        ) : (
          <HistoryList />
        )}
      </section>
    </div>
  );
}

function Following() {
  const query = useAuthors("", true);
  const authors = query.data?.pages.flatMap((page) => page.items) ?? [];
  return (
    <>
      <div className="workspace-card-head">
        <div>
          <h3>
            <Users aria-hidden="true" />
            关注作者
          </h3>
          <p className="rank-note">
            {query.data
              ? `已加载 ${authors.length} 位关注作者 · 点名字查看作品`
              : "集中查看你关注的创作者"}
          </p>
        </div>
        <Link to="/authors">管理作者</Link>
      </div>
      <ErrorNotice
        error={query.error}
        stale={authors.length > 0}
        retry={() => void query.refetch()}
      />
      {query.isPending ? (
        <Loading />
      ) : authors.length ? (
        <div className="follow-list mine-follow-grid">
          {authors.map((author) => (
            <FollowingCard key={author.id} author={author} />
          ))}
        </div>
      ) : (
        !query.isError && (
          <div className="workspace-empty">
            <Users aria-hidden="true" />
            <h4>暂无关注作者</h4>
            <p>关注后，作者和作品入口会集中显示在这里。</p>
            <Link to="/authors">去管理作者</Link>
          </div>
        )
      )}
      {query.hasNextPage && (
        <div className="workspace-load-more">
          <button
            className="pill"
            disabled={query.isFetchingNextPage}
            onClick={() => void query.fetchNextPage()}
          >
            加载更多关注作者
          </button>
        </div>
      )}
    </>
  );
}
function FollowingCard({ author }: { author: Schema<"Author"> }) {
  const mutation = useUpdateAuthor(author.id);
  return (
    <article className="follow-card">
      <Link
        className="mine-follow-content"
        to={`/authors/${encodeURIComponent(author.id)}`}
      >
        <span className="mine-author-avatar" aria-hidden="true">
          {Array.from(author.name)[0]}
        </span>
        <div className="follow-info">
          <p className="follow-name">{author.name}</p>
          <p className="follow-sub">{author.fileCount ?? "—"} 个文件</p>
        </div>
      </Link>
      <button
        className="follow-btn"
        type="button"
        disabled={mutation.isPending}
        aria-label={`取消关注 ${author.name}`}
        onClick={() =>
          mutation.mutate({
            expectedRevision: author.revision,
            following: false,
          })
        }
      >
        {mutation.isPending ? "处理中…" : "已关注"}
      </button>
      <ErrorNotice error={mutation.error} />
    </article>
  );
}
function Favorites() {
  const query = useAssets({ favorite: true, sort: "favorited", order: "desc" });
  const items = query.data?.pages.flatMap((page) => page.items) ?? [];
  return (
    <>
      <div className="workspace-card-head">
        <div>
          <h3>
            <Heart aria-hidden="true" />
            收藏作品
          </h3>
          <p className="rank-note">
            按收藏时间从新到旧 ·{" "}
            {query.data
              ? `已加载 ${items.length} 项 / 共 ${query.data.pages[0]?.totalMatched ?? "—"} 项`
              : "选择喜欢的作品，方便下次找到"}
          </p>
        </div>
      </div>
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
          <div className="workspace-empty">
            <Heart aria-hidden="true" />
            <h4>暂无收藏内容</h4>
            <p>浏览作品时收藏，喜欢的内容就会留在这里。</p>
            <Link to="/">去浏览作品</Link>
          </div>
        )
      )}
      <AutoPageSentinel
        enabled={query.hasNextPage}
        loading={query.isFetchingNextPage}
        failed={query.isFetchNextPageError}
        load={() => query.fetchNextPage({ cancelRefetch: false })}
      />
      {query.hasNextPage && (
        <div className="workspace-load-more">
          <button
            className="pill"
            disabled={query.isFetchingNextPage}
            onClick={() => void query.fetchNextPage({ cancelRefetch: false })}
          >
            加载更多收藏
          </button>
        </div>
      )}
    </>
  );
}
function HistoryList() {
  const query = useHistory();
  const [params, setParams] = useSearchParams();
  const search = params.get("q") ?? "";
  const items = query.data?.pages.flatMap((page) => page.items) ?? [];
  const shown = items.filter(
    (item) =>
      !search ||
      item.fileName?.toLocaleLowerCase().includes(search.toLocaleLowerCase()),
  );
  const groups = new Map<string, typeof shown>();
  for (const item of shown) {
    const day = new Date(item.createdAt).toLocaleDateString("zh-CN");
    groups.set(day, [...(groups.get(day) ?? []), item]);
  }
  return (
    <>
      <div className="workspace-card-head">
        <div>
          <h3>
            <History aria-hidden="true" />
            浏览历史
          </h3>
          <p className="rank-note">
            每个文件最近一次浏览 · 已加载 {items.length} 项
          </p>
        </div>
        <div className="hist-search mine-history-search">
          <input
            type="search"
            aria-label="筛选已加载历史"
            placeholder="筛选已加载记录的文件名"
            value={search}
            onChange={(event) =>
              setParams(
                (current) => {
                  const next = new URLSearchParams(current);
                  event.target.value
                    ? next.set("q", event.target.value)
                    : next.delete("q");
                  return next;
                },
                { replace: true },
              )
            }
          />
          <SearchIcon />
        </div>
      </div>
      {search && (
        <div className="mine-history-filter">
          <p>仅筛选已加载的记录，可继续加载查找更多。</p>
          <button
            className="workspace-link"
            onClick={() => setParams({ tab: "history" }, { replace: true })}
          >
            清空筛选
          </button>
        </div>
      )}
      <ErrorNotice
        error={query.error}
        stale={items.length > 0}
        retry={() => void query.refetch()}
      />
      {[...groups].map(([label, events]) => (
        <div className="hist-group2" key={label}>
          <h3>
            {label}
            <span>{events.length} 项</span>
          </h3>
          <div className="hist-grid">
            {events.map((event) => (
              <HistoryCard key={event.eventId} event={event} />
            ))}
          </div>
        </div>
      ))}
      {query.isPending ? (
        <Loading />
      ) : (
        !shown.length &&
        !query.isError && (
          <div className="workspace-empty">
            <History aria-hidden="true" />
            <h4>{search ? "已加载记录中没有匹配项" : "暂无浏览记录"}</h4>
            <p>
              {search
                ? "换个文件名关键词，或继续加载更多历史。"
                : "打开作品后，最近浏览的内容会出现在这里。"}
            </p>
            {!search && <Link to="/">去浏览作品</Link>}
          </div>
        )
      )}
      {!search && (
        <AutoPageSentinel
          enabled={query.hasNextPage}
          loading={query.isFetchingNextPage}
          failed={query.isFetchNextPageError}
          load={() => query.fetchNextPage({ cancelRefetch: false })}
        />
      )}
      {query.hasNextPage && (
        <div className="workspace-load-more">
          <button
            className="pill"
            disabled={query.isFetchingNextPage}
            onClick={() => void query.fetchNextPage({ cancelRefetch: false })}
          >
            加载更多历史
          </button>
        </div>
      )}
    </>
  );
}
function HistoryCard({ event }: { event: Schema<"HistoryEvent"> }) {
  const { ref, visible } = useNearViewport<HTMLAnchorElement>();
  const query = useAsset(event.assetId, visible && !event.asset);
  const asset = event.asset ?? query.data;
  const item = asset ? mediaCards([asset])[0] : undefined;
  return (
    <Link
      ref={ref}
      className="hist-card card"
      to={`/asset/${encodeURIComponent(event.assetId)}`}
      aria-label={`打开 ${event.fileName ?? "媒体记录"}`}
    >
      <div className="hc-cover card--cover">
        {item ? (
          <Preview item={item} />
        ) : (
          <span className="media-placeholder">
            {query.isError ? "预览信息读取失败" : "正在读取预览…"}
          </span>
        )}
        <span className="hc-time">{dateTime(event.createdAt)}</span>
        {item?.duration && <span className="hc-duration">{item.duration}</span>}
      </div>
      <p className="card--title">
        {event.fileName ?? item?.title ?? "媒体记录"}
      </p>
      {item?.caption && (
        <p className="card--meta">
          <span className="card--up">{item.caption}</span>
        </p>
      )}
    </Link>
  );
}
