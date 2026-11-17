import { Link, useNavigate } from 'react-router-dom';
import { Activity, Clock3, Database, Eye, FolderOpen, History, Play, Repeat2, Tags, Users } from 'lucide-react';
import { useActivityRanking, useOverview, useTrends, useReview } from '../data/analytics';
import type { StatsRange } from '../data/analytics';
import type { Schema } from '../data/client';
import { useAuthors } from '../data/manage';
import { useSession } from '../data/session';
import { ErrorNotice, Loading, PageHeading } from '../components/Feedback';
import { WorkspaceHeading } from '../components/LegacyPageHeading';
import { DonutCard } from '../components/LegacyDonutCard';
import { LegacyTrendChart } from '../components/LegacyTrendChart';
import { BrowsingReviewContent } from '../components/LegacyReviewContent';
import { bytes, readableDuration } from '../lib/format';
import { activityWindows as windows, useActivityRange as useRange } from '../lib/activity-window';

const tones = [{ key: 'image', name: '图片', dot: 'd2', arc: 'arc-2' }, { key: 'video', name: '视频', dot: 'd1', arc: 'arc-1' }, { key: 'animated_image', name: '动图', dot: 'd4', arc: 'arc-4' }] as const;

export function DataPage() {
  const { range, change } = useRange();
  const overview = useOverview(range);
  const trends = useTrends(range);
  const review = useReview(range);
  const data = overview.data;
  const current = windows.find(item => item.value === range) ?? windows[0];
  const metrics = [
    { key: 'views', Icon: Eye, label: '总浏览次数', value: data?.totals.views.toLocaleString('zh-CN') ?? '—', note: '作品详情的浏览记录' },
    { key: 'plays', Icon: Play, label: '总播放次数', value: data?.totals.plays.toLocaleString('zh-CN') ?? '—', note: '视频播放记录' },
    { key: 'time', Icon: Clock3, label: '总浏览时长', value: data ? readableDuration(data.totals.seconds) : '—', note: '详情页的有效停留时间' },
    { key: 'average', Icon: Repeat2, label: '平均浏览次数', value: data?.averageViewsPerFile == null ? '—' : data.averageViewsPerFile.toFixed(1), note: '每个被浏览过的现存文件' },
  ];
  const inventorySlices = data && data.inventory.assets > 0 ? tones.flatMap(tone => {
    const dimension = data.mediaTypes.find(item => item.key === tone.key);
    return dimension ? [{ ...tone, value: dimension.assets.toLocaleString('zh-CN'), pct: dimension.assets / data.inventory.assets * 100 }] : [];
  }) : [];
  const viewSlices = data && data.totals.views > 0 ? tones.flatMap(tone => {
    const dimension = data.mediaTypes.find(item => item.key === tone.key);
    return dimension ? [{ ...tone, value: dimension.views.toLocaleString('zh-CN'), pct: dimension.views / data.totals.views * 100 }] : [];
  }) : [];
  return <div className="page personal-workspace data-workspace" id="page-data">
    <WorkspaceHeading title="数据" description="浏览习惯、内容热度与媒体库概况" actions={<Link className="workspace-link" to="/manage"><FolderOpen aria-hidden="true" />数据管理</Link>} />
    <div className="data-window-bar"><div className="workspace-segments" role="group" aria-label="统计时段">{windows.map(item => <button className={range === item.value ? 'active' : ''} key={item.value} type="button" aria-pressed={range === item.value} onClick={() => change(item.value)}>{item.label}</button>)}</div><span>{current.note}</span></div>
    <ErrorNotice error={overview.error} stale={!!data} retry={() => void overview.refetch()} />
    <div className="data-metrics" role="group" aria-label="所选时段指标">{metrics.map(({ Icon, ...metric }) => <section className={`metric-card data-metric data-metric--${metric.key}`} key={metric.key}><div className="data-metric-label"><Icon aria-hidden="true" /><span>{metric.label}</span></div><strong>{metric.value}</strong><small>{metric.note}</small></section>)}</div>
    <div className="data-primary-grid">
      <section className="chart-card data-trend-panel" aria-label="浏览与播放趋势"><div className="workspace-card-head"><h3><Activity aria-hidden="true" />浏览与播放趋势</h3><span>{current.label}</span></div><p className="rank-note">浏览和播放分别统计 · {current.note}</p>
        {data && <div className="data-coverage"><span>看过 <b>{data.viewedAssets.toLocaleString('zh-CN')}</b> 个文件</span><span>涉及 <b>{data.viewedAuthors.toLocaleString('zh-CN')}</b> 位作者</span><span><b>{data.viewedTags.toLocaleString('zh-CN')}</b> 个标签</span></div>}
        <ErrorNotice error={trends.error} stale={!!trends.data} retry={() => void trends.refetch()} />
        {trends.isPending ? <Loading /> : trends.data && (trends.data.items.every(item => item.views === 0 && item.plays === 0) ? <p className="workspace-empty-note">暂无趋势数据</p> : <LegacyTrendChart items={trends.data.items} />)}
      </section>
      <section className="chart-card data-inventory-panel" aria-label="媒体库概况"><div className="workspace-card-head"><h3><Database aria-hidden="true" />媒体库概况</h3><span>当前库存</span></div><p className="rank-note">不随上方统计时段改变</p><dl className="data-inventory-metrics"><div><dt>总文件</dt><dd>{data?.inventory.assets.toLocaleString('zh-CN') ?? '—'}</dd></div><div><dt>总容量</dt><dd>{data ? bytes(data.inventory.bytes) : '—'}</dd></div></dl><h4>内容类型分布</h4>
        {inventorySlices.length ? <><div className="data-type-bar" aria-hidden="true">{inventorySlices.map(item => <i key={item.key} className={item.dot} style={{ width: `${item.pct}%` }} />)}</div><ul className="legend data-inventory-types">{inventorySlices.map(item => <li key={item.key}><i className={`dot ${item.dot}`} /><span>{item.name}</span><b>{item.value}</b><small>{item.pct.toFixed(1)}%</small></li>)}</ul></> : <p className="rank-note">{overview.isPending ? '正在读取库存…' : data?.inventory.assets === 0 ? '暂无库存文件' : '分类信息暂不可用'}</p>}
      </section>
    </div>
    <div className="data-secondary-grid"><section className="rank-card data-content-panel" aria-label="浏览回顾"><div className="workspace-card-head"><h3><History aria-hidden="true" />浏览回顾</h3><Link to={`/data/review?range=${range}`}>查看回顾</Link></div><p className="rank-note">探索与回访 · 兴趣变化 · 浏览习惯</p><ErrorNotice error={review.error} stale={!!review.data} retry={() => void review.refetch()} />{review.isPending ? <Loading /> : review.data && <ReviewMetrics review={review.data} />}</section>
      {data || overview.isPending ? <DonutCard title="浏览量占比" sub={`按类型的浏览次数 · ${current.label}`} slices={viewSlices} loading={overview.isPending} emptyText="此窗口暂无浏览记录" centerValue={data?.totals.views.toLocaleString('zh-CN')} centerLabel="浏览次数" /> : <section className="donut-card"><h3>浏览量占比</h3><p className="rank-note">统计概览尚不可用，恢复读取后显示。</p></section>}
    </div>
    <div className="data-rank-grid"><DashboardRank kind="tags" range={range} /><DashboardRank kind="authors" range={range} /><AuthorOverview /></div>
  </div>;
}
function ReviewMetrics({ review }: { review: Schema<'BrowsingReview'> }) {
  const metrics = [['首次浏览', review.firstViews, '次'], ['再次浏览', review.returnViews, '次'], ['浏览内容', review.distinctAssets, '个'], ['活跃天数', review.activeDays, '天']] as const;
  return <dl className="review-metrics">{metrics.map(([label, value, unit]) => <div key={label}><dt>{label}</dt><dd>{value.toLocaleString('zh-CN')}<small>{unit}</small></dd></div>)}</dl>;
}
function DashboardRank({ kind, range }: { kind: 'authors' | 'tags'; range: StatsRange }) {
  const query = useActivityRanking(kind, range, 'views', 5);
  const items = query.data?.pages[0]?.items ?? [];
  const title = kind === 'authors' ? '作者榜' : '标签榜';
  const Icon = kind === 'authors' ? Users : Tags;
  const navigate = useNavigate();
  const open = (item: Schema<'ActivityRankingItem'>) => navigate(kind === 'authors' ? `/authors/${encodeURIComponent(item.id)}` : `/album?tags=${encodeURIComponent(item.name)}`);
  return <section className="rank-card" aria-label={title}><div className="workspace-card-head"><h3><Icon aria-hidden="true" />{title}</h3><Link to={`/data/rankings?kind=${kind}&range=${range}`}>查看全部</Link></div><p className="rank-note">Top 5 · 窗口浏览次数</p><ErrorNotice error={query.error} stale={items.length > 0} retry={() => void query.refetch()} />{query.isPending ? <Loading /> : items.length ? <ul>{items.map(item => <li key={item.id} role="button" tabIndex={0} onClick={() => open(item)} onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); open(item); } }}><span className="rank-name">{item.name}</span><b>{item.views.toLocaleString('zh-CN')}</b></li>)}</ul> : !query.isError && <p className="rank-note">暂无数据</p>}</section>;
}
function AuthorOverview() {
  const query = useAuthors('', undefined, true, { sort: 'files' });
  const following = useAuthors('', true);
  const items = query.data?.pages[0]?.items.slice(0, 5) ?? [];
  const navigate = useNavigate();
  return <section className="rank-card" aria-label="作者总览"><div className="workspace-card-head"><h3><Users aria-hidden="true" />作者总览</h3><Link to="/authors">管理作者</Link></div><p className="rank-note">{query.data ? `${query.data.pages[0]?.totalMatched} 位作者 · 已关注 ${following.data?.pages[0]?.totalMatched ?? '—'}` : '作者管理预览'}</p><ErrorNotice error={query.error ?? following.error} stale={items.length > 0} retry={() => { void query.refetch(); void following.refetch(); }} />{query.isPending ? <Loading /> : items.length ? <ul>{items.map(item => <li key={item.id} role="button" tabIndex={0} onClick={() => navigate(`/authors/${encodeURIComponent(item.id)}`)} onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); navigate(`/authors/${encodeURIComponent(item.id)}`); } }}><span className="rank-name">{item.name}</span></li>)}</ul> : !query.isError && <p className="rank-note">暂无数据</p>}</section>;
}

export function ReviewPage() {
  const { range, change } = useRange();
  const query = useReview(range);
  return <div className="page personal-workspace data-workspace">
    <WorkspaceHeading title="浏览回顾" description="看看自己如何探索、回访，以及兴趣如何变化" actions={<Link className="workspace-link" to={`/data?range=${range}`}>返回数据</Link>} />
    <div className="data-window-bar"><div className="workspace-segments" role="group" aria-label="回顾时段">{windows.map(item => <button key={item.value} type="button" className={range === item.value ? 'active' : ''} aria-pressed={range === item.value} onClick={() => change(item.value)}>{item.label}</button>)}</div></div>
    <ErrorNotice error={query.error} stale={!!query.data} retry={() => void query.refetch()} />
    {query.isPending ? <Loading /> : query.data && <BrowsingReviewContent review={query.data} />}
  </div>;
}
export function MaintenancePage() {
  const { instance } = useSession();
  return (
    <>
      <PageHeading title="服务与维护" subtitle="当前连接与维护入口。" />
      <section className="panel">
        <h2>{instance.name}</h2>
        <dl className="facts">
          <div>
            <dt>协议</dt>
            <dd>{instance.apiVersion}</dd>
          </div>
          <div>
            <dt>智能执行器</dt>
            <dd>{instance.capabilities.aiConfigured ? "已配置" : "未配置"}</dd>
          </div>
        </dl>
        <p className="muted">
          已配置不代表执行器当前在线，逐批进度以任务报告为准。
        </p>
      </section>
      <div className="manage-grid section-space">
        <Link className="panel manage-card" to="/manage/upload">
          <h2>上传与入库</h2>
        </Link>
        <Link className="panel manage-card" to="/manage/tasks">
          <h2>任务与报告</h2>
        </Link>
        <Link className="panel manage-card" to="/manage/trash">
          <h2>回收站</h2>
        </Link>
        <Link className="panel manage-card" to="/manage/backup">
          <h2>备份与归档</h2>
        </Link>
      </div>
    </>
  );
}
