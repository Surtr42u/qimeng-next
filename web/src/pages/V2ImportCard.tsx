import { useEffect, useRef, useState } from "react";
import { FileUp } from "lucide-react";
import { useV2Inputs, stageV2, planV2, useV2Review, useV2Commit } from "../data/backups";
import type { Schema } from "../data/client";
import { ErrorNotice } from "../components/Feedback";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { newId } from "../lib/id";

export function V2ImportCard({ selected }: { selected?: File }) {
  const input = useRef<HTMLInputElement>(null);
  const requests = useRef<AbortController | undefined>(undefined);
  useEffect(() => () => requests.current?.abort(), []);
  const [file, setFile] = useState<File>();
  const [sourceId, setSourceId] = useState("");
  const [staged, setStaged] = useState<Schema<"V2Import">>();
  const [plan, setPlan] = useState<Schema<"V2MigrationPlan">>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [skipped, setSkipped] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [mediaMap, setMediaMap] = useState<Record<string, string>>({});
  const [authorMap, setAuthorMap] = useState<Record<string, string>>({});
  const attempt = useRef({ stage: newId(), plan: newId(), commit: newId() });
  const inputs = useV2Inputs();
  const items = inputs.data?.pages.flatMap(page => page.items) ?? [];
  const review = useV2Review(plan?.id ?? "");
  const commit = useV2Commit();
  const locked = busy || commit.isPending;
  const inputsChanged = Object.keys(mediaMap).length > 0 || Object.keys(authorMap).length > 0;
  const needsConsent = !!plan && (plan.skippedCount > 0 || plan.issueCount > 0);
  const reset = (chosen?: File) => {
    setFile(chosen); setStaged(undefined); setPlan(undefined); setMediaMap({}); setAuthorMap({}); setError(undefined); setSkipped(false); commit.reset();
    attempt.current = { stage: newId(), plan: newId(), commit: newId() };
  };
  useEffect(() => { if (selected) reset(selected); }, [selected]);
  const prepare = async (existing?: Schema<"V2Import">) => {
    if (requests.current || locked || (!existing && !file && !staged)) return;
    const controller = new AbortController(); requests.current = controller;
    setBusy(true); setError(undefined);
    try {
      const document = existing ?? staged ?? await stageV2(file!, sourceId.trim(), attempt.current.stage, controller.signal);
      controller.signal.throwIfAborted();
      setStaged(document);
      const body: Schema<"V2PlanCreate"> = {
        mediaMappings: Object.entries(mediaMap).map(([recordKey, assetId]) => ({ recordKey, assetId: assetId.trim() })),
        authorMappings: Object.entries(authorMap).filter(([, id]) => id.trim()).map(([sourceAuthorId, authorId]) => ({ sourceAuthorId, authorId: authorId.trim() })),
      };
      const prepared = await planV2(document.id, attempt.current.plan, body, controller.signal);
      controller.signal.throwIfAborted();
      setPlan(prepared); setMediaMap({}); setAuthorMap({}); setSkipped(false);
      attempt.current.commit = newId(); void inputs.refetch();
    } catch (failure) { if (!controller.signal.aborted) setError(failure); }
    finally { requests.current = undefined; if (!controller.signal.aborted) setBusy(false); }
  };
  const changeMapping = (key: string, value: string, author: boolean) => {
    const update = author ? setAuthorMap : setMediaMap;
    update(previous => ({ ...previous, [key]: value })); attempt.current.plan = newId();
  };
  return <div className="v2-import">
    <button type="button" className="upload-drop" disabled={locked} onClick={() => input.current?.click()}><FileUp aria-hidden /><b>选择备份文件导入恢复</b><span>选择旧版 v2 JSON，先预检资产、作者和事件，再确认合并。</span></button>
    <input ref={input} type="file" hidden accept=".json,application/json" aria-label="选择旧版 v2 备份 JSON 文件" onChange={event => { const chosen = event.target.files?.[0]; event.target.value = ""; if (chosen) reset(chosen); }} />
    {!!file && !staged && <div className="settings-field"><strong>{file.name}</strong><label>来源实例标识<input value={sourceId} maxLength={128} disabled={locked} list="v2-known-sources" onChange={event => { setSourceId(event.target.value); attempt.current.stage = newId(); }} /></label><datalist id="v2-known-sources">{[...new Set(items.map(item => item.sourceId))].map(id => <option key={id}>{id}</option>)}</datalist><small>同一旧影库的多次导出使用同一标识；首次可生成并妥善记录。</small><div className="pill-row"><button className="pill" type="button" disabled={locked} onClick={() => { setSourceId(newId()); attempt.current.stage = newId(); }}>生成新来源标识</button><button className="save-btn" type="button" disabled={locked || sourceId.trim().length < 8} onClick={() => void prepare()}>预检备份</button></div></div>}
    <ErrorNotice error={error} />
    {staged && <div className="rank-note">已保留 v2 原始文档。媒体 {staged.mediaCount} · 作者 {staged.authorCount} · 事件 {staged.eventCount} · 来源 {staged.sourceId}{!plan && <button className="pill" disabled={locked} onClick={() => void prepare()}>生成预检报告</button>}</div>}
    {plan && <div>
      <h4>导入预检</h4><p className="rank-note">匹配 {plan.matchedCount} / {plan.mediaCount} · 已跳过 {plan.skippedCount} · 问题 {plan.issueCount}。旧版未记录的原件摘要无法补证。</p>
      {plan.issues.map((issue, index) => <p className="rank-note" key={index}>{issue}</p>)}
      <details><summary>逐项匹配与冲突</summary>
        <ErrorNotice error={review.media.error ?? review.authors.error} retry={() => { void review.media.refetch(); void review.authors.refetch(); }} />
        {review.media.data?.pages.flatMap(page => page.items).map(item => <div className="settings-field" key={item.recordKey}><b>{item.fileName}</b><small>{item.state} · {item.reason} · {item.issues.join("、")}</small>{plan.state === "prepared" && <label>目标资产 ID<input disabled={locked} value={mediaMap[item.recordKey] ?? item.assetId ?? ""} placeholder="明确选择目标，或清空以跳过" onChange={event => changeMapping(item.recordKey, event.target.value, false)} /></label>}</div>)}
        {review.media.hasNextPage && <button className="pill" disabled={review.media.isFetchingNextPage} onClick={() => void review.media.fetchNextPage()}>更多媒体</button>}
        {review.authors.data?.pages.flatMap(page => page.items).map(item => <div className="settings-field" key={item.sourceAuthorId}><b>{item.name}</b><small>{item.state} · {item.reason}</small>{plan.state === "prepared" && <label>目标作者 ID<input disabled={locked} value={authorMap[item.sourceAuthorId] ?? item.authorId ?? ""} onChange={event => changeMapping(item.sourceAuthorId, event.target.value, true)} /></label>}</div>)}
        {review.authors.hasNextPage && <button className="pill" disabled={review.authors.isFetchingNextPage} onClick={() => void review.authors.fetchNextPage()}>更多作者</button>}
      </details>
      {plan.state === "prepared" ? <div className="settings-actions">
        {inputsChanged && <button type="button" className="pill" disabled={locked} onClick={() => void prepare()}>重新生成预检</button>}
        {needsConsent && <label><input type="checkbox" checked={skipped} disabled={locked} onChange={event => { setSkipped(event.target.checked); attempt.current.commit = newId(); }} />已核对并接受报告中的跳过项和冲突</label>}
        <button className="save-btn" type="button" disabled={locked || inputsChanged || (needsConsent && !skipped) || !review.media.data || !review.authors.data || review.media.isError || review.authors.isError} onClick={() => setConfirm(true)}>导入恢复</button>
      </div> : <p role="status">服务端已提交{plan.state === "partial" ? "可应用部分" : "此计划"}。导入前检查点：{plan.checkpointId || "未返回"}</p>}
    </div>}
    {confirm && plan && <ConfirmDialog title="导入这份旧版 v2 备份？" confirmLabel="确认合并导入" busy={commit.isPending} onCancel={() => setConfirm(false)} onConfirm={() => commit.mutate({ plan, acceptSkipped: skipped, key: attempt.current.commit }, { onSuccess: result => { setPlan(result); setConfirm(false); } })}><p>按刚核对的计划合并资料与事件；原件保留。来源实例为 {staged?.sourceId}。</p><ErrorNotice error={commit.error} /></ConfirmDialog>}
    <details><summary>已暂存的 v2 文档</summary><ErrorNotice error={inputs.error} retry={() => void inputs.refetch()} />{items.map(item => <button type="button" className="pill" key={item.id} disabled={locked} onClick={() => { reset(); setStaged(item); }}>{item.sourceId} · {item.createdAt}</button>)}{inputs.hasNextPage && <button type="button" className="pill" disabled={inputs.isFetchingNextPage} onClick={() => void inputs.fetchNextPage()}>更多文档</button>}</details>
  </div>;
}
