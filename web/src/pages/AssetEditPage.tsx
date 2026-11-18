import { useEffect, useState } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import { AuthorSuggestField } from "../components/AuthorSuggestField";
import { useAsset, mediaCards } from "../data/media";
import type { Asset } from "../data/media";
import type { Schema } from "../data/client";
import { useAssetAuthorRecords, useSaveAssetAuthors } from "../data/asset-edit";
import type { AuthorSourceChange } from "../data/asset-edit";
import { AcquisitionSourceField } from "../components/AcquisitionSourceField";
import { Preview } from "../components/MediaGrid";
import { ErrorNotice, Loading } from "../components/Feedback";
import { UnsavedGuard } from "../lib/navigation";

const sameIds = (left: string[], right: string[]) => left.length === right.length && left.every(id => right.includes(id));

export function AssetEditPage() {
  const { id = "" } = useParams();
  const query = useAsset(id);
  return (
    <div className="page" id="page-asset-edit">
      {!query.data ? query.isPending ? <Loading text="资产加载中…" /> : <ErrorNotice error={query.error} retry={() => void query.refetch()} /> : <AssetAuthorLoader asset={query.data} />}
    </div>
  );
}

function AssetAuthorLoader({ asset }: { asset: Asset }) {
  const [ids] = useState(() => asset.authorStatus === "unknown" ? [] : asset.authorIds);
  const records = useAssetAuthorRecords(ids);
  const failure = records.find(record => record.isError && !record.data);
  if (failure) return <ErrorNotice error={failure.error} retry={() => void failure.refetch()} />;
  if (records.some(record => !record.data)) return <Loading text="关联作者加载中…" />;
  return <AssetAuthorDraft key={asset.id} asset={asset} authors={records.map(record => record.data!)} />;
}

function AssetAuthorDraft({ asset, authors }: { asset: Asset; authors: Schema<"Author">[] }) {
  const navigate = useNavigate();
  const location = useLocation();
  const [base, setBase] = useState(asset);
  const [fixed] = useState(authors.filter(author => author.kind === "cos"));
  const [selected, setSelected] = useState(authors.filter(author => author.kind === "regular"));
  const [baselineIds, setBaselineIds] = useState(selected.map(author => author.id));
  const [sources, setSources] = useState<Record<string, AuthorSourceChange>>({});
  const [saved, setSaved] = useState(false);
  const save = useSaveAssetAuthors();
  const selectedIds = selected.map(author => author.id);
  const linksChanged = !sameIds(baselineIds, selectedIds);
  const dirty = linksChanged || Object.keys(sources).length > 0;
  const back = () => navigate(`/asset/${encodeURIComponent(asset.id)}`, { replace: true, state: location.state });
  // Wait until UnsavedGuard has committed its clean state before navigating after success.
  useEffect(() => { if (saved && !dirty) navigate(`/asset/${encodeURIComponent(asset.id)}`, { replace: true, state: location.state }); }, [saved, dirty, asset.id, navigate, location.state]);
  const remove = (id: string) => {
    setSelected(values => values.filter(author => author.id !== id));
    setSources(values => { const next = { ...values }; delete next[id]; return next; });
  };
  const apply = () => save.mutate({
    asset: base,
    authorIds: linksChanged ? [...fixed.map(author => author.id), ...selectedIds] : undefined,
    sourceChanges: Object.values(sources),
  }, { onSuccess: result => {
    if (!result.linkError) { setBase(result.savedAsset); setBaselineIds(selectedIds); }
    const committed = new Map(result.savedSources.map(author => [author.id, author]));
    setSelected(values => values.map(author => committed.get(author.id) ?? author));
    setSources(values => Object.fromEntries(Object.entries(values).filter(([id]) => !committed.has(id))));
    if (!result.linkError && !result.sourceErrors.length) setSaved(true);
  } });
  return (
    <>
      <div className="settings-actions"><button className="pill" disabled={save.isPending} onClick={back}>← 返回详情</button></div>
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
        <p>在下方输入框联想选择已有作者；COS 作者不在此列（由 COS 目录语义决定）。</p>
        <AuthorSuggestField label="查找作者" disabled={save.isPending} selectedIds={selectedIds} onChoose={author => { if (author) setSelected(values => values.some(value => value.id === author.id) ? values : [...values, author]); }} />
        {!selected.length ? <p className="grid-empty">暂无关联作者——联想选择添加，或保存空集解除全部常规作者</p> : selected.map(author => (
          <div key={author.id} style={{ marginTop: 12 }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
              <b>{author.name}</b><button className="pill" title="从本资产移除该作者" disabled={save.isPending} onClick={() => remove(author.id)}>移除</button>
            </div>
            <AcquisitionSourceField selected={sources[author.id]?.sources ?? author.acquisitionSources ?? []} disabled={save.isPending} onChange={values => setSources(current => {
              const next = { ...current };
              if (sameIds(values, author.acquisitionSources ?? [])) delete next[author.id];
              else next[author.id] = { author, sources: values };
              return next;
            })} />
          </div>
        ))}
        <ErrorNotice error={save.error || save.data?.linkError} />
        {save.data?.sourceErrors.map(failure => <div key={failure.id}><p className="rank-note">{failure.name}：来源尚未保存，草稿已保留。</p><ErrorNotice error={failure.error} /></div>)}
        <div className="settings-actions" style={{ marginTop: 12 }}><button className="save-btn" disabled={save.isPending || !dirty} onClick={apply}>{save.isPending ? "保存中…" : "保存"}</button></div>
      </div>
      <UnsavedGuard dirty={dirty || save.isPending} />
    </>
  );
}
