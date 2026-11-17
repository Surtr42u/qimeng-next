import { useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { SegmentedControl } from '@ui/GlassNavigation';
import { useAssets, mediaCards, useTags } from '../data/media';
import type { AssetFilters } from '../data/media';
import { useLibraries } from '../data/manage';
import { useSession } from '../data/session';
import { EmptyState, ErrorNotice, Loading, PageHeading } from '../components/Feedback';
import { MediaGrid } from '../components/MediaGrid';
import { Icon } from '../components/Icon';

const sections = [{ value: 'recommend', label: '推荐' }, { value: 'cos', label: 'COS' }, { value: 'rank', label: '排行榜' }] as const;
const mediaTypes = [{ value: '', label: '综合' }, { value: 'video', label: '视频' }, { value: 'animated_image', label: '动图' }, { value: 'image', label: '图片' }] as const;
export function BrowsePage({ home = false }: { home?: boolean }) {
  const [params, setParams] = useSearchParams();
  const [advanced, setAdvanced] = useState(false);
  const tab = params.get('tab') ?? 'recommend';
  const q = params.get('q') ?? '';
  const { instance } = useSession();
  const libraries = useLibraries();
  const tags = useTags();
  const filters: AssetFilters = {
    q: q || undefined, kind: home && tab === 'cos' ? 'cos' : (params.get('kind') || undefined) as AssetFilters['kind'],
    mediaType: (params.get('mediaType') || undefined) as AssetFilters['mediaType'],
    libraryId: params.get('libraryId') || undefined, tag: params.get('tag') || undefined,
    authorId: params.get('authorId') || undefined, collectionId: params.get('collectionId') || undefined,
    sort: (params.get('sort') || 'added_desc') as AssetFilters['sort'],
  };
  const ranked = home && tab === 'rank';
  const query = useAssets(filters, !ranked);
  const items = query.data?.pages.flatMap(page => page.items) ?? [];
  const change = (key: string, value: string) => setParams(previous => { const next = new URLSearchParams(previous); if (value) next.set(key, value); else next.delete(key); return next; }, { replace: true });
  return <>
    {home ? <div className="browse-heading"><div><span className="eyebrow">在喜欢的世界里停留</span><h1 data-page-heading tabIndex={-1}>发现你的绮梦</h1></div><SegmentedControl value={tab} options={sections} label="首页内容" onChange={value => change('tab', value)} /></div>
      : <PageHeading title={q ? `搜索「${q}」` : '相册'} subtitle="保留每一份喜欢，按你熟悉的方式浏览。" />}
    {home && !q && ((ranked && !instance.capabilities.rankings) || (!ranked && !instance.capabilities.recommendations)) && <div className="notice" role="status"><strong>{ranked ? '排行榜正在接入' : '推荐正在接入'}</strong><p>{ranked ? '服务器尚未提供已确认口径的排行榜。' : '以下单独展示最近入库内容；个性化推荐需要服务器提供对应能力。'}</p></div>}
    {!ranked && <>
      <div className="browse-toolbar"><SegmentedControl value={filters.mediaType ?? ''} options={mediaTypes} label="媒体类型" onChange={value => change('mediaType', value)} />
        <div className="actions"><span className="muted">已加载 {items.length} 项</span><button className="button button--small" aria-expanded={advanced} onClick={() => setAdvanced(!advanced)}>筛选</button><button className="icon-button" aria-label="刷新列表" disabled={query.isFetching} onClick={() => void query.refetch()}><Icon name="refresh" /></button></div>
      </div>
      {advanced && <div className="filter-panel panel"><label>分区<select value={filters.kind ?? ''} onChange={event => change('kind', event.target.value)}><option value="">全部分区</option><option value="regular">常规</option><option value="cos">COS</option></select></label>
        <label>媒体库<select value={filters.libraryId ?? ''} disabled={!libraries.data} onChange={event => change('libraryId', event.target.value)}><option value="">全部媒体库</option>{libraries.data?.items.map(library => <option key={library.id} value={library.id}>{library.name}</option>)}</select></label>
        <label>标签<select value={filters.tag ?? ''} disabled={!tags.data} onChange={event => change('tag', event.target.value)}><option value="">全部标签</option>{tags.data?.items.map(tag => <option key={tag}>{tag}</option>)}</select></label>
        <label>排序<select value={filters.sort} onChange={event => change('sort', event.target.value)}><option value="added_desc">最近入库</option><option value="updated_desc">最近修改</option><option value="name_asc">文件名称</option></select></label>
        <button className="button" onClick={() => setParams(q ? { q } : {}, { replace: true })}>清空筛选</button><ErrorNotice error={libraries.error ?? tags.error} retry={() => { void libraries.refetch(); void tags.refetch(); }} />
      </div>}
      <ErrorNotice error={query.error} stale={items.length > 0} retry={() => void (query.isFetchNextPageError ? query.fetchNextPage() : query.refetch())} />
      {query.isPending ? <Loading text={q ? '正在搜索…' : '正在读取媒体…'} /> : items.length ? <MediaGrid items={mediaCards(items)} search={!!q} /> : !query.isError && <EmptyState title={q ? '没有匹配的内容' : '这里还没有媒体'} action={!q && <Link to="/manage/upload" className="button button--primary">上传文件</Link>}>{q ? '试试其他关键词或清空筛选。' : '上传图片或视频，让喜欢的内容留在自己的影库。'}</EmptyState>}
      {query.hasNextPage && <div className="load-more"><button className="button" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>{query.isFetchingNextPage ? '正在加载…' : '加载更多'}</button></div>}
    </>}
  </>;
}
