import { Link, useSearchParams } from 'react-router-dom';
import { SegmentedControl } from '@ui/GlassNavigation';
import { useOverview, useTrends, useReview } from '../data/analytics';
import type { StatsRange } from '../data/analytics';
import type { Schema } from '../data/client';
import { useSession } from '../data/session';
import { ErrorNotice, Loading, PageHeading } from '../components/Feedback';
import { HourChart, TrendChart } from '../components/ActivityCharts';
import { bytes, duration } from '../lib/format';

const windows = [{ value: '7d', label: '7天' }, { value: '30d', label: '30天' }, { value: '90d', label: '90天' }, { value: 'all', label: '全部' }] as const;
const names: Record<string, string> = { image: '图片', animated_image: '动图', video: '视频', regular: '常规', cos: 'COS' };
function useRange() {
  const [params, setParams] = useSearchParams();
  const value = params.get('range') ?? '7d';
  const range = (windows.some(item => item.value === value) ? value : '7d') as StatsRange;
  return { range, change: (next: string) => setParams({ range: next }, { replace: true }) };
}

export function DataPage() {
  const { range, change } = useRange();
  const overview = useOverview(range);
  const trends = useTrends(range);
  return <><PageHeading title="数据" subtitle="所选窗口的浏览活动，与当前库存分别展示。" action={<SegmentedControl label="数据窗口" value={range ?? '7d'} options={windows} onChange={change} />} />
    <ErrorNotice error={overview.error} stale={!!overview.data} retry={() => void overview.refetch()} />
    {overview.isPending ? <Loading /> : overview.data && <>
      <div className="metric-grid metric-grid--four"><Metric label="浏览" value={String(overview.data.totals.views)} caption="窗口内浏览次数" /><Metric label="播放" value={String(overview.data.totals.plays)} caption="窗口内播放次数" /><Metric label="详情停留" value={duration(overview.data.totals.seconds * 1000)} caption="已记录可见时长" /><Metric label="浏览内容" value={String(overview.data.viewedAssets)} caption={`覆盖 ${overview.data.viewedAuthors} 位作者 · ${overview.data.viewedTags} 个标签`} /></div>
      <div className="data-panels"><section className="panel"><h2>窗口浏览构成</h2><div className="type-segments" role="img" aria-label={overview.data.mediaTypes.map(item => `${names[item.key] ?? item.key}浏览${item.views}次`).join('，')}>{overview.data.mediaTypes.filter(item => item.views > 0).map(item => <span key={item.key} className={`type-${item.key}`} style={{ flexGrow: item.views }} />)}</div><dl className="facts">{overview.data.mediaTypes.map(item => <div key={item.key}><dt>{names[item.key] ?? item.key}</dt><dd>浏览 {item.views} · 播放 {item.plays}</dd></div>)}</dl></section><section className="panel"><h2>浏览摘要</h2><dl className="facts"><div><dt>活跃天数</dt><dd>{overview.data.activeDays}</dd></div><div><dt>平均浏览</dt><dd>{overview.data.averageViewsPerFile === null ? '—' : overview.data.averageViewsPerFile.toLocaleString('zh-CN', { maximumFractionDigits: 2 })}</dd></div>{overview.data.sources.map(source => <div key={source.key}><dt>{names[source.key] ?? source.key}</dt><dd>浏览 {source.views} · 播放 {source.plays}</dd></div>)}</dl></section></div>
    </>}
    <section className="panel"><h2>浏览与播放趋势</h2><ErrorNotice error={trends.error} stale={!!trends.data} retry={() => void trends.refetch()} />{trends.isPending ? <Loading /> : trends.data && <TrendChart items={trends.data.items} />}</section>
    <section className="panel section-space"><div className="section-heading"><h2>浏览回顾</h2><Link className="button button--small" to={`/data/review?range=${range}`}>查看报告</Link></div><p className="muted">看看首次发现、再次打开、一天里的活跃时段，以及兴趣份额的变化。</p></section>
    {overview.data && <><div className="section-heading"><h2>当前媒体库</h2><span className="muted">当前库存，不按窗口筛选</span></div><div className="metric-grid"><Metric label="媒体文件" value={String(overview.data.inventory.assets)} caption={`${overview.data.inventory.images} 张图片（含动图） · ${overview.data.inventory.videos} 个视频`} /><Metric label="原件体积" value={bytes(overview.data.inventory.bytes)} caption="已入库内容" /><Metric label="收藏" value={String(overview.data.inventory.favorites)} caption={`${overview.data.inventory.authors} 位作者`} /></div></>}
  </>;
}

function Metric({ label, value, caption }: { label: string; value: string; caption: string }) { return <div className="metric panel"><span>{label}</span><strong>{value}</strong><small>{caption}</small></div>; }

export function ReviewPage() {
  const { range, change } = useRange();
  const report = useReview(range);
  const overview = useOverview(range);
  const positive = report.data?.changes.filter(item => item.changePoints > 0) ?? [];
  const negative = report.data?.changes.filter(item => item.changePoints < 0) ?? [];
  return <><Link className="back-link" to={`/data?range=${range}`}>返回数据</Link><PageHeading title="浏览回顾" subtitle="已记录的详情停留，不等同于视频播放时长。" action={<SegmentedControl label="回顾窗口" value={range ?? '7d'} options={windows} onChange={change} />} /><ErrorNotice error={report.error} stale={!!report.data} retry={() => void report.refetch()} />
    {report.isPending ? <Loading /> : report.data && <><section className="panel review-hours"><div className="section-heading"><div><span className="muted">{windows.find(item => item.value === range)?.label} · 浏览时长</span><p className="large-number">{overview.data ? duration(overview.data.totals.seconds * 1000) : '—'}</p></div></div><ErrorNotice error={overview.error} stale={!!overview.data} retry={() => void overview.refetch()} /><HourChart hours={report.data.hours} /></section><div className="metric-grid metric-grid--four section-space"><Metric label="首次发现" value={String(report.data.firstViews)} caption="所选窗口" /><Metric label="再次浏览" value={String(report.data.returnViews)} caption="所选窗口" /><Metric label="浏览内容" value={String(report.data.distinctAssets)} caption="不同媒体" /><Metric label="活跃天数" value={String(report.data.activeDays)} caption="有浏览记录的日期" /></div>
      {!report.data.comparisonAvailable && <div className="notice">当前窗口没有可比较的前一时段，或任一窗口没有浏览记录；不显示虚构的兴趣变化。</div>}
      {report.data.comparisonAvailable && <>{positive.length > 0 && <InterestGroup title="更多关注" items={positive} />}{negative.length > 0 && <InterestGroup title="较少关注" items={negative} />}{!positive.length && !negative.length && <p className="muted">与前一窗口相比，没有可展示的兴趣份额变化。</p>}</>}
    </>}
  </>;
}
function InterestGroup({ title, items }: { title: string; items: Schema<'InterestChange'>[] }) {
  return <section className="manage-group"><h2>{title}</h2><div className="interest-grid">{items.map(item => <article className="panel interest-card" key={`${item.kind}:${item.id}`}><Link to={item.kind === 'author' ? `/authors/${encodeURIComponent(item.id)}` : `/album?tag=${encodeURIComponent(item.name)}`}><h3>{item.name}</h3></Link><p className="muted">{item.kind === 'author' ? '作者' : '标签'} · 在浏览中的份额{item.changePoints > 0 ? '增加' : '减少'}</p><details><summary>详情 / 收起</summary><dl className="facts"><div><dt>上期</dt><dd>{item.previousShare.toFixed(1)}% · {item.previousViews} 次</dd></div><div><dt>本期</dt><dd>{item.currentShare.toFixed(1)}% · {item.currentViews} 次</dd></div><div><dt>变化</dt><dd>{item.changePoints > 0 ? '+' : ''}{item.changePoints.toFixed(1)} 个百分点</dd></div></dl></details></article>)}</div></section>;
}

export function MaintenancePage() {
  const { instance } = useSession();
  return <><PageHeading title="服务与维护" subtitle="当前连接与维护入口。" /><section className="panel"><h2>{instance.name}</h2><dl className="facts"><div><dt>协议</dt><dd>{instance.apiVersion}</dd></div><div><dt>智能执行器</dt><dd>{instance.capabilities.aiConfigured ? '已配置' : '未配置'}</dd></div></dl><p className="muted">已配置不代表执行器当前在线，逐批进度以任务报告为准。</p></section><div className="manage-grid section-space"><Link className="panel manage-card" to="/manage/upload"><h2>上传与入库</h2></Link><Link className="panel manage-card" to="/manage/tasks"><h2>任务与报告</h2></Link><Link className="panel manage-card" to="/manage/trash"><h2>回收站</h2></Link><Link className="panel manage-card" to="/manage/backup"><h2>备份与归档</h2></Link></div></>;
}
