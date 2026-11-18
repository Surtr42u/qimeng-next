import { useEffect } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useActivityRanking, useRankings, type RankingPeriod, type StatsRange } from "../data/analytics";
import { mediaCards } from "../data/media";
import { Preview } from "../components/MediaGrid";
import { ErrorNotice, Loading } from "../components/Feedback";
import { WorkspaceHeading } from "../components/LegacyPageHeading";
import { activityWindows } from "../lib/activity-window";
import { readableDuration } from "../lib/format";

const periods: Record<string, RankingPeriod> = { "7d": "week", "30d": "month", "90d": "quarter", all: "all" };
export function ActivityRankingPage() {
  const [params] = useSearchParams();
  const requested = params.get("kind");
  const kind = requested === "tags" || requested === "assets" || requested === "content" ? requested : "authors";
  const rangeParam = params.get("range");
  const range = activityWindows.find(option => option.value === (rangeParam === "day" ? "30d" : rangeParam)) ?? activityWindows[3];
  const title = kind === "tags" ? "标签榜" : kind === "authors" ? "作者榜" : "内容榜";
  return <div className="page personal-workspace data-workspace" id="page-ranks">
    <WorkspaceHeading title={title} description={range.label + " · 前 " + (kind === "content" ? 200 : 50) + " 项"}
      actions={<Link className="workspace-link" to={"/data?range=" + range.value}>返回数据</Link>} />
    <div className="rank-card">{kind === "content" ? <HeatRank period={periods[range.value]!} />
      : <ActivityRank kind={kind} range={range.value} duration={kind === "assets" && params.get("metric") === "seconds"} />}</div>
  </div>;
}
function ActivityRank({ kind, range, duration }: { kind: "tags" | "authors" | "assets"; range: StatsRange; duration: boolean }) {
  const query = useActivityRanking(kind, range, duration ? "seconds" : "views", 50);
  const navigate = useNavigate();
  const rows = query.data?.pages[0]?.items ?? [];
  return <><p className="rank-note">{duration ? "按详情停留" : "按窗口浏览"}</p>
    <ErrorNotice error={query.error} stale={!!query.data} retry={() => void query.refetch()} />
    {query.isPending ? <Loading /> : rows.length ? <ul>{rows.map(item => {
      const path = kind === "tags" ? "/collection/tag/" + encodeURIComponent(item.name) : kind === "authors" ? "/authors/" + encodeURIComponent(item.id) : item.asset ? "/asset/" + encodeURIComponent(item.asset.id) : undefined;
      return <li key={item.id} role={path ? "button" : undefined} tabIndex={path ? 0 : undefined} onClick={() => {if(path)navigate(path);}} onKeyDown={event=>{if(path&&(event.key==="Enter"||event.key===" ")){event.preventDefault();navigate(path);}}}>
        <span className="rank-name">{item.name}</span><b>{duration ? readableDuration(item.value) : item.views.toLocaleString("zh-CN")}</b>
      </li>;})}</ul> : !query.isError && <p className="rank-note">暂无数据</p>}
  </>;
}
function HeatRank({ period }: { period: RankingPeriod }) {
  const navigate = useNavigate();
  const query = useRankings(period, true, undefined, 100);
  const items = query.data?.pages.flatMap(page => page.items.map(row => row.asset)).slice(0,200) ?? [];
  useEffect(() => { if(items.length<200 && query.hasNextPage && !query.isPlaceholderData && !query.isFetching && !query.isError)void query.fetchNextPage(); }, [items.length,query.hasNextPage,query.isPlaceholderData,query.isFetching,query.isError,query.fetchNextPage]);
  return <><p className="rank-note">按热度</p>
    <ErrorNotice error={query.error} stale={!!query.data} retry={() => void (query.isFetchNextPageError ? query.fetchNextPage() : query.refetch())} />
    {query.isPending || query.isPlaceholderData ? <Loading /> : items.length ? <div className="rank-cards">{mediaCards(items).map(item => {
      const open=()=>navigate("/asset/"+encodeURIComponent(item.id));return <div className="rank-item" key={item.id} role="button" tabIndex={0} onClick={open} onKeyDown={e=>{if(e.key==="Enter"||e.key===" "){e.preventDefault();open();}}}><div className="rank-cover"><Preview item={item} /></div><p className="rank-title">{item.title}</p></div>;
    })}</div> : !query.isError && <p className="rank-note">暂无数据</p>}
  </>;
}
