import { useState } from 'react';
import { Link, useLocation, useParams, useSearchParams } from 'react-router-dom';
import { AuthorOperations } from './AuthorOperations';
import { useAuthor, useAuthors, useUpdateAuthor, useCreateAuthor } from '../data/manage';
import { mediaCards, useAssets } from '../data/media';
import type { Schema } from '../data/client';
import { EmptyState, ErrorNotice, Loading, PageHeading, StatusBadge } from '../components/Feedback';
import { AuthorDetails } from './AuthorDetails';
import { MediaGrid } from '../components/MediaGrid';

import { newId } from '../lib/id';

export function AuthorsPage() {
  const [params, setParams] = useSearchParams();
  const [creating, setCreating] = useState(false);
  return <><PageHeading title="作者资料" subtitle="身份、已确认资料与本地作品，在同一个地方。" action={<button className="button" onClick={() => setCreating(!creating)}>添加作者</button>} /><label className="search-field">查找作者<input value={params.get('q') ?? ''} onChange={event => setParams({ q: event.target.value }, { replace: true })} placeholder="名称或别名" /></label>{creating && <NewAuthor onDone={() => setCreating(false)} />}<AuthorsList queryText={params.get('q') ?? ''} /></>;
}

export function AuthorsList({ following, queryText = '' }: { following?: boolean; queryText?: string }) {
  const query = useAuthors(queryText, following);
  const authors = query.data?.pages.flatMap(page => page.items) ?? [];
  return <><ErrorNotice error={query.error} stale={authors.length > 0} retry={() => void query.refetch()} />{query.isPending ? <Loading /> : authors.length ? <div className="author-grid">{authors.map(author => <AuthorCard key={author.id} author={author} />)}</div> : !query.isError && <EmptyState title={following ? '还没有关注的作者' : '没有匹配的作者'} />}{query.hasNextPage && <div className="load-more"><button className="button" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>加载更多作者</button></div>}</>;
}
function AuthorCard({ author }: { author: Schema<'Author'> }) {
  const mutation = useUpdateAuthor(author.id);
  return <article className="author-card panel"><Link to={`/authors/${encodeURIComponent(author.id)}`} className="author-identity"><span className="avatar">{author.name.slice(0, 1)}</span><div><h2>{author.name}</h2>{author.kind === 'cos' && <StatusBadge>COS</StatusBadge>}<p>{author.placeholder ? '尚未确认身份' : author.aliases?.join(' · ') || '作者档案'}</p></div></Link><button className="button button--small" aria-pressed={author.following} disabled={mutation.isPending} onClick={() => mutation.mutate({ expectedRevision: author.revision, following: !author.following })}>{author.following ? '已关注' : '关注'}</button><ErrorNotice error={mutation.error} /></article>;
}
function NewAuthor({ onDone }: { onDone: () => void }) {
  const [name, setName] = useState('');
  const [attempt, setAttempt] = useState({ key: newId(), name: '' });
  const mutation = useCreateAuthor();
  return <form className="panel form-stack" onSubmit={event => {
    event.preventDefault(); const key = attempt.name === name ? attempt.key : newId(); setAttempt({ key, name });
    mutation.mutate({ key, body: { name, kind: 'regular' } }, { onSuccess: onDone });
  }}><label>作者名称<input required disabled={mutation.isPending} maxLength={200} value={name} onChange={event => setName(event.target.value)} /></label><ErrorNotice error={mutation.error} /><button className="button button--primary" disabled={mutation.isPending || !name.trim()}>保存作者</button></form>;
}
export function AuthorPage() {
  const location = useLocation();
  const { id = '' } = useParams();
  const author = useAuthor(id);
  const assets = useAssets({ authorId: id });
  const [editing, setEditing] = useState(false);
  const mutation = useUpdateAuthor(id);
  const items = assets.data?.pages.flatMap(page => page.items) ?? [];
  if (!author.data) return <><PageHeading title="作者档案" />{author.isPending ? <Loading /> : <ErrorNotice error={author.error} retry={() => void author.refetch()} />}</>;
  const current = author.data;
  return <><PageHeading title={current.name} subtitle="作者档案与本地作品" /><ErrorNotice error={author.error} stale retry={() => void author.refetch()} />
    <section className="profile-card panel"><span className="avatar avatar--large">{current.name.slice(0, 1)}</span><div className="profile-copy"><div className="actions"><h2>{current.name}</h2>{current.kind === 'cos' && <StatusBadge>COS</StatusBadge>}{current.placeholder && <StatusBadge tone="warning">其他搁置 · 未确认</StatusBadge>}</div><p className="preserve-lines">{current.biography || '尚未填写简介'}</p><p className="muted">{current.aliases?.join(' · ')}</p><div className="actions"><button className="button button--small" aria-pressed={current.following} disabled={mutation.isPending} onClick={() => mutation.mutate({ expectedRevision: current.revision, following: !current.following })}>{current.following ? '已关注' : '关注作者'}</button>{current.homepage && <a className="button button--small" href={safeExternal(current.homepage)} target="_blank" rel="noreferrer">作者主页</a>}<button className="button button--small" onClick={() => setEditing(true)}>详细资料</button></div><ErrorNotice error={mutation.error} /></div></section>
    {editing && <AuthorDetails key={current.id} author={current} close={() => setEditing(false)} reload={() => author.refetch()} />}
    {location.state?.mergeSummary && <p role="status" className="notice">作者合并已完成，影响 {location.state.mergeSummary.affected} 份媒体，保留 {location.state.mergeSummary.conflicts} 项档案冲突记录。</p>}
    <AuthorOperations author={current} disabled={editing} />
    <div className="section-heading"><h2>本地作品</h2><span className="muted">已加载 {items.length} 项</span></div><ErrorNotice error={assets.error} stale={items.length > 0} retry={() => void assets.refetch()} />{assets.isPending ? <Loading /> : items.length ? <MediaGrid items={mediaCards(items)} /> : !assets.isError && <EmptyState title="暂无本地作品" />}{assets.hasNextPage && <div className="load-more"><button className="button" disabled={assets.isFetchingNextPage} onClick={() => void assets.fetchNextPage()}>加载更多作品</button></div>}
  </>;
}
function safeExternal(value: string) {
  try { const url = new URL(value); return ['https:', 'http:'].includes(url.protocol) ? url.href : undefined; } catch { return undefined; }
}
