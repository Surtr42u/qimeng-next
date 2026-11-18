import { useEffect, useState } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import { AuthorSuggestField } from "../components/AuthorSuggestField";
import { useAsset, mediaCards } from "../data/media";
import type { Asset } from "../data/media";
import type { Schema } from "../data/client";
import { useAssetAuthorRecords, useSaveAssetAuthors, useRebaseAssetAuthors } from "../data/asset-edit";
import type { AuthorSourceChange } from "../data/asset-edit";
import { AcquisitionSourceField } from "../components/AcquisitionSourceField";
import { Preview } from "../components/MediaGrid";
import { ErrorNotice, Loading } from "../components/Feedback";
import { UnsavedGuard } from "../lib/navigation";
import { ApiError } from "../lib/errors";

const sameIds = (left: string[], right: string[]) => left.length === right.length && left.every(id => right.includes(id));

export function AssetEditPage() {
  const { id = "" } = useParams();
  const query = useAsset(id);
  return (
    <div className="page" id="page-asset-edit">
      {!query.data ? query.isPending ? <Loading text="资产加载中…" /> : <ErrorNotice error={query.error} retry={() => void query.refetch()} /> : <AssetAuthorLoader key={id} asset={query.data} />}
    </div>
  );
}

function AssetAuthorLoader({ asset }: { asset: Asset }) {
  const [ids] = useState(() => asset.authorStatus === "unknown" ? [] : asset.authorIds);
  const records = useAssetAuthorRecords(ids);
  const failure = records.find(record => record.isError && !record.data);
  if (failure) return <ErrorNotice error={failure.error} retry={() => void failure.refetch()} />;
  if (records.some(record => !record.data)) return <Loading text="关联作者加载中…" />;
  return <AssetAuthorDraft asset={asset} authors={records.map(record => record.data!)} />;
}

function AssetAuthorDraft({ asset, authors }: { asset: Asset; authors: Schema<"Author">[] }) {
  const navigate = useNavigate();
  const location = useLocation();
  const [base, setBase] = useState(asset);
  const [fixed] = useState(() => authors.filter(author => author.kind === "cos").map(author => author.id));
  const [selectedIds, setSelectedIds] = useState(() => authors.filter(author => author.kind !== "cos" && !author.placeholder).map(author => author.id));
  const [baselineIds, setBaselineIds] = useState(selectedIds);
  const [pending, setPending] = useState<string[]>([]);
  const [sources, setSources] = useState<Record<string, AuthorSourceChange>>({});
  const [saved, setSaved] = useState(false);
  const records = useAssetAuthorRecords(selectedIds);
  const save = useSaveAssetAuthors();
  const rebase = useRebaseAssetAuthors();
  const [reviewed, setReviewed] = useState(false);
  const staleRevision = (error: unknown) => error instanceof ApiError && error.code === "revision_conflict";
  const conflict = staleRevision(save.data?.linkError) || save.data?.sourceErrors.some(item => staleRevision(item.error));
  const detached = Object.keys(sources).filter(id => !selectedIds.includes(id));
  const busy = save.isPending || rebase.isPending;
  const linksChanged = !sameIds(baselineIds, selectedIds) || pending.length > 0;
  const dirty = linksChanged || Object.keys(sources).length > 0;
  const back = () => navigate("/asset/" + encodeURIComponent(asset.id), { replace: true, state: location.state });
  useEffect(() => { if (saved && !dirty && !save.isPending) navigate("/asset/" + encodeURIComponent(asset.id), { replace: true, state: location.state }); }, [saved, dirty, save.isPending, asset.id, navigate, location.state]);
  const remove = (id: string) => {
    setSelectedIds(values => values.filter(value => value !== id));
    setSources(values => { const next = { ...values }; delete next[id]; return next; });
  };
  const apply = () => save.mutate({
    asset: base, authorIds: linksChanged ? selectedIds : undefined, newAuthorNames: pending.length ? pending : undefined, sourceChanges: Object.values(sources),
  }, { onSuccess: result => {
    if (!result.linkError && linksChanged) {
      const ids = result.savedAsset.authorStatus === "unknown" ? [] : result.savedAsset.authorIds.filter(id => !fixed.includes(id) && id !== result.savedAsset.cosDirectoryAuthorId);
      setBase(result.savedAsset); setSelectedIds(ids); setBaselineIds(ids); setPending([]);
    }
    const committed = new Set(result.savedSources.map(author => author.id));
    setSources(values => Object.fromEntries(Object.entries(values).filter(([id]) => !committed.has(id))));
    if (!result.linkError && !result.sourceErrors.length) setSaved(true);
  } });
  return (
    <>
      <div className="settings-actions"><button className="pill" disabled={busy} onClick={back}>← 返回详情</button></div>
      <div className="page-head"><h2 data-page-heading tabIndex={-1}>编辑资产</h2><p>维护关联作者与每作者来源区（保存为整体替换）</p></div>
      <div className="settings-card">
        <h3>资产信息</h3>
        <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
          <div className="asset-edit-thumb"><Preview item={mediaCards([asset])[0]!} /></div>
          <div><b>{asset.cosWorkName || asset.fileName}</b><p className="rank-note">{asset.fileName}</p></div>
        </div>
      </div>
      <div className="settings-card">
        <h3>关联作者</h3>
        <p>在下方输入框联想选择已有作者，或对没有的作者点「新建：xxx」直接创建；COS 作者不在此列（由 COS 目录语义决定）。</p>
        <AuthorSuggestField label="查找作者" disabled={busy} selectedIds={selectedIds}
          onChoose={author => { if (author) setSelectedIds(values => values.includes(author.id) ? values : [...values, author.id]); }}
          onCreate={name => setPending(values => values.includes(name) ? values : [...values, name])} />
        {!selectedIds.length && !pending.length && <p className="grid-empty">暂无关联作者——联想选择添加，或保存空集解除全部常规作者</p>}
        {selectedIds.map((id, index) => {
          const record = records[index]!;
          const author = record.data;
          if (!author) return <div key={id}>{record.isPending ? <Loading text="关联作者加载中…" /> : <ErrorNotice error={record.error} retry={() => void record.refetch()} />}</div>;
          const draft = sources[id];
          return <div key={id} style={{ marginTop: 12 }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
              <b>{author.name}</b><button className="pill" title="从本资产移除该作者" disabled={busy} onClick={() => remove(id)}>移除</button>
            </div>
            <AcquisitionSourceField selected={draft?.sources ?? author.acquisitionSources ?? []} disabled={busy} onChange={values => setSources(current => {
              const next = { ...current };
              const original = current[id]?.author ?? author;
              if (sameIds(values, original.acquisitionSources ?? [])) delete next[id];
              else next[id] = { author: original, sources: values };
              return next;
            })} />
          </div>;
        })}
        {!!pending.length && <div style={{ marginTop: 12 }}><p className="rank-note">待新建作者（保存后自动创建并关联）</p>{pending.map(name => <div key={name} style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}><b>{name}</b><button type="button" className="pill" title="取消新建该作者" disabled={busy} onClick={() => setPending(values => values.filter(value => value !== name))}>移除</button></div>)}</div>}
        <ErrorNotice error={save.error || save.data?.linkError} />
        {save.data?.sourceErrors.map(failure => <div key={failure.id}><p className="rank-note">{failure.name}：来源尚未保存，草稿已保留。</p><ErrorNotice error={failure.error} /></div>)}
        {conflict && <button className="pill" type="button" disabled={busy} onClick={() => rebase.mutate({ id:asset.id, baseline:baselineIds, selected:selectedIds, sources }, { onSuccess: result => { setBase(result.asset); setBaselineIds(result.regular); setSelectedIds(result.merged); setSources(result.changes); setReviewed(true); save.reset(); } })}>读取当前版本并保留我的改动</button>}
        <ErrorNotice error={rebase.error} />
        {reviewed && <p className="rank-note" role="status">已读取当前版本。请核对关联作者及来源，然后保存。</p>}
        {detached.map(id => <div key={id} className="rank-note">{sources[id]!.author.name} 的来源草稿已保留。此作者已解除关联。 <button type="button" className="pill" disabled={busy} onClick={() => setSelectedIds(values=>[...values,id])}>重新关联</button><button type="button" className="pill" disabled={busy} onClick={() => remove(id)}>放弃该来源草稿</button></div>)}
        <div className="settings-actions" style={{ marginTop: 12 }}><button className="save-btn" disabled={busy || !dirty || !!conflict || !!detached.length} onClick={apply}>{save.isPending ? "保存中…" : "保存"}</button></div>
      </div>
      <UnsavedGuard dirty={dirty || busy} />
    </>
  );
}
