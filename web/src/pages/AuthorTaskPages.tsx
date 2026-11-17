import { useState } from 'react';
import { Link, NavLink, useParams, useSearchParams } from 'react-router-dom';
import { useAuthorTasks, useAuthorTask, useCancelAuthorTask } from '../data/maintenance';
import type { Schema } from '../data/client';
import { EmptyState, ErrorNotice, Loading, PageHeading, StatusBadge } from '../components/Feedback';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { dateTime } from '../lib/format';

const states: Record<Schema<'AuthorTask'>['state'], string> = { authorized: '已授权，等待处理', completed: '已完成授权步骤', cancelled: '已取消', expired: '已过期' };
const fields: Record<string, string> = { name: '名称', biography: '简介', homepage: '主页', aliases: '别名', x: 'X资料候选', basic: '基础资料候选', accounts: '账号来源候选' };
export function TaskNavigation() {
  return <nav className="actions section-space" aria-label="任务类型"><NavLink className="button button--small" to="/manage/tasks" end>入库与整理</NavLink><NavLink className="button button--small" to="/manage/author-tasks" end>作者资料维护</NavLink></nav>;
}
export function AuthorTasksPage() {
  const [params, setParams] = useSearchParams(); const state = (params.get('state') || undefined) as Schema<'AuthorTask'>['state'] | undefined;
  const query = useAuthorTasks(state); const items = query.data?.pages.flatMap(page => page.items) ?? [];
  return <><PageHeading title="任务与报告" subtitle="作者资料维护的授权、进展与当前事实。" /><TaskNavigation /><label className="compact-field section-space">任务状态<select value={state ?? ''} onChange={event => setParams({ state: event.target.value }, { replace: true })}><option value="">全部状态</option>{Object.entries(states).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label><ErrorNotice error={query.error} stale={items.length > 0} retry={() => void query.refetch()} />{query.isPending ? <Loading /> : items.length ? <div className="list-panel">{items.map(task => <Link className="list-row" key={task.id} to={`/manage/author-tasks/${encodeURIComponent(task.id)}`}><div className="row-main"><strong>作者资料维护</strong><span>{[...task.allowedFields, ...(task.candidateBlocks ?? [])].map(field => fields[field] ?? field).join(' · ')}</span><span>{dateTime(task.createdAt)}</span></div><StatusBadge tone={task.state === 'completed' ? 'success' : 'neutral'}>{states[task.state]}</StatusBadge></Link>)}</div> : !query.isError && <EmptyState title="没有此状态的作者维护任务" />}{query.hasNextPage && <button className="button" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>加载更多任务</button>}</>;
}
export function AuthorTaskPage() {
  const { id = '' } = useParams(); const query = useAuthorTask(id); const cancel = useCancelAuthorTask(id); const [confirm, setConfirm] = useState(false);
  const data = query.data;
  return <><Link className="back-link" to="/manage/author-tasks">返回作者维护任务</Link><PageHeading title={data ? `${data.author.name} · 资料维护` : '作者资料维护'} /><ErrorNotice error={query.error} stale={!!data} retry={() => void query.refetch()} />{query.isPending ? <Loading /> : data && <section className="panel form-stack"><StatusBadge>{states[data.task.state]}</StatusBadge><dl className="facts"><div><dt>已授权字段</dt><dd>{data.task.allowedFields.map(field => fields[field] ?? field).join(' · ') || '没有直接修改授权'}</dd></div><div><dt>候选范围</dt><dd>{data.task.candidateBlocks?.map(block => fields[block]).join(' · ') || '没有候选提交授权'}</dd></div><div><dt>操作次数</dt><dd>已使用 {data.task.usedUpdates} / 授权 {data.task.maxUpdates}</dd></div><div><dt>清空资料</dt><dd>{data.task.allowClear ? '允许' : '不允许'}</dd></div><div><dt>授权到期</dt><dd>{dateTime(data.task.expiresAt)}</dd></div></dl><p className="muted">完成授权步骤不代表候选已被确认。已确认资料以作者档案的当前修订为准。</p><div className="actions"><Link className="button" to={`/authors/${encodeURIComponent(data.author.id)}`}>查看作者档案</Link><button className="button" onClick={() => void query.refetch()}>刷新状态</button>{data.task.state === 'authorized' && <button className="button button--danger" onClick={() => setConfirm(true)}>撤销本次授权</button>}</div></section>}{confirm && <ConfirmDialog title="撤销本次作者维护授权？" busy={cancel.isPending} onCancel={() => setConfirm(false)} onConfirm={() => cancel.mutate(undefined, { onSuccess: () => setConfirm(false) })}><p>阻止尚未提交的后续操作；已经保存的资料与候选记录保留。</p><ErrorNotice error={cancel.error} /></ConfirmDialog>}</>;
}
