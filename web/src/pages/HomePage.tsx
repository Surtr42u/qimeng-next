import { Link, useSearchParams } from 'react-router-dom';
import { SegmentedControl } from '@ui/GlassNavigation';
import { useRecommendations, useRankings } from '../data/analytics';
import type { RankingPeriod } from '../data/analytics';
import { mediaCards } from '../data/media';
import type { Asset } from '../data/media';
import { useSession } from '../data/session';
import { EmptyState, ErrorNotice, Loading } from '../components/Feedback';
import { MediaGrid } from '../components/MediaGrid';
import { Icon } from '../components/Icon';

export function HomePage() {
  const [params, setParams] = useSearchParams();
  const { instance } = useSession();
  const tab = params.get('tab') ?? 'recommend';
  const period = (params.get('period') ?? 'week') as RankingPeriod;
  const type = (params.get('mediaType') || undefined) as Asset['mediaType'] | undefined;
  const seed = Number(params.get('seed') ?? 0);
  const ranked = tab === 'rank';
  const recommendations = useRecommendations(seed, tab === 'cos' ? 'cos' : undefined, type, !ranked && instance.capabilities.recommendations);
  const rankings = useRankings(period, ranked && instance.capabilities.rankings);
  const query = ranked ? rankings : recommendations;
  const assets = ranked ? rankings.data?.pages.flatMap(page => page.items.map(item => item.asset)) ?? [] : recommendations.data?.pages.flatMap(page => page.items) ?? [];
  const unavailable = ranked ? !instance.capabilities.rankings : !instance.capabilities.recommendations;
  const change = (key: string, value: string) => setParams(previous => { const next = new URLSearchParams(previous); if (value) next.set(key, value); else next.delete(key); return next; }, { replace: true });
  return <><div className="browse-heading"><div><span className="eyebrow">在喜欢的世界里停留</span><h1 data-page-heading tabIndex={-1}>发现你的绮梦</h1></div><SegmentedControl label="首页内容" value={tab} options={[{ value: 'recommend', label: '推荐' }, { value: 'cos', label: 'COS' }, { value: 'rank', label: '排行榜' }]} onChange={value => change('tab', value)} /></div>
    <div className="browse-toolbar">{ranked ? <SegmentedControl label="排行周期" value={period ?? 'week'} options={[{ value: 'day', label: '日' }, { value: 'week', label: '周' }, { value: 'month', label: '月' }, { value: 'quarter', label: '季' }, { value: 'year', label: '年' }, { value: 'all', label: '全部' }]} onChange={value => change('period', value)} />
      : <SegmentedControl label="推荐媒体类型" value={type ?? ''} options={[{ value: '', label: '综合' }, { value: 'video', label: '视频' }, { value: 'animated_image', label: '动图' }, { value: 'image', label: '图片' }]} onChange={value => change('mediaType', value)} />}
      <div className="actions"><span className="muted">已加载 {assets.length} 项</span><button className="button button--small" disabled={query.isFetching || unavailable} onClick={() => ranked ? void rankings.refetch() : change('seed', String(crypto.getRandomValues(new Uint32Array(1))[0]))}><Icon name="refresh" size={15} />{query.isFetching && !query.isFetchingNextPage ? '更新中…' : ranked ? '刷新榜单' : '换一批'}</button><Link className="button button--small" to={tab === 'cos' ? '/album?kind=cos' : '/album'}>高级筛选</Link></div>
    </div>
    {unavailable ? <EmptyState title={ranked ? '当前服务器尚未开放排行榜' : '当前服务器尚未开放推荐'} action={<Link className="button" to="/album">浏览相册</Link>}>更新服务器后可读取对应结果，不以普通排序替代。</EmptyState> : <><ErrorNotice error={query.error} stale={assets.length > 0} retry={() => void (query.isFetchNextPageError ? query.fetchNextPage() : query.refetch())} />{query.isPending ? <Loading text={ranked ? '正在读取榜单…' : '正在准备推荐…'} /> : assets.length ? <MediaGrid items={mediaCards(assets)} /> : !query.isError && <EmptyState title={ranked ? '这个周期还没有排行内容' : '暂无可推荐的内容'} action={<Link to="/manage/upload" className="button button--primary">上传媒体</Link>} />}{query.hasNextPage && <div className="load-more"><button className="button" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>加载更多</button></div>}</>}
  </>;
}
