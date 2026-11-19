import { lazy, Suspense, useCallback, useEffect, useRef, useState } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import { ChevronLeft, ChevronRight, FolderInput, Pencil, Star, ThumbsUp, Trash2 } from "lucide-react";
import { useAsset, useAssetActions, originalUrl, mediaCardDate, safeMediaUrl, useThumbnailURL } from "../data/media";
import type { Asset } from "../data/media";
import { useDetailSession, usePlaybackResume } from "../data/detail-session";
import { useTimeline } from "../data/catalog";
import type { FileJob } from "../data/file-actions";
import { ErrorNotice, Loading } from "../components/Feedback";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ImageViewer } from "../components/ImageViewer";
import { ImageStage } from "../components/ImageStage";
import { AssetFileName } from "../components/AssetFileName";
import { bytes, count } from "../lib/format";
import { readDetailNavigation } from "../lib/detail-navigation";
import type { DetailNavigation } from "../lib/detail-navigation";
import { newId } from "../lib/id";
import { AssetAuthors, AssetUpNext } from "./AssetSidebar";
import { AssetTagDialog } from "./AssetTagDialog";
import { MoveFilesDialog, FileJobsPanel } from "./FileManagementDialogs";

const VideoPlayer = lazy(async () => ({ default: (await import("../components/VideoPlayer")).VideoPlayer }));

export function AssetPage() {
  const { id = "" } = useParams();
  const query = useAsset(id, true, true);
  const location = useLocation();
  const navigate = useNavigate();
  const navigation = readDetailNavigation(location.state, id);
  // This intent survives image → video → image within the same detail chain.
  const [viewer, setViewer] = useState(false);
  const overlay = useRef<HTMLDivElement>(null);
  useEffect(() => { overlay.current?.scrollTo(0, 0); }, [id]);
  const go = useCallback((delta: -1 | 1) => {
    if (!navigation) return;
    const index = navigation.index + delta;
    const next = navigation.ids[index];
    if (next) navigate(`/asset/${encodeURIComponent(next)}`, { state: { ...navigation, index } });
  }, [navigation, navigate]);
  return (
    <div ref={overlay} className="page asset-overlay" id="page-asset">
      {query.isPlaceholderData && <p className="rank-note" role="status">正在读取下一件…</p>}
      <ErrorNotice error={query.error} stale={!!query.data} retry={() => void query.refetch()} />
      {!query.data ? (
        query.isPending ? <Loading text="正在打开媒体…" /> : null
      ) : (
        <div inert={query.isPlaceholderData || undefined}>
          <AssetDetail key={`${query.data.id}:${query.data.contentVersion}`} asset={query.data} navigation={navigation} go={go} openViewer={() => setViewer(true)} />
        </div>
      )}
      {viewer && query.data && query.data.mediaType !== "video" && (
        <ImageViewer
          src={originalUrl(query.data)}
          title={query.data.fileName}
          busy={query.isPlaceholderData}
          onClose={() => setViewer(false)}
          focusFallback={() => document.querySelector<HTMLButtonElement>(".asset-img-open")}
          onPrev={navigation && navigation.index > 0 ? () => go(-1) : undefined}
          onNext={navigation && navigation.index < navigation.ids.length - 1 ? () => go(1) : undefined}
          position={navigation ? { index: navigation.index + 1, total: navigation.ids.length } : undefined}
        />
      )}
    </div>
  );
}

function AssetDetail({ asset, navigation, go, openViewer }: {
  asset: Asset;
  navigation?: DetailNavigation;
  go: (delta: -1 | 1) => void;
  openViewer: () => void;
}) {
  const navigate = useNavigate();
  const location = useLocation();
  const actions = useAssetActions(asset.id);
  const event = useDetailSession(asset.id, asset.contentVersion);
  const timeline = useTimeline(asset.id, asset.mediaType === "video");
  const [dialog, setDialog] = useState<"tags" | "move" | "trash" | null>(null);
  const [likeBounce, setLikeBounce] = useState(false);
  const [jobs, setJobs] = useState<FileJob[]>([]);
  const trashKey = useRef(newId());
  const src = originalUrl(asset);
  const poster = useThumbnailURL(asset.thumbnailUrl ? safeMediaUrl(asset.thumbnailUrl, asset.contentVersion) : undefined);
  const marks = (timeline.data?.pages.flatMap(page => page.items) ?? [])
    .filter(mark => mark.contentVersion === asset.contentVersion)
    .map(mark => ({ time: mark.positionMs / 1000, text: mark.label }));
  const close = useCallback(() => setDialog(null), []);
  const report = useCallback((value: FileJob[]) => setJobs(value), []);
  const date = mediaCardDate(asset.fileDate);
  return (
    <>
      <div className={`detail-layout${asset.mediaType === "video" ? " detail-layout--video" : ""}`}>
        <div className="detail-main">
          <div className="asset-stage">
            {asset.mediaType === "video" ? (
              <Suspense fallback={<Loading text="正在加载播放器…" />}>
                <AssetVideo asset={asset} src={src} poster={poster} marks={marks} event={event} />
              </Suspense>
            ) : <ImageStage src={src} title={asset.fileName} onOpen={openViewer} />}
          </div>
          {navigation && (
            <div className="asset-pager">
              <button className="asset-pager__btn" disabled={navigation.index <= 0} onClick={() => go(-1)}><ChevronLeft size={14} />上一件</button>
              <span className="asset-pager__count">{navigation.index + 1} / {navigation.ids.length}</span>
              <button className="asset-pager__btn" disabled={navigation.index >= navigation.ids.length - 1} onClick={() => go(1)}>下一件<ChevronRight size={14} /></button>
            </div>
          )}
          <h1 className="detail-title" data-page-heading tabIndex={-1}>{asset.cosWorkName || asset.fileName}</h1>
          <AssetFileName fileName={asset.fileName} />
          <p className="detail-meta">
            <span>浏览 {count(asset.viewCount)}</span>
            <span>播放 {count(asset.playCount)}</span>
            <span>{bytes(asset.size)}</span>
            {asset.width && asset.height ? <span>{asset.width}×{asset.height}</span> : null}
            <span>{date || "日期未记录"}</span>
            {asset.source && <span>{asset.source}</span>}
            {!!asset.taxonomyCandidates?.length && <span>待核对的出处或角色：{asset.taxonomyCandidates.join("、")}</span>}
          </p>
          <div className="detail-actions">
            <button
              className={`detail-act${asset.liked ? " active" : ""}${likeBounce ? " bounce" : ""}`}
              aria-label={`${asset.liked ? "取消今日赞" : "点赞"} · ${asset.likeCount}`}
              title={asset.liked ? "取消今日赞" : "点赞（每资产每日一次）"}
              aria-pressed={asset.liked}
              disabled={actions.reaction.isPending}
              onClick={() => { setLikeBounce(!asset.liked); actions.reaction.mutate({ kind: "todayLike", value: !asset.liked, key: newId() }); }}
              onAnimationEnd={() => setLikeBounce(false)}
            ><ThumbsUp /><b>{count(asset.likeCount)}</b></button>
            <button className={`detail-act${asset.favorite ? " active" : ""}`} title={asset.favorite ? "取消收藏" : "收藏"} aria-pressed={asset.favorite} disabled={actions.reaction.isPending} onClick={() => actions.reaction.mutate({ kind: "favorite", value: !asset.favorite, key: newId() })}><Star /><b>{asset.favorite ? "已收藏" : "收藏"}</b></button>
            <button className="detail-act" title="编辑关联作者与来源" onClick={() => navigate(`/asset/${encodeURIComponent(asset.id)}/edit`, { state: location.state })}><Pencil /><b>编辑</b></button>
            <button className="detail-act" title="移动到其他目录 / 重命名" onClick={() => setDialog("move")}><FolderInput /><b>整理</b></button>
            <button className="detail-act" title="移入回收站（可在维护页恢复）" onClick={() => setDialog("trash")}><Trash2 /><b>删除</b></button>
          </div>
          <ErrorNotice error={actions.reaction.error} retry={() => {
            if (actions.reaction.variables) actions.reaction.mutate(actions.reaction.variables);
          }} />
          <ErrorNotice error={event.error} retry={event.retry} />
          <div className="detail-tags">
            {asset.tags.map(tag => <span className="pill" key={tag}>{tag}</span>)}
            <button className="pill detail-tag-manage" aria-label="管理标签" onClick={() => setDialog("tags")}>{asset.tags.length ? "管理" : "+ 添加标签"}</button>
          </div>
          <FileJobsPanel jobs={jobs} running={false} action="移动" dismiss={() => setJobs([])} />
          {asset.mediaType === "video" && <ErrorNotice error={timeline.error} stale={!!timeline.data} retry={() => void timeline.refetch()} />}
        </div>
        <aside className="detail-side">
          <AssetAuthors asset={asset} />
          <AssetUpNext asset={asset} />
        </aside>
      </div>
      {dialog === "tags" && <AssetTagDialog asset={asset} close={close} />}
      {dialog === "move" && <MoveFilesDialog libraryId={asset.libraryId} directory={asset.relativePath.split("/").slice(0, -1).join("/")} targets={[asset]} close={close} report={report} />}
      {dialog === "trash" && (
        <ConfirmDialog title="移入回收站？" danger confirmLabel="移入回收站" busy={actions.trash.isPending} onCancel={close} onConfirm={() => actions.trash.mutate({ revision: asset.revision, key: trashKey.current }, { onSuccess: () => navigate(navigation?.returnTo ?? "/album", { replace: true }) })}>
          <p>{asset.fileName}</p><p>媒体将从浏览列表移除，可以在回收站恢复原身份和关联。</p>
          <ErrorNotice error={actions.trash.error} />
        </ConfirmDialog>
      )}
    </>
  );
}

function AssetVideo({ asset, src, poster, marks, event }: {
  asset: Asset; src: string; poster?: string; marks: { time: number; text: string }[]; event: ReturnType<typeof useDetailSession>;
}) {
  const resume = usePlaybackResume(asset.id, asset.contentVersion, event.id);
  return <VideoPlayer src={src} title={asset.fileName} poster={poster} highlights={marks}
    initialPosition={resume?.positionMs ?? 0} progressMatches={resume?.positionContentVersion === asset.contentVersion}
    onPlay={event.play} onProgress={event.progress} />;
}
