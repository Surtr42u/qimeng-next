import { useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuthors, useDeleteAuthor, useMergeAuthors } from '../data/manage';
import type { Schema } from '../data/client';
import { ErrorNotice } from '../components/Feedback';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { newId } from '../lib/id';

export function AuthorOperations({ author, disabled }: { author: Schema<'Author'>; disabled: boolean }) {
  const navigate = useNavigate(); const remove = useDeleteAuthor(); const merge = useMergeAuthors();
  const [open, setOpen] = useState(false); const [q, setQ] = useState(''); const [targetId, setTargetId] = useState('');
  const candidates = useAuthors(q); const [confirm, setConfirm] = useState<'delete' | 'merge'>();
  const operation = useRef(newId());
  const target = candidates.data?.pages.flatMap(page => page.items).find(item => item.id === targetId);
  if (author.placeholder || author.kind === 'cos') return null;
  return <section className="section-space"><button className="button button--small" aria-expanded={open} disabled={disabled} onClick={() => setOpen(!open)}>作者管理操作</button>{open && <div className="panel form-stack section-space"><h2>合并与删除</h2><p className="muted">普通作者的操作保留媒体原件。合并只作用于明确选定的两个身份。</p><label>查找合并目标<input value={q} onChange={event => setQ(event.target.value)} /></label><label>目标作者<select value={targetId} onChange={event => setTargetId(event.target.value)}><option value="">选择要保留的作者身份</option>{candidates.data?.pages.flatMap(page => page.items).filter(item => item.id !== author.id && !item.placeholder && item.kind !== 'cos').map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label><ErrorNotice error={candidates.error} retry={() => void candidates.refetch()} />{candidates.hasNextPage && <button className="button" disabled={candidates.isFetchingNextPage} onClick={() => void candidates.fetchNextPage()}>更多作者</button>}<div className="actions"><button className="button" disabled={!target || disabled} onClick={() => { operation.current = newId(); merge.reset(); setConfirm('merge'); }}>合并到所选作者</button><button className="button button--danger" disabled={disabled} onClick={() => { operation.current = newId(); remove.reset(); setConfirm('delete'); }}>删除作者身份</button></div></div>}
    {confirm === 'delete' && <ConfirmDialog title="删除作者身份？" danger busy={remove.isPending} onCancel={() => setConfirm(undefined)} onConfirm={() => remove.mutate({ id: author.id, revision: author.revision, key: operation.current }, { onSuccess: () => navigate('/authors', { replace: true }) })}><p>{author.name}</p><p>移除这个作者身份与相应关联。媒体原件与其他作者关系保留；没有其他作者的媒体回到未确认占位。</p><ErrorNotice error={remove.error} /></ConfirmDialog>}
    {confirm === 'merge' && target && <ConfirmDialog title="合并这两个作者身份？" busy={merge.isPending} onCancel={() => setConfirm(undefined)} onConfirm={() => merge.mutate({ body: { sourceId: author.id, targetId: target.id, expectedSourceRevision: author.revision, expectedTargetRevision: target.revision }, key: operation.current }, { onSuccess: result => navigate(`/authors/${encodeURIComponent(result.author.id)}`, { replace: true, state: { mergeSummary: { affected: result.affectedAssets, conflicts: result.preservedConflicts.length } } }) })}><p>将“{author.name}”合并到“{target.name}”。</p><p>目标人工档案保留；来源档案中的冲突信息归档，不静默覆盖。媒体和历史保留。</p><ErrorNotice error={merge.error} /></ConfirmDialog>}
  </section>;
}
