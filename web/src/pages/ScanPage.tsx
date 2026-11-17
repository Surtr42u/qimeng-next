import { useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useScan, useCancelScan } from '../data/maintenance';
import { ErrorNotice, Loading, PageHeading, StatusBadge } from '../components/Feedback';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { dateTime } from '../lib/format';
import { newId } from '../lib/id';

const labels = { queued: '等待扫描', running: '正在扫描', completed: '扫描完成', partial: '部分完成', failed: '扫描失败', interrupted: '扫描中断', cancelled: '已取消' };
export function ScanPage() {
  const { id = '' } = useParams(); const query = useScan(id); const cancel = useCancelScan(id);
  const [confirm, setConfirm] = useState(false); const key = useRef(newId());
  const job = query.data;
  return (
    <>
      <Link className="back-link" to="/manage/libraries">返回媒体库</Link>
      <PageHeading title="扫描报告" subtitle="只登记实际读取并核验的文件；目录缺失不等于删除媒体。" />
      <ErrorNotice error={query.error} stale={!!job} retry={() => void query.refetch()} />
      {query.isPending ? <Loading /> : job && <>
        <StatusBadge tone={job.state === 'completed' ? 'success' : ['partial', 'failed', 'interrupted'].includes(job.state) ? 'warning' : 'neutral'}>{labels[job.state]}</StatusBadge>
        <div className="metric-grid section-space"><div className="metric panel"><span>已处理</span><strong>{job.processed}</strong></div><div className="metric panel"><span>新入库</span><strong>{job.added}</strong></div><div className="metric panel"><span>问题记录</span><strong>{job.issueCount}</strong></div></div>
        <section className="panel"><dl className="facts"><div><dt>位置更新</dt><dd>{job.moved}</dd></div><div><dt>内容变化</dt><dd>{job.changed}</dd></div><div><dt>已跳过</dt><dd>{job.skipped}</dd></div><div><dt>开始 / 更新</dt><dd>{dateTime(job.createdAt)} / {dateTime(job.updatedAt)}</dd></div></dl><Link className="button" to={`/album?libraryId=${encodeURIComponent(job.libraryId)}`}>浏览本库内容</Link></section>
        {job.issues.length > 0 && <section className="panel"><h2>需要核对的文件</h2><p className="muted">显示 {job.issues.length} / {job.issueCount} 条，最多100条明细；问题总数不被截断。</p>{job.issues.map((issue, index) => <div className="list-row" key={`${issue.relativePath}:${index}`}><div className="row-main"><strong>{issue.relativePath}</strong><span>{issue.code}</span></div></div>)}</section>}
        <div className="actions section-space"><button className="button" onClick={() => void query.refetch()}>刷新报告</button>{['queued', 'running'].includes(job.state) && <button className="button button--danger" onClick={() => setConfirm(true)}>取消后续扫描</button>}</div>
      </>}
      {confirm && <ConfirmDialog title="取消尚未处理的扫描？" busy={cancel.isPending} onCancel={() => setConfirm(false)} onConfirm={() => cancel.mutate(key.current, { onSuccess: () => setConfirm(false) })}><p>已经登记的媒体和记录保留，不撤回已完成结果。</p><ErrorNotice error={cancel.error} /></ConfirmDialog>}
    </>
  );
}
