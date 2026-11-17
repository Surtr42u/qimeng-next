import { useEffect, useId, useState } from 'react';
import { useNameSuggestions, useNamingContext } from '../data/naming';
import { ErrorNotice, Loading } from '../components/Feedback';

export function NamingControls({ libraryId, directory, collectionId, enabled, family, setEnabled, setFamily, busy }: {
  libraryId: string; directory: string; collectionId: string; enabled: boolean; family: string;
  setEnabled: (value: boolean) => void; setFamily: (value: string) => void; busy: boolean;
}) {
  const [mediaType, setMediaType] = useState<'image' | 'video'>('image');
  const [query, setQuery] = useState(family);
  const listId = useId();
  useEffect(() => { const timer = window.setTimeout(() => setQuery(family), 250); return () => window.clearTimeout(timer); }, [family]);
  const context = useNamingContext({ libraryId, directory, family: query, mediaType, collectionId: collectionId || undefined }, enabled || !!collectionId);
  const suggestions = useNameSuggestions(libraryId, directory, query, mediaType, enabled && !collectionId);
  return <section className="panel form-stack">
    <h2>作品命名</h2>
    <label className="check-label"><input type="checkbox" checked={enabled} disabled={busy} onChange={event => setEnabled(event.target.checked)} />按作品基名自动编号</label>
    {collectionId && <p className="muted">已选集合将按服务端现有作品继续追加页码或短剧集号。不同集合和普通作品的编号规则由服务端分别处理。</p>}
    {(enabled || collectionId) && <>
      <label>作品基名<input maxLength={200} list={listId} value={family} disabled={busy} onChange={event => setFamily(event.target.value)} placeholder={collectionId ? '可留空，使用集合名称' : '作品名称，无需填写编号'} /></label>
      <datalist id={listId}>{suggestions.data?.items.map(item => <option value={item.family} key={item.family}>{item.suggestedName}</option>)}</datalist>
      <label>查看编号类别<select value={mediaType} onChange={event => setMediaType(event.target.value as typeof mediaType)}><option value="image">图片 / 动图</option><option value="video">视频</option></select></label>
      <ErrorNotice error={context.error ?? suggestions.error} retry={() => { if (libraryId && (query.trim() || collectionId)) void context.refetch(); if (enabled && !collectionId && libraryId && query.trim()) void suggestions.refetch(); }} />
      {(context.isFetching || suggestions.isFetching) && <Loading text="正在读取命名规则…" />}
      {suggestions.data?.items.length ? <div className="actions">{suggestions.data.items.map(item => <button className="button button--small" type="button" key={item.family} disabled={busy} onClick={() => setFamily(item.family)}>{item.suggestedName}</button>)}</div> : null}
      {context.data && <p role="status" className="notice">“{context.data.family}”：{context.data.nextChapter !== undefined ? `下一集：${context.data.nextChapter}` : context.data.nextPage !== undefined ? `当前作品号 ${context.data.currentWorkNumber}，下一页 ${context.data.nextPage}` : `下一个${context.data.numberClass === 'video' ? '视频' : '图片'}作品号：${context.data.nextWorkNumber}`}。预览不占号，正式保存时再次核对。</p>}
      <p className="muted">选取文件后会用服务端预览实际名称。同一集合或基名按选取顺序入库，前项失败时后项等待；不在浏览器或 AI 内计算编号。</p>
    </>}
  </section>;
}
