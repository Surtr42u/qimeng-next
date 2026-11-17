import { useState } from 'react';
import { useAssetActions } from '../data/media';
import type { Asset } from '../data/media';
import { useAuthors } from '../data/manage';
import { useTerms } from '../data/catalog';
import { ErrorNotice } from '../components/Feedback';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { UnsavedGuard } from '../lib/navigation';

const sameSet = (left: readonly string[], right: readonly string[]) => left.length === right.length && left.every(value => right.includes(value));
export function AssetMetadataEditor({ asset, close }: { asset: Asset; close: () => void }) {
  const [base] = useState(asset);
  const [note, setNote] = useState(asset.note);
  const [authorIds, setAuthorIds] = useState([...asset.authorIds]);
  const [sourceIds, setSourceIds] = useState([...(asset.sourceIds ?? [])]);
  const [characterIds, setCharacterIds] = useState([...(asset.characterIds ?? [])]);
  const [acquisition, setAcquisition] = useState(asset.acquisitionSources?.join('\n') ?? '');
  const [q, setQ] = useState('');
  const authors = useAuthors(q); const sources = useTerms('source'); const characters = useTerms('character');
  const actions = useAssetActions(asset.id); const [discard, setDiscard] = useState(false);
  const authorsChanged = !sameSet(authorIds, base.authorIds);
  const sourceChanged = !sameSet(sourceIds, base.sourceIds ?? []);
  const characterChanged = !sameSet(characterIds, base.characterIds ?? []);
  const acquisitionChanged = acquisition !== (base.acquisitionSources?.join('\n') ?? '');
  const dirty = note !== base.note || authorsChanged || sourceChanged || characterChanged || acquisitionChanged;
  const authorItems = authors.data?.pages.flatMap(page => page.items) ?? [];
  const sourceItems = sources.data?.pages.flatMap(page => page.items) ?? [];
  const characterItems = characters.data?.pages.flatMap(page => page.items) ?? [];
  const busy = actions.update.isPending;
  return <form className="panel form-stack" onSubmit={event => {
    event.preventDefault();
    actions.update.mutate({ expectedRevision: base.revision,
      ...(note !== base.note ? { note } : {}),
      ...(authorsChanged ? { expectedAuthorsRevision: base.authorsRevision, authorIds } : {}),
      ...(sourceChanged ? { sourceIds } : {}), ...(characterChanged ? { characterIds } : {}),
      ...(acquisitionChanged ? { acquisitionSources: acquisition.split('\n').map(value => value.trim()).filter(Boolean) } : {}),
    }, { onSuccess: close });
  }}>
    <h2>编辑资料</h2><label>查找作者<input value={q} disabled={busy} onChange={event => setQ(event.target.value)} placeholder="输入名称后从结果选择" /></label>
    <p className="muted">已选 {authorIds.length} 位作者。清空保存后回到未确认占位，不删除作者档案。</p>
    <IdentityPool ids={[...new Set([...base.authorIds, ...authorItems.map(author => author.id)])]} selected={authorIds} setSelected={setAuthorIds} disabled={busy} names={Object.fromEntries(authorItems.map(author => [author.id, author.name]))} label="作者" />
    <ErrorNotice error={authors.error} retry={() => void authors.refetch()} />{authors.hasNextPage && <button type="button" className="button" disabled={authors.isFetchingNextPage} onClick={() => void authors.fetchNextPage()}>更多作者</button>}
    <details><summary>作品出处与角色</summary><p className="muted">原作与角色身份独立于获取渠道。只选择已确认关系；没有确认时可以保持为空。</p><h3>作品出处</h3><IdentityPool ids={[...new Set([...(base.sourceIds ?? []), ...sourceItems.map(item => item.id)])]} selected={sourceIds} setSelected={setSourceIds} disabled={busy || !sources.data} names={Object.fromEntries(sourceItems.map(item => [item.id, item.name]))} label="出处" /><ErrorNotice error={sources.error} retry={() => void sources.refetch()} />{sources.hasNextPage && <button type="button" className="button" disabled={sources.isFetchingNextPage} onClick={() => void sources.fetchNextPage()}>更多出处</button>}<h3>角色</h3><IdentityPool ids={[...new Set([...(base.characterIds ?? []), ...characterItems.map(item => item.id)])]} selected={characterIds} setSelected={setCharacterIds} disabled={busy || !characters.data} names={Object.fromEntries(characterItems.map(item => [item.id, item.name]))} label="角色" /><ErrorNotice error={characters.error} retry={() => void characters.refetch()} />{characters.hasNextPage && <button type="button" className="button" disabled={characters.isFetchingNextPage} onClick={() => void characters.fetchNextPage()}>更多角色</button>}</details>
    <label>获取渠道，每行一个<textarea value={acquisition} maxLength={200000} disabled={busy} onChange={event => setAcquisition(event.target.value)} rows={3} /></label>
    <label>个人备注<textarea value={note} rows={4} maxLength={10000} disabled={busy} onChange={event => setNote(event.target.value)} /></label>
    <ErrorNotice error={actions.update.error} /><div className="actions"><button className="button button--primary" disabled={!dirty || busy}>{busy ? '正在保存…' : '保存资料'}</button><button type="button" className="button" disabled={busy} onClick={() => dirty ? setDiscard(true) : close()}>取消</button></div><UnsavedGuard dirty={dirty} />
    {discard && <ConfirmDialog title="放弃资料修改？" onCancel={() => setDiscard(false)} onConfirm={close}><p>尚未保存的输入会被放弃。</p></ConfirmDialog>}
  </form>;
}
function IdentityPool({ ids, selected, setSelected, names, disabled, label }: { ids: string[]; selected: string[]; setSelected: (ids: string[]) => void; names: Record<string, string>; disabled: boolean; label: string }) {
  return <div className="tag-pool tag-pool--scroll" role="group" aria-label={label}>{ids.map(id => <button key={id} type="button" className="tag" disabled={disabled} aria-pressed={selected.includes(id)} onClick={() => setSelected(selected.includes(id) ? selected.filter(value => value !== id) : [...selected, id])}>{names[id] ?? `现有${label} ${id.slice(0, 8)}`}</button>)}</div>;
}
