import { useId, useState } from 'react';
import type { ReactNode } from 'react';
import type { AssetFilters } from '../data/media';
import { useAssetFacets } from '../data/browse';
import type { FacetRow } from '../data/browse';
import { ErrorNotice } from './Feedback';
import { ChevronDownIcon } from './LegacyShellIcons';

type FacetResult = ReturnType<typeof useAssetFacets>;
export type FacetDimension = { row: FacetRow; label: string; query: FacetResult };
export function LegacyFacetFilter({ filters, dimensions, onChange, panelOpen, setPanelOpen, active, children }: {
  filters: AssetFilters; dimensions: FacetDimension[]; onChange: (patch: Partial<AssetFilters>) => void;
  panelOpen?: boolean; setPanelOpen?: (open: boolean) => void; active?: boolean; children?: ReactNode;
}) {
  const id = useId();
  const [selectedRow, setSelectedRow] = useState(dimensions[0]?.row ?? 'partitions');
  const [open, setOpen] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const current = dimensions.find(item => item.row === selectedRow) ?? dimensions[0];
  if (!current) return null;
  const page = current.query.data?.pages[0];
  const items = current.query.data?.pages.flatMap(value => value.items) ?? [];
  const fixed = current.row === 'partitions' || current.row === 'types';
  const choices = fixed ? items : [{ key: '', kind: current.row === 'works' ? 'source' : 'character', id: '', name: '全部', count: page?.totalMatched } as const, ...items];
  const isActive = (item: { key: string; id: string }) => {
    if (current.row === 'partitions') return (filters.kind ?? 'all') === item.id;
    if (current.row === 'types') return (filters.mediaType ?? 'all') === item.id;
    const keys = current.row === 'works' ? filters.workKeys : filters.roleKeys;
    return item.key ? !!keys?.includes(item.key) : !keys?.length;
  };
  const pick = (item: { key: string; id: string }) => {
    if (current.row === 'partitions') onChange({ kind: item.id === 'all' ? undefined : item.id as AssetFilters['kind'], workKeys: undefined, roleKeys: undefined });
    else if (current.row === 'types') onChange({ mediaType: item.id === 'all' ? undefined : item.id as AssetFilters['mediaType'] });
    else if (current.row === 'works') onChange({ workKeys: item.key ? [item.key] : undefined });
    else onChange({ roleKeys: item.key ? [item.key] : undefined });
  };
  return <section className="filter-card" aria-label="文件筛选">
    <div className="pill-row" role="group" aria-label="筛选维度">
      {dimensions.map((dimension, index) => {
        const total = dimension.query.data?.pages[0]?.totalOptions;
        const count = total === undefined ? undefined : total - (dimension.row === 'partitions' || dimension.row === 'types' ? 1 : 0);
        return <span key={dimension.row} className="pill-row-item">{index > 0 && <span className="pill-divider" aria-hidden="true" />}<button className={`pill${selectedRow === dimension.row ? ' active' : ''}`} type="button" aria-pressed={selectedRow === dimension.row} aria-expanded={selectedRow === dimension.row && open} aria-controls={id} onClick={() => { if (selectedRow !== dimension.row) { setSelectedRow(dimension.row); setOpen(true); setExpanded(false); } else setOpen(!open); }}>{dimension.label} <span className="pill-count">{count}</span></button></span>;
      })}
      {setPanelOpen && <button type="button" className={`more-filter${panelOpen ? ' open' : ''}${active ? ' active' : ''}`} aria-expanded={panelOpen} onClick={() => setPanelOpen(!panelOpen)}>更多筛选<ChevronDownIcon /></button>}
    </div>
    <div id={id} role="group" aria-label={`${current.label}选项`} className={`pill-row value-row${expanded ? ' expanded' : ''}${open ? '' : ' collapsed'}`}>
      {current.query.isPending ? <span role="status">正在读取…</span> : choices.map(item => <button key={item.key} type="button" className={`pill${isActive(item) ? ' active' : ''}`} aria-pressed={isActive(item)} onClick={() => pick(item)}>{item.name}{item.count !== undefined && <span className="pill-count">{item.count.toLocaleString('zh-CN')}</span>}</button>)}
      {expanded && current.query.hasNextPage && <button type="button" className="pill" disabled={current.query.isFetchingNextPage} onClick={() => void current.query.fetchNextPage()}>更多选项</button>}
    </div>
    {open && <ErrorNotice error={current.query.error} stale={!!page} retry={() => void (current.query.isFetchNextPageError ? current.query.fetchNextPage() : current.query.refetch())} />}
    {open && (choices.length > 9 || current.query.hasNextPage) && <div className="expand-row"><button className="expand-btn" type="button" onClick={() => setExpanded(!expanded)}>{expanded ? '收起 ⌃' : '展开 ⌄'}</button></div>}
    {children}
  </section>;
}
