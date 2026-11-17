import { useRef, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { useScan, useCancelScan, useScans } from '../data/maintenance';
import type { Schema } from '../data/client';
import { useLibraries } from '../data/manage';
import { EmptyState, ErrorNotice, Loading, PageHeading, StatusBadge } from '../components/Feedback';
import { TaskNavigation } from './AuthorTaskPages';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { dateTime } from '../lib/format';
import { newId } from '../lib/id';

const labels = { queued: '等待扫描', running: '正在扫描', completed: '扫描完成', partial: '部分完成', failed: '扫描失败', interrupted: '扫描中断', cancelled: '已取消' };
export function ScansPage() {
  const [params, setParams] = useSearchParams();
  const libraryId = params.get('libraryId') || undefined;
  const state = params.get('state') as Schema<'ScanJob'>['state'] | null;
  const query = useScans(libraryId, state || undefined);
  const libraries = useLibraries();
  const items = query.data?.pages.flatMap(page => page.items) ?? [];
  const filter = (name: string, value: string) => setParams(current => { const next = new URLSearchParams(current); value ? next.set(name, value) : next.delete(name); return next; }, { replace: true });
  return <>
    <PageHeading title="任务与报告" subtitle="服务器保存的扫描记录，刷新或更换设备后仍可回读。" /><TaskNavigation />
    <div className="actions section-space"><label>扫描媒体库<select value={libraryId ?? ''} onChange={event => filter('libraryId', event.target.value)}><option value="">所有媒体库</option>{libraries.data?.items.map(library => <option value={library.id} key={library.id}>{library.name}</option>)}</select></label><label>扫描状态<select value={state ?? ''} onChange={event => filter('state', event.target.value)}><option value="">全部状态</option>{Object.entries(labels).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select></label><button className="button" onClick={() => void query.refetch()}>刷新记录</button></div>
    <ErrorNotice error={libraries.error} retry={() => void libraries.refetch()} /><ErrorNotice error={query.error} stale={items.length > 0} retry={() => void query.refetch()} />
    {query.isPending ? <Loading /> : items.length ? <div className="list-panel">{items.map(item => <Link className="list-row" to={`/manage/scans/${encodeURIComponent(item.id)}`} key={item.id}><div className="row-main"><strong>{libraries.data?.items.find(library => library.id === item.libraryId)?.name ?? '媒体库扫描'}</strong><span>{dateTime(item.createdAt)} · 登记 {item.added} · 缺失 {item.missing} · 问题 {item.issueCount}</span></div><StatusBadge tone={item.state === 'completed' ? 'success' : item.state === 'partial' ? 'warning' : 'neutral'}>{labels[item.state]}</StatusBadge></Link>)}</div> : !query.isError && <EmptyState title="没有符合条件的扫描记录" action={<Link className="button" to="/manage/libraries">管理媒体库并开始扫描</Link>} />}
    {query.hasNextPage && <button className="button" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>加载更多扫描记录</button>}
  </>;
}
export function ScanPage() {
  const { id = '' } = useParams(); const query = useScan(id); const cancel = useCancelScan(id);
  const [confirm, setConfirm] = useState(false); const key = useRef(newId());
  const job = query.data;
  return (
    <>
      <Link className="back-link" to="/manage/libraries">返回媒体库</Link>
      <Link className="text-link" to="/manage/scans">全部扫描记录</Link>
      <PageHeading title="扫描报告" subtitle="只登记实际读取并核验的文件；目录缺失不等于删除媒体。" />
      <ErrorNotice error={query.error} stale={!!job} retry={() => void query.refetch()} />
      {query.isPending ? <Loading /> : job && <>
        <StatusBadge tone={job.state === 'completed' ? 'success' : ['partial', 'failed', 'interrupted'].includes(job.state) ? 'warning' : 'neutral'}>{labels[job.state]}</StatusBadge>
        <div className="metric-grid section-space"><div className="metric panel"><span>已处理</span><strong>{job.processed}</strong></div><div className="metric panel"><span>新入库</span><strong>{job.added}</strong></div><div className="metric panel"><span>问题记录</span><strong>{job.issueCount}</strong></div></div>
        <section className="panel"><dl className="facts"><div><dt>位置更新</dt><dd>{job.moved}</dd></div><div><dt>内容变化</dt><dd>{job.changed}</dd></div><div><dt>原件缺失</dt><dd>{job.missing}（保留资产与历史记录）</dd></div><div><dt>已跳过</dt><dd>{job.skipped}</dd></div><div><dt>开始 / 更新</dt><dd>{dateTime(job.createdAt)} / {dateTime(job.updatedAt)}</dd></div></dl><Link className="button" to={`/album?libraryId=${encodeURIComponent(job.libraryId)}`}>浏览本库内容</Link></section>
        {job.issues.length > 0 && <section className="panel"><h2>需要核对的文件</h2><p className="muted">显示 {job.issues.length} / {job.issueCount} 条，最多100条明细；问题总数不被截断。</p>{job.issues.map((issue, index) => <div className="list-row" key={`${issue.relativePath}:${index}`}><div className="row-main"><strong>{issue.relativePath}</strong><span>{issue.code}</span></div></div>)}</section>}
        <div className="actions section-space"><button className="button" onClick={() => void query.refetch()}>刷新报告</button>{['queued', 'running'].includes(job.state) && <button className="button button--danger" onClick={() => setConfirm(true)}>取消后续扫描</button>}</div>
      </>}
      {confirm && <ConfirmDialog title="取消尚未处理的扫描？" busy={cancel.isPending} onCancel={() => setConfirm(false)} onConfirm={() => cancel.mutate(key.current, { onSuccess: () => setConfirm(false) })}><p>已经登记的媒体和记录保留，不撤回已完成结果。</p><ErrorNotice error={cancel.error} /></ConfirmDialog>}
    </>
  );
}
