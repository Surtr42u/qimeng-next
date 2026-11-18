import { useParams } from "react-router-dom";
import { useAssets, mediaCards } from "../data/media";
import { MediaGrid } from "../components/MediaGrid";
import { SkeletonGrid } from "../components/LegacyLoading";
import { ErrorNotice } from "../components/Feedback";
import { BrowseTail } from "./BrowsePage";

export function TagCollectionPage() {
  const { name = "" } = useParams();
  const query = useAssets({ tag: name }, !!name);
  const items = query.data?.pages.flatMap(page => page.items) ?? [];
  return <div className="page" id="page-collection">
    <div className="page-head"><h2 data-page-heading tabIndex={-1}>{name}</h2><p>标签 · {query.data?.pages[0]?.totalMatched ?? "—"} 个文件</p></div>
    <ErrorNotice error={query.error} stale={!!query.data} retry={() => void query.refetch()} />
    {query.isPending ? <SkeletonGrid className="media-grid" /> : items.length ? <MediaGrid items={mediaCards(items)} /> : !query.isError && <p className="grid-empty">该标签下暂无内容。</p>}
    <BrowseTail query={query} count={items.length} />
  </div>;
}
