import { useState } from 'react';
import { useUpdateAuthor } from '../data/manage';
import type { Schema } from '../data/client';
import { ErrorNotice } from '../components/Feedback';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { UnsavedGuard } from '../lib/navigation';

type Author = Schema<'Author'>;
type Section = 'identity' | 'biography' | 'homepage';
export function AuthorDetails({ author, close, reload }: {
  author: Author; close: () => void;
  reload: () => Promise<{ data?: Author; isSuccess: boolean; error?: unknown }>;
}) {
  const [section, setSection] = useState<Section | null>(null);
  const [base, setBase] = useState(author);
  const [name, setName] = useState(author.name);
  const [aliases, setAliases] = useState(author.aliases?.join('\n') ?? '');
  const [biography, setBiography] = useState(author.biography ?? '');
  const [homepage, setHomepage] = useState(author.homepage ?? '');
  const [discard, setDiscard] = useState(false);
  const [saved, setSaved] = useState(false);
  const [comparison, setComparison] = useState<Author>();
  const [readError, setReadError] = useState<unknown>(null);
  const mutation = useUpdateAuthor(author.id);
  const dirty = name !== base.name || aliases !== (base.aliases?.join('\n') ?? '') || biography !== (base.biography ?? '') || homepage !== (base.homepage ?? '');
  const save = () => {
    const fields = section === 'identity'
      ? { name, aliases: aliases.split('\n').map(value => value.trim()).filter(Boolean) }
      : section === 'biography' ? { biography } : { homepage };
    mutation.mutate({ expectedRevision: base.revision, ...fields }, {
      onSuccess: updated => {
        setBase(updated); setName(updated.name); setAliases(updated.aliases?.join('\n') ?? '');
        setBiography(updated.biography ?? ''); setHomepage(updated.homepage ?? '');
        setSection(null); setComparison(undefined); setSaved(true);
      },
    });
  };
  const loadCurrent = async () => {
    const result = await reload();
    if (result.isSuccess && result.data) { setComparison(result.data); setReadError(null); }
    else setReadError(result.error ?? new Error('未取得服务器当前资料，草稿保持不变。'));
  };
  const adoptRevision = () => {
    if (!comparison) return;
    setBase(comparison);
    // Only this section's explicit draft survives; unrelated fields adopt the latest confirmed values.
    if (section !== 'identity') { setName(comparison.name); setAliases(comparison.aliases?.join('\n') ?? ''); }
    if (section !== 'biography') setBiography(comparison.biography ?? '');
    if (section !== 'homepage') setHomepage(comparison.homepage ?? '');
    setComparison(undefined); mutation.reset();
  };
  return (
    <section className="panel form-stack">
      <div className="section-heading"><h2>详细资料</h2><button className="button button--small" disabled={mutation.isPending} onClick={() => dirty ? setDiscard(true) : close()}>关闭</button></div>
      <p className="muted">{author.name} · 修订 {base.revision}</p>
      {!section ? (
        <>
          {saved && <p role="status" className="success-text">已保存</p>}
          <button className="detail-section" disabled={base.kind === 'cos' || base.placeholder} onClick={() => setSection('identity')}><strong>基础资料</strong><span>{name} · {aliases ? '已填写别名' : '未填写别名'}</span></button>
          <button className="detail-section" onClick={() => setSection('biography')}><strong>简介</strong><span>{biography || '尚未填写'}</span></button>
          <button className="detail-section" onClick={() => setSection('homepage')}><strong>账号来源</strong><span>{homepage || '尚未填写'}</span></button>
          <details><summary>字段核实状态</summary><dl className="facts">{Object.entries(base.fieldStatus).map(([field, status]) => <div key={field}><dt>{field}</dt><dd>{status === 'confirmed' ? '已确认' : status === 'candidate' ? '候选' : '未填写'}</dd></div>)}</dl></details>
        </>
      ) : (
        <>
          {section === 'identity' ? (
            <><label>作者名称<input value={name} disabled={mutation.isPending} maxLength={200} onChange={event => setName(event.target.value)} /></label><label>别名，每行一个<textarea value={aliases} disabled={mutation.isPending} onChange={event => setAliases(event.target.value)} /></label></>
          ) : section === 'biography' ? (
            <label>简介<textarea rows={8} maxLength={20000} value={biography} disabled={mutation.isPending} onChange={event => setBiography(event.target.value)} /><span className="muted">{biography.length} / 20000</span></label>
          ) : <label>主页地址<input type="url" value={homepage} disabled={mutation.isPending} maxLength={2000} onChange={event => setHomepage(event.target.value)} /></label>}
          <ErrorNotice error={mutation.error} />
          <div className="actions">
            <button className="button button--primary" disabled={!dirty || mutation.isPending} onClick={save}>{mutation.isPending ? '正在保存…' : '保存'}</button>
            <button className="button" disabled={mutation.isPending} onClick={() => dirty ? setDiscard(true) : setSection(null)}>取消</button>
            {mutation.isError && <button className="button" onClick={() => void loadCurrent()}>读取服务器当前资料</button>}
          </div>
          <ErrorNotice error={readError} />
          {comparison && <div className="notice"><strong>服务器当前修订 {comparison.revision}</strong><p className="preserve-lines">{section === 'identity' ? `${comparison.name}\n${comparison.aliases?.join(' · ') ?? ''}` : section === 'biography' ? comparison.biography || '简介为空' : comparison.homepage || '主页为空'}</p><button className="button button--small" onClick={adoptRevision}>已核对，保留本块草稿继续编辑</button></div>}
        </>
      )}
      <UnsavedGuard dirty={dirty} />
      {discard && <ConfirmDialog title="放弃未保存的资料？" onCancel={() => setDiscard(false)} onConfirm={close}><p>当前编辑尚未保存，关闭后会恢复服务器资料。</p></ConfirmDialog>}
    </section>
  );
}
