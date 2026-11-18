import { useRef, useState } from "react";
import { Dialog } from "radix-ui";
import { useSourceRules } from "../data/source-rules";
import type { Schema } from "../data/client";
import { ApiError } from "../lib/errors";
import { newId } from "../lib/id";
import { UnsavedGuard } from "../lib/navigation";
import { ManagePageHeader } from "../components/LegacyManageHeader";
import { VocabularyGroupsCard } from "../components/VocabularyGroupsCard";
import { ErrorNotice, Loading } from "../components/Feedback";

type Layer = Schema<"SourceRuleLayer">;
type Draft = { layer: Layer; revision: number };
type Prompt = { title: string; initial?: string; confirm: (value: string) => void };
const unique = (list: string[], name: string) => { if (!list.includes(name)) list.push(name); };

export function SourceRulesPage() {
  const { query, save } = useSourceRules();
  const [draft, setDraft] = useState<Draft>(), [prompt, setPrompt] = useState<Prompt>();
  const [reviewed, setReviewed] = useState<Schema<"SourceRuleSettings">>();
  const [notice, setNotice] = useState("");
  const [editError, setEditError] = useState<unknown>();
  const attempt = useRef(newId());
  const state = query.data;
  const layer = draft?.layer ?? state;
  const busy = save.isPending;
  const edit = (fn: (next: Layer) => void) => {
    if (!layer || !state || busy) return;
    const next = structuredClone({ groups: layer.groups, stopWords: layer.stopWords });
    try { fn(next); setEditError(undefined); } catch (error) { setEditError(error); return; }
    if (JSON.stringify(next) === JSON.stringify({ groups: layer.groups, stopWords: layer.stopWords })) return;
    setDraft({ layer: next, revision: draft?.revision ?? state.revision }); setNotice(""); attempt.current = newId();
  };
  const ask = (title: string, apply: (next: Layer, name: string) => void, initial?: string) => setPrompt({ title, initial, confirm: value => edit(next => apply(next, value)) });
  const conflict = save.error instanceof ApiError && save.error.code === "revision_conflict";
  const submit = () => {
    if (!state || !layer || busy) return;
    save.mutate({ body: { groups: layer.groups, stopWords: layer.stopWords, expectedRevision: draft?.revision ?? state.revision }, id: attempt.current }, {
      onSuccess: result => { setDraft(undefined); setReviewed(undefined); setNotice("已保存：" + result.groups.length + " 组出处、" + result.stopWords.length + " 个停用词，服务端将后台重算"); }
    });
  };
  return <div className="page manage-workspace" id="page-maintenance-files-vocabulary-edit">
    <ManagePageHeader title="词表维护" description="出处组与停用词的检索词表追加层编辑（内置基线恒生效）" />
    <ErrorNotice error={query.error} stale={!!state} retry={() => void query.refetch()} />
    <ErrorNotice error={save.error} />
    <ErrorNotice error={editError} />
    {conflict && <div className="vocab-banner vocab-banner--error"><button type="button" className="vocab-text-btn" onClick={async () => { const fresh = await query.refetch(); if (fresh.isSuccess) setReviewed(fresh.data); }}>读取服务器当前词表</button>
      {reviewed && draft && <><details><summary>核对当前服务器修订 {reviewed.revision}</summary><pre>{JSON.stringify({ groups: reviewed.groups, stopWords: reviewed.stopWords }, null, 2)}</pre></details>
        <button type="button" className="vocab-text-btn" onClick={() => { setDraft({ ...draft, revision: reviewed.revision }); setReviewed(undefined); attempt.current = newId(); save.reset(); }}>已核对，保留草稿用于整组替换</button>
        <button type="button" className="vocab-text-btn" onClick={() => { setDraft(undefined); setReviewed(undefined); save.reset(); }}>放弃草稿，使用服务器词表</button></>}
    </div>}
    {notice && <div className="vocab-banner vocab-banner--notice" role="status"><span>{notice}</span><button className="vocab-text-btn" type="button" onClick={() => setNotice("")}>关闭</button></div>}
    {state?.rebuildState === "failed" && <div className="vocab-banner vocab-banner--error" role="alert">服务端重算未完成，已处理 {state.processed} 项。可重试当前规则。</div>}
    {query.isPending ? <div className="settings-card"><Loading text="正在读取词表…" /></div> : layer && <>
      <div className="settings-card"><div className="vocab-head"><h3>停用词</h3><button type="button" className="vocab-text-btn" disabled={busy || layer.stopWords.length >= 256} onClick={() => ask("新增停用词（兜底提取将跳过该词）", (d,n) => unique(d.stopWords,n))}>添加停用词</button></div>
        {layer.stopWords.length ? layer.stopWords.map((word,i) => <div key={i} className="vocab-row"><span className="vocab-item-text">{word}</span><button type="button" className="vocab-text-btn" disabled={busy} onClick={() => edit(d => { d.stopWords.splice(i,1); })}>移除</button></div>) : <p className="vocab-note">没有自定义停用词（内置冻结基线始终生效，这里只维护追加层）</p>}
      </div>
      <VocabularyGroupsCard groups={layer.groups} busy={busy}
        onAddGroup={() => ask("新增出处组（规范名）",(d,n) => { if (!d.groups.some(g => g.canonical === n)) d.groups.push({ canonical:n, variants:[], characters:[] }); })}
        onRemoveGroup={i => edit(d => { d.groups.splice(i,1); })}
        onRenameGroup={(i,old) => ask("修改组规范名",(d,n) => { const g=d.groups[i]!; if (old!==n) { if(d.groups.some(other=>other.canonical===n))throw new Error("已有同名出处组，原词组已保留。"); if(!g.variants.includes(old)&&g.variants.length>=64)throw new Error("变体已满，请先留出旧名称的位置。"); unique(g.variants,old); g.canonical=n;} },old)}
        onAddVariant={i => ask("新增变体写法",(d,n) => unique(d.groups[i]!.variants,n))}
        onRemoveVariant={(i,v) => edit(d => {d.groups[i]!.variants.splice(v,1);})}
        onAddCharacter={i => ask("新增角色（规范名）",(d,n) => {const chars=d.groups[i]!.characters;if(!chars.some(c=>c.canonical===n)) chars.push({canonical:n,aliases:[]});})}
        onRemoveCharacter={(i,c) => edit(d => {d.groups[i]!.characters.splice(c,1);})}
        onRenameCharacter={(i,c,old) => ask("修改角色规范名",(d,n) => {const roles=d.groups[i]!.characters, role=roles[c]!;if(old!==n){if(roles.some(other=>other.canonical===n))throw new Error("该组已有同名角色，原角色已保留。");if(!role.aliases.includes(old)&&role.aliases.length>=32)throw new Error("别名已满，请先留出旧名称的位置。");unique(role.aliases,old);role.canonical=n;}},old)}
        onAddAlias={(i,c) => ask("新增角色别名",(d,n) => unique(d.groups[i]!.characters[c]!.aliases,n))}
        onRemoveAlias={(i,c,a) => edit(d => {d.groups[i]!.characters[c]!.aliases.splice(a,1);})} />
      <button type="button" className="save-btn vocab-save" disabled={(!draft && state?.rebuildState !== "failed") || busy || conflict} onClick={submit}>{busy ? "保存中…" : state?.rebuildState === "failed" && !draft ? "重试重算" : "保存修改"}</button>
    </>}
    <ul className="vocab-rules"><li>· 编辑对象是当前连接服务器的检索词表，保存后整体替换并自动后台重算</li><li>· 组规范名与内置 133 组同名 = 并入扩词条，新名 = 追加新组</li><li>· 变体和别名保留改名前的拼写，明确填写或清空的归属优先</li></ul>
    {prompt && <TermPrompt request={prompt} dismiss={() => setPrompt(undefined)} />}
    <UnsavedGuard dirty={!!draft || busy} />
  </div>;
}
function TermPrompt({ request, dismiss }: { request: Prompt; dismiss: () => void }) {
  const [text,setText]=useState(request.initial ?? "");
  const accept=()=>{if(text.trim()){request.confirm(text.trim());dismiss();}};
  return <Dialog.Root open onOpenChange={open=>{if(!open)dismiss();}}><Dialog.Portal><Dialog.Overlay className="detail-dialog-overlay" />
    <Dialog.Content className="detail-dialog detail-dialog--vocab-prompt"><Dialog.Title className="detail-dialog-title">{request.title}</Dialog.Title><Dialog.Description className="detail-dialog-desc">输入词条（自动去除首尾空格，服务端保存时去重）</Dialog.Description>
      <div className="tag-newrow" style={{marginTop:14}}><input autoFocus className="tag-input" value={text} placeholder="输入词条，回车确认" onChange={e=>setText(Array.from(e.target.value).slice(0,100).join(""))} onKeyDown={e=>{if(e.key==="Enter"&&!e.nativeEvent.isComposing){e.preventDefault();accept();}}} /><button type="button" className="confirm-btn confirm-btn--primary" disabled={!text.trim()} onClick={accept}>确定</button></div>
      <div className="vocab-counter" style={{marginTop:6}}>{Array.from(text).length}/100</div><div className="detail-dialog-foot"><button className="confirm-btn confirm-btn--cancel" onClick={dismiss}>取消</button></div>
    </Dialog.Content></Dialog.Portal></Dialog.Root>;
}
