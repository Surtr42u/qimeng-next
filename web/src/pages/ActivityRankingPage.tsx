import { Link, useSearchParams } from 'react-router-dom';
import { SegmentedControl } from '@ui/GlassNavigation';
import { useActivityRanking } from '../data/analytics';
import type { ActivityRankingKind, StatsRange } from '../data/analytics';
import type { Schema } from '../data/client';
import { ErrorNotice, Loading, EmptyState, PageHeading } from '../components/Feedback';
import { activityWindows, useActivityRange } from '../lib/activity-window';
import { readableDuration } from '../lib/format';

const kinds = [{ value: 'authors', label: '作者' }, { value: 'tags', label: '标签' }, { value: 'assets', label: '内容' }] as const;
const titles = { authors: '常看作者', tags: '常看标签', assets: '常看内容' };

export function ActivityTopCards({ range }: { range: StatsRange }) {
  return <div className="data-panels section-space">{kinds.map(kind => <ActivityTopCard key={kind.value} kind={kind.value} range={range} />)}</div>;
}

function ActivityTopCard({ kind, range }: { kind: ActivityRankingKind; range: StatsRange }) {
  const query = useActivityRanking(kind, range, 'views', 5);
  const items = query.data?.pages[0]?.items ?? [];
  return <section className="panel">
    <div className="section-heading"><h2>{titles[kind]}</h2><Link className="text-link" to={`/data/rankings?kind=${kind}&range=${range}`}>完整榜单</Link></div>
    <ErrorNotice error={query.error} stale={items.length > 0} retry={() => void query.refetch()} />
    {query.isPending ? <Loading /> : items.length ? <RankingRows kind={kind} items={items} /> : !query.isError && <p className="muted">此窗口没有相应活动记录。</p>}
  </section>;
}

export function ActivityRankingPage() {
  const [params, setParams] = useSearchParams();
  const { range, change } = useActivityRange();
  const kind = (kinds.some(item => item.value === params.get('kind')) ? params.get('kind') : 'authors') as ActivityRankingKind;
  const metric = params.get('metric') === 'seconds' && kind === 'assets' ? 'seconds' : 'views';
  const query = useActivityRanking(kind, range, metric);
  const items = query.data?.pages.flatMap(page => page.items) ?? [];
  const select = (key: string, value: string) => setParams(current => { const next = new URLSearchParams(current); next.set(key, value); return next; }, { replace: true });
  return <>
    <Link className="back-link" to={`/data?range=${range}`}>返回数据总览</Link>
    <PageHeading title="浏览活动榜单" subtitle="按服务端记录的窗口活动排序，与每日点赞榜分别统计。" action={<SegmentedControl label="榜单窗口" value={range ?? '7d'} options={activityWindows} onChange={change} />} />
    <div className="actions section-space"><SegmentedControl label="榜单对象" value={kind} options={kinds} onChange={value => select('kind', value)} />{kind === 'assets' && <SegmentedControl label="内容榜排序" value={metric} options={[{ value: 'views', label: '浏览次数' }, { value: 'seconds', label: '详情停留' }]} onChange={value => select('metric', value)} />}</div>
    <ErrorNotice error={query.error} stale={items.length > 0} retry={() => void query.refetch()} />
    {query.isPending ? <Loading /> : items.length ? <section className="panel"><RankingRows kind={kind} items={items} metric={metric} /></section> : !query.isError && <EmptyState title="此窗口没有活动排名" />}
    {query.hasNextPage && <button className="button section-space" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>加载更多排名</button>}
  </>;
}

function RankingRows({ kind, items, metric = 'views' }: { kind: ActivityRankingKind; items: Schema<'ActivityRankingItem'>[]; metric?: 'views' | 'seconds' }) {
  return <ol className="activity-ranking-list">{items.map(item => {
    const target = kind === 'authors' ? `/authors/${encodeURIComponent(item.id)}` : kind === 'tags' ? `/album?tag=${encodeURIComponent(item.name)}` : item.asset ? `/asset/${encodeURIComponent(item.asset.id)}` : undefined;
    return <li key={item.id}><div>{target ? <Link className="text-link" to={target}>{item.name}</Link> : <span>{item.name}</span>}<p className="muted">{item.assets} 份内容 · 播放 {item.plays} 次 · 停留 {readableDuration(item.seconds)}</p></div><strong>{metric === 'seconds' ? readableDuration(item.value) : `${item.views} 次`}</strong></li>;
  })}</ol>;
}
