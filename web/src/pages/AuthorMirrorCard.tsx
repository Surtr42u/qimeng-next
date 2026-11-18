import { useRef, useState } from "react";
import type { Schema } from "../data/client";
import { useAuthorLists, useAuthorMirror, useUpdateAuthorMirror } from "../data/author-lists";
import { ErrorNotice } from "../components/Feedback";
import { UnsavedGuard } from "../lib/navigation";
import { newId } from "../lib/id";

export function AuthorMirrorCard() {
  const mirror = useAuthorMirror();
  const lists = useAuthorLists();
  const save = useUpdateAuthorMirror();
  const [draft, setDraft] = useState<Schema<"AuthorListMirrorUpdate">>();
  const attempt = useRef({ signature: "", key: newId() });
  const source = mirror.data;
  const shown = draft ?? (source && { expectedRevision: source.revision, path: source.path, mode: source.mode, listId: source.listId });
  const change = (fields: Partial<Schema<"AuthorListMirrorUpdate">>) => { if (shown) setDraft({ ...shown, ...fields }); };
  const busy = !source || mirror.isError || save.isPending;
  const submit = () => {
    if (!draft) return;
    const body = { ...draft, path: draft.path.trim(), listId: draft.mode === "fragment" && draft.path.trim() ? draft.listId : undefined };
    const signature = JSON.stringify(body);
    if (attempt.current.signature !== signature) attempt.current = { signature, key: newId() };
    save.mutate({ body, key: attempt.current.key }, { onSuccess: () => setDraft(undefined) });
  };
  return <div className="settings-card">
    <h3>作者总表镜像</h3>
    <ErrorNotice error={mirror.error} stale={!!source} retry={() => void mirror.refetch()} />
    <div className="settings-grid">
      <label className="settings-field"><span>镜像文件路径</span><input type="text" placeholder="服务端可写的绝对路径；留空 = 关闭" autoComplete="off" maxLength={4096} value={shown?.path ?? ""} disabled={busy} onChange={event => change({ path: event.target.value })} /><small>使用服务器受信目录内的绝对 TXT 路径；留空关闭镜像。</small></label>
      <label className="settings-field"><span>镜像目标片段</span><select value={shown?.mode === "fragment" ? shown.listId || "" : "master"} disabled={busy} onChange={event => change(event.target.value === "master" ? { mode: "master", listId: undefined } : { mode: "fragment", listId: event.target.value || undefined })}><option value="master">作者总表</option><option value="">最近导入的片段</option>{lists.data?.pages.flatMap(page => page.items).map(list => <option key={list.id} value={list.id}>{list.name}</option>)}{shown?.mode === "fragment" && shown.listId && !lists.data?.pages.some(page => page.items.some(list => list.id === shown.listId)) && <option value={shown.listId}>当前指定的片段</option>}</select><small>多片段场景可指定镜像的片段。</small></label>
    </div>
    <ErrorNotice error={lists.error} retry={() => void lists.refetch()} />
    {lists.hasNextPage && <button type="button" className="pill" disabled={lists.isFetchingNextPage} onClick={() => void lists.fetchNextPage()}>更多片段</button>}
    <p className="rank-note" style={{ marginTop: 10 }}>单向同步：手动编辑的正文须先核对或导入，摘要不符时服务器会停止覆盖。</p>
    {source && <p className="rank-note" role="status">{({ disabled: "镜像已关闭", pending: "镜像配置已保存，等待写入", current: "镜像文件已更新", error: "镜像写入未完成" })[source.state]}{source.errorCode ? " · " + source.errorCode : ""}</p>}
    <div className="settings-actions" style={{ marginTop: 12 }}><button className="save-btn" type="button" disabled={busy || !draft} onClick={submit}>{save.isPending ? "保存中…" : "保存镜像配置"}</button><button className="pill" type="button" disabled={save.isPending || !draft} onClick={() => { setDraft(undefined); save.reset(); }}>放弃修改</button></div>
    <ErrorNotice error={save.error} />
    {source && draft && source.revision !== draft.expectedRevision && <button className="pill" type="button" onClick={() => { setDraft({ ...draft, expectedRevision: source.revision }); save.reset(); }}>已核对当前配置，保留草稿继续</button>}
    <UnsavedGuard dirty={!!draft || save.isPending} />
  </div>;
}
