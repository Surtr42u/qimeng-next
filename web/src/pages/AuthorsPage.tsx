import { useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useAuthors, useDeleteAuthor, useUpdateAuthor } from '../data/manage';
import { useAuthorProfileSummary } from '../data/author-profiles';
import type { Schema } from '../data/client';
import { ErrorNotice, Loading } from '../components/Feedback';
import { ManagePageHeader } from '../components/LegacyManageHeader';
import { SearchIcon } from '../components/LegacyShellIcons';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { AuthorAvatar } from './ProfilePresentation';
import { AuthorEditDialog } from './AuthorEditDialog';
import { useNearViewport } from '../lib/visibility';
import { newId } from '../lib/id';

const zones = [{ value: '', label: '全部' }, { value: 'regular', label: '常规' }, { value: 'cos', label: 'COS' }] as const;
const sorts = [{ value: 'default', label: '默认' }, { value: 'views', label: '经常浏览' }, { value: 'files', label: '文件数量' }, { value: 'addedAsc', label: '加入较早' }, { value: 'addedDesc', label: '加入较晚' }] as const;
export function AuthorsPage() {
  const [params, setParams] = useSearchParams();
  const [editor, setEditor] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<{ author: Schema<'Author'>; key: string }>();
  const [notice, setNotice] = useState('');
  const kind = zones.find(item => item.value === params.get('kind'))?.value || undefined;
  const sort = sorts.find(item => item.value === params.get('sort'))?.value ?? 'default';
  const q = params.get('q') ?? '';
  const query = useAuthors(q, undefined, true, { kind, sort });
  const all = useAuthors();
  const authors = query.data?.pages.flatMap(page => page.items) ?? [];
  const remove = useDeleteAuthor();
  const change = (key: string, value: string) => setParams(current => { const next = new URLSearchParams(current); value ? next.set(key, value) : next.delete(key); return next; }, { replace: true });
  return <div className="page" id="page-authors">
    <ManagePageHeader title="作者管理" description={`全部作者 · ${all.data?.pages[0]?.totalMatched ?? '—'} 位`} />
    <p className="a-guide">管理作者名称、别名与来源记录；COS 作者随媒体目录自动维护。</p>
    <div className="a-toolbar"><div className="a-zones">{zones.map(item => <button key={item.value} type="button" className={`stype${(kind ?? '') === item.value ? ' active' : ''}`} data-zone={item.value} aria-pressed={(kind ?? '') === item.value} onClick={() => change('kind', item.value)}>{item.label}</button>)}</div><div className="hist-search"><input type="text" placeholder="搜索作者" aria-label="搜索作者" value={q} onChange={event => change('q', event.target.value)} /><SearchIcon /></div></div>
    <div className="a-sortrow">{sorts.map(item => <button key={item.value} type="button" className={`sort-pill${sort === item.value ? ' active' : ''}`} data-sort={item.value} aria-pressed={sort === item.value} onClick={() => change('sort', item.value)}>{item.label}</button>)}{kind !== 'cos' && <button type="button" className="pill" style={{ marginLeft: 'auto' }} onClick={() => setEditor('')}>新建作者</button>}</div>
    {notice && <p role="status" className="manage-notice">{notice}</p>}
    <div className="a-list"><ErrorNotice error={query.error} stale={authors.length > 0} retry={() => void query.refetch()} />{query.isPending ? <Loading /> : authors.length ? <ul>{authors.map(author => <AuthorRow key={author.id} author={author} onEdit={() => setEditor(author.id)} onDelete={() => { remove.reset(); setDeleteTarget({ author, key: newId() }); }} />)}</ul> : !query.isError && <p className="a-empty">没有匹配的作者</p>}</div>
    {query.hasNextPage && <div className="workspace-load-more"><button className="pill" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>加载更多作者</button></div>}
    {editor !== null && <AuthorEditDialog id={editor || undefined} onDismiss={() => setEditor(null)} onSaved={setNotice} />}
    {deleteTarget && <ConfirmDialog title={`删除作者「${deleteTarget.author.name}」？`} danger confirmLabel="删除" busy={remove.isPending} onCancel={() => setDeleteTarget(undefined)} onConfirm={() => remove.mutate({ id: deleteTarget.author.id, revision: deleteTarget.author.revision, key: deleteTarget.key }, { onSuccess: () => { setDeleteTarget(undefined); setNotice('已删除作者，关联文件已保留'); } })}><p>关联的 {deleteTarget.author.fileCount ?? '—'} 个文件会保留、仅解除关联；作者总表里的对应条目一并移除。此操作不可撤销。</p><ErrorNotice error={remove.error} /></ConfirmDialog>}
  </div>;
}
export function AuthorsList({ following, queryText = '' }: { following?: boolean; queryText?: string }) {
  const query = useAuthors(queryText, following);
  const items = query.data?.pages.flatMap(page => page.items) ?? [];
  return <div className="a-list"><ErrorNotice error={query.error} stale={items.length > 0} retry={() => void query.refetch()} />{query.isPending ? <Loading /> : items.length ? <ul>{items.map(author => <AuthorRow key={author.id} author={author} />)}</ul> : !query.isError && <p className="a-empty">{following ? '还没有关注的作者' : '没有匹配的作者'}</p>}{query.hasNextPage && <button className="pill" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>加载更多作者</button>}</div>;
}
function AuthorRow({ author, onEdit, onDelete }: { author: Schema<'Author'>; onEdit?: () => void; onDelete?: () => void }) {
  const follow = useUpdateAuthor(author.id);
  const { ref, visible } = useNearViewport<HTMLLIElement>();
  const profile = useAuthorProfileSummary(author.id, visible);
  return <li ref={ref}>
    <Link className="author-identity" to={`/authors/${encodeURIComponent(author.id)}`}><AuthorAvatar name={author.name} avatar={profile.data?.avatar} className="author-avatar" /><span className="author-info"><strong>{[author.name, ...(author.aliases ?? [])].join(' / ')}</strong><small>{author.kind === 'cos' && <span className="author-type">COS</span>}<span>{author.fileCount ?? '—'} 个文件</span></small></span></Link>
    <button type="button" className={`follow-btn${author.following ? '' : ' follow-btn--idle'}`} disabled={follow.isPending} onClick={() => follow.mutate({ expectedRevision: author.revision, following: !author.following })}>{author.following ? '已关注' : '关注'}</button>
    {author.kind !== 'cos' && (onEdit || onDelete) && <div className="author-manage-actions">{onEdit && <button className="pill" disabled={author.placeholder} onClick={onEdit}>编辑资料</button>}{onDelete && <button className="pill" disabled={author.placeholder} onClick={onDelete}>删除</button>}</div>}
    <ErrorNotice error={follow.error} />
  </li>;
}
