import { useEffect, useRef, useState } from "react";
import { FileUp } from "lucide-react";
import type { Schema } from "../data/client";
import { useAuthorLists, useAuthorListActions, findAuthorList, readListInput, exportAuthorList, downloadText } from "../data/author-lists";
import type { ListInput } from "../data/author-lists";
import { ApiError } from "../lib/errors";
import { ErrorNotice } from "../components/Feedback";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { newId } from "../lib/id";

export function AuthorListsCard({ folderFiles }: { folderFiles?: () => Promise<File[]> }) {
  const query = useAuthorLists();
  const actions = useAuthorListActions();
  const [draft, setDraft] = useState<{ body: ListInput; key: string }>();
  const [remove, setRemove] = useState<{ list: Schema<"AuthorList">; key: string }>();
  const [files, setFiles] = useState<File[]>([]);
  const [error, setError] = useState<unknown>();
  const [working, setWorking] = useState(false);
  const [result, setResult] = useState<Schema<"AuthorListResult">>();
  const input = useRef<HTMLInputElement>(null);
  const operation = useRef<AbortController | undefined>(undefined);
  const rebuildKey = useRef(newId());
  useEffect(() => () => operation.current?.abort(), []);
  const lists = query.data?.pages.flatMap(page => page.items) ?? [];
  const busy = working || actions.importList.isPending || actions.rebuild.isPending || actions.remove.isPending;
  const conflict = actions.importList.error instanceof ApiError && actions.importList.error.code === "author_list_missing_entries" ? actions.importList.error.details : undefined;
  const submit = (body: ListInput, key: string) => {
    setDraft({ body, key });
    actions.importList.mutate({ body, key }, { onSuccess: value => { setResult(value); setDraft(undefined); } });
  };
  const choose = async (file: File) => {
    if (busy || operation.current) return;
    const controller = new AbortController(); operation.current = controller;
    setWorking(true); setError(undefined); setResult(undefined); setDraft(undefined); actions.importList.reset();
    try {
      const content = await readListInput(file);
      const existing = await findAuthorList(file.name, controller.signal);
      controller.signal.throwIfAborted();
      submit({ name: file.name, content, expectedRevision: existing?.revision ?? 0 }, newId());
    } catch (failure) { if (!controller.signal.aborted) setError(failure); }
    finally { operation.current = undefined; if (!controller.signal.aborted) setWorking(false); }
  };
  const download = async (list?: Schema<"AuthorList">) => {
    if (operation.current) return;
    const controller = new AbortController(); operation.current = controller;
    setWorking(true); setError(undefined);
    try {
      const content = await exportAuthorList(list, controller.signal);
      controller.signal.throwIfAborted(); downloadText(content, list?.name ?? "作者总表.txt");
    } catch (failure) { if (!controller.signal.aborted) setError(failure); }
    finally { operation.current = undefined; if (!controller.signal.aborted) setWorking(false); }
  };
  return <section className="rank-card">
    <div className="rank-head"><h3>作者 TXT 导入</h3><span className="rank-note">{lists.length}{query.hasNextPage ? "+" : ""} 份片段</span></div>
    {folderFiles && <div className="upload-queue-actions"><button type="button" className="pill" disabled={busy || !!conflict} onClick={async () => { setError(undefined); setWorking(true); try { setFiles(await folderFiles()); } catch (failure) { setError(failure); } finally { setWorking(false); } }}>读取维护目录的TXT</button>{files.map(file => <button type="button" className="pill" key={file.name} disabled={busy || !!conflict} onClick={() => void choose(file)}>导入 {file.name}</button>)}</div>}
    <ErrorNotice error={query.error} retry={() => void query.refetch()} />
    <button type="button" className="upload-drop" disabled={busy || !!conflict} aria-label="选择作者清单 TXT 并导入" onClick={() => input.current?.click()}><FileUp aria-hidden /><b>{busy ? "正在处理…" : "选择 TXT 导入"}</b><span>{lists.length ? "点击选择新的 .txt 清单，同名文件按修订更新" : "点击选择作者清单 .txt，开始重建作者关联"}</span></button>
    <input type="file" ref={input} hidden accept=".txt,text/plain" aria-label="选择作者清单 TXT" onChange={event => { const file = event.target.files?.[0]; event.target.value = ""; if (file) void choose(file); }} />
    <div className="rank-note" style={{ marginTop: 10, display: "grid", gap: 4 }}><span>· 自动识别编号清单和纯作者名</span><span>· 同名片段更新，已手工挂靠的作品需确认</span><span>· 删除片段会重算关联，作者与媒体保留</span></div>
    <ErrorNotice error={error ?? actions.importList.error ?? actions.rebuild.error} />
    {draft && conflict && <div className="txt-conflict-panel">
      <div className="txt-conflict-head"><b>重新导入会移除以下上传挂靠条目</b><span className="txt-conflict-file">{draft.body.name}</span></div>
      {Array.isArray(conflict.missing) && <ul className="txt-conflict-list">{conflict.missing.map((row, index) => <li key={index}><strong>{String(row?.authorName ?? "")}</strong> · {String(row?.fileName ?? "")}</li>)}</ul>}
      {conflict.truncated === true && <p className="rank-note">共 {String(conflict.totalMissing)} 项，上方显示前 100 项。</p>}
      <div className="txt-conflict-actions">{(["keep", "remove"] as const).map(choice => <button key={choice} type="button" className={choice === "keep" ? "save-btn" : "pill"} disabled={busy} onClick={() => submit({ ...draft.body, conflictResolution: choice }, draft.body.conflictResolution === choice ? draft.key : newId())}>{choice === "keep" ? "保留并导入" : "确认移除"}</button>)}<button className="pill" disabled={busy} onClick={() => { setDraft(undefined); actions.importList.reset(); }}>取消</button></div>
    </div>}
    {draft && actions.importList.isError && !conflict && <button className="pill" type="button" disabled={busy} onClick={() => submit(draft.body, draft.key)}>重试原清单</button>}
    {result && <div role="status" className="rank-note">新建作者 {result.authorsCreated} · 匹配文件 {result.matchedAssets} · 更新关联 {result.changedAssets}{result.issues.map((issue, index) => <p key={index}>第 {issue.line} 行 · {issue.code}</p>)}</div>}
    <div className="upload-queue-actions"><button className="pill" disabled={busy || !!conflict || query.isError || !lists.length} onClick={() => actions.rebuild.mutate(rebuildKey.current, { onSuccess: value => { setResult(value); rebuildKey.current = newId(); } })}>重新匹配</button><button className="pill" disabled={busy} onClick={() => void download()}>导出作者总表</button></div>
    {!lists.length && !query.isPending && !query.isError && <p className="grid-empty">还没有导入片段——从上方选择 .txt 开始。</p>}
    {!!lists.length && <table className="log-table"><thead><tr><th>片段文件</th><th style={{ width: 170 }}>操作</th></tr></thead><tbody>{lists.map(list => <tr key={list.id}><td>{list.name}</td><td><button className="pill" disabled={busy} onClick={() => void download(list)}>导出</button> <button className="pill" disabled={busy || !!conflict || query.isError} onClick={() => { actions.remove.reset(); setRemove({ list, key: newId() }); }}>移除</button></td></tr>)}</tbody></table>}
    {query.hasNextPage && <button className="pill" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>更多片段</button>}
    {remove && <ConfirmDialog title={"移除片段「" + remove.list.name + "」？"} confirmLabel="确认移除片段" danger busy={actions.remove.isPending} onCancel={() => setRemove(undefined)} onConfirm={() => actions.remove.mutate(remove, { onSuccess: () => setRemove(undefined) })}><p>将从剩余片段重新计算作者与文件关联。作者行与媒体文件保留。</p><ErrorNotice error={actions.remove.error} /></ConfirmDialog>}
  </section>;
}
