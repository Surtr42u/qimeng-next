import { useLayoutEffect, useRef, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { Shuffle } from "lucide-react";
import { useAuthor, useUpdateAuthor } from "../data/manage";
import { useAuthorProfileSummary } from "../data/author-profiles";
import { useRecommendations } from "../data/analytics";
import { useSession } from "../data/session";
import { mediaCards } from "../data/media";
import type { Asset } from "../data/media";
import { ErrorNotice, Loading } from "../components/Feedback";
import { Preview } from "../components/MediaGrid";
import { AuthorAvatar } from "./ProfilePresentation";
import { readDetailNavigation } from "../lib/detail-navigation";

// Display structure reused from the old detail/AuthorCard and UpNextList.
export function AssetAuthors({ asset }: { asset: Asset }) {
  if (!asset.authorIds.length) return null;
  return (
    <div className="rank-card author-card">
      <h3>作者</h3>
      <div>
        {asset.authorIds.map(id => (
          <AuthorRow key={id} id={id} fallbackName={id === asset.authorId ? asset.authorName : undefined} />
        ))}
      </div>
    </div>
  );
}

function AuthorRow({ id, fallbackName }: { id: string; fallbackName?: string }) {
  const query = useAuthor(id);
  const profile = useAuthorProfileSummary(id);
  const follow = useUpdateAuthor(id);
  const author = query.data;
  const name = author?.name ?? fallbackName;
  if (!name) return query.isPending ? <Loading text="作者加载中…" /> : <ErrorNotice error={query.error} retry={() => void query.refetch()} />;
  const href = `/authors/${encodeURIComponent(id)}`;
  const displayName = author?.kind === "cos" ? `${name} ·COS` : name;
  return (
    <>
      <div className="author-row">
        <Link to={href} aria-label={`查看${displayName}的作品`}>
          <AuthorAvatar name={name} avatar={profile.data?.avatar} className="author-avatar" />
        </Link>
        <div className="author-main">
          <Link className="author-name" to={href}>{displayName}</Link>
        </div>
        <button
          type="button"
          className={`follow-btn${author?.following ? "" : " follow-btn--idle"}`}
          disabled={!author || follow.isPending}
          onClick={() => author && follow.mutate({ expectedRevision: author.revision, following: !author.following })}
        >
          {author?.following ? "已关注" : "+ 关注"}
        </button>
      </div>
      <ErrorNotice error={query.error} stale={!!author} retry={() => void query.refetch()} />
      <ErrorNotice error={profile.error} stale={!!profile.data} retry={() => void profile.refetch()} />
      <ErrorNotice error={follow.error} />
    </>
  );
}

export function AssetUpNext({ asset }: { asset: Asset }) {
  const [seed, setSeed] = useState(0);
  const list = useRef<HTMLDivElement>(null);
  const location = useLocation();
  const navigation = readDetailNavigation(location.state, asset.id);
  const { instance } = useSession();
  const enabled = instance.capabilities.recommendations;
  const query = useRecommendations(seed, asset.cosWorkId ? "cos" : "regular", asset.mediaType, enabled);
  const items = (query.data?.pages.flatMap(page => page.items) ?? []).filter(item => item.id !== asset.id);
  const cards = mediaCards(items);
  const ids = items.map(item => item.id);
  useLayoutEffect(() => {
    if (!query.isFetching) list.current?.scrollTo({ top: 0, behavior: "instant" });
  }, [asset.id, seed, query.data, query.isFetching]);
  return (
    <div className="rank-card upnext-card">
      <div className="upnext-head">
        <h3>接下来播放</h3>
        <button className="shuffle-btn" disabled={!enabled || query.isFetching} onClick={() => setSeed(Date.now())}>
          <Shuffle size={13} className={query.isFetching ? "spin" : undefined} />换一批
        </button>
      </div>
      <ErrorNotice error={query.error} stale={items.length > 0} retry={() => void query.refetch()} />
      <div className="upnext-list" ref={list} tabIndex={0} role="region" aria-label="接下来播放列表" aria-busy={query.isFetching}>
        {items.map((item, index) => (
          <Link
            key={item.id}
            className="upnext-row"
            to={`/asset/${encodeURIComponent(item.id)}`}
            state={{ returnTo: navigation?.returnTo ?? "/", ids, index }}
          >
            <span className="upnext-thumb">
              <Preview item={cards[index]!} />
              {cards[index]?.duration && <span className="upnext-dur">{cards[index]!.duration}</span>}
            </span>
            <span className="upnext-info">
              <span className="upnext-title" title={item.cosWorkName || item.fileName}>{item.cosWorkName || item.fileName}</span>
              <span className="upnext-up">{item.authorName || item.source || ""}</span>
            </span>
          </Link>
        ))}
        {!items.length && !query.isError && (
          <p className="grid-empty">{!enabled ? "当前服务尚未提供推荐" : query.isPending ? "推荐加载中…" : "暂无推荐"}</p>
        )}
      </div>
    </div>
  );
}
