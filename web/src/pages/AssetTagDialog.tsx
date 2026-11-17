import { useRef, useState } from "react";
import { Dialog } from "radix-ui";
import { useAssetActions, useTags } from "../data/media";
import type { Asset } from "../data/media";
import { useCreateTag } from "../data/catalog";
import { ErrorNotice, Loading } from "../components/Feedback";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { UnsavedGuard } from "../lib/navigation";
import { newId } from "../lib/id";
import { ApiError } from "../lib/errors";

export function AssetTagDialog({ asset, close }: { asset: Asset; close: () => void }) {
  const vocabulary = useTags();
  const actions = useAssetActions(asset.id);
  const createTag = useCreateTag();
  const [base] = useState({ revision: asset.tagsRevision, tags: [...asset.tags] });
  const [selected, setSelected] = useState([...asset.tags]);
  const [created, setCreated] = useState<string[]>([]);
  const [draft, setDraft] = useState("");
  const [discard, setDiscard] = useState(false);
  const creation = useRef({ name: "", key: newId() });
  const saving = useRef({ content: "", key: newId() });
  const busy = actions.tags.isPending || createTag.isPending;
  const dirty = selected.length !== base.tags.length || selected.some(tag => !base.tags.includes(tag));
  const candidates = [...new Set([...(vocabulary.data?.items ?? []), ...base.tags, ...created])]
    .sort((a, b) => a.localeCompare(b, "zh-CN", { numeric: true }));
  const dismiss = () => { if (!busy) dirty || draft.trim() ? setDiscard(true) : close(); };
  const create = () => {
    const name = draft.trim();
    if (!name || busy) return;
    if (creation.current.name !== name) creation.current = { name, key: newId() };
    createTag.mutate({ name, key: creation.current.key }, {
      onSuccess: tag => {
        setCreated(values => values.includes(tag.name) ? values : [...values, tag.name]);
        setSelected(values => values.includes(tag.name) ? values : [...values, tag.name]);
        setDraft("");
      },
    });
  };
  const save = () => {
    const content = JSON.stringify(selected);
    if (saving.current.content !== content) saving.current = { content, key: newId() };
    actions.tags.mutate({ key: saving.current.key, body: { expectedRevision: base.revision, tags: selected } }, {
      onSuccess: close,
      onError: error => { if (error instanceof ApiError && error.code === "tag_deleted") void vocabulary.refetch(); },
    });
  };
  return (
    <Dialog.Root open onOpenChange={open => { if (!open) dismiss(); }}>
      <Dialog.Portal>
        <Dialog.Overlay className="detail-dialog-overlay" />
        <Dialog.Content className="detail-dialog" onEscapeKeyDown={event => { if (busy) event.preventDefault(); }} onPointerDownOutside={event => { if (busy) event.preventDefault(); }}>
          <Dialog.Title className="detail-dialog-title">标签管理</Dialog.Title>
          <Dialog.Description className="detail-dialog-desc">点选标签挂到本文件，保存时整体替换当前标签。</Dialog.Description>
          <ErrorNotice error={vocabulary.error} stale={!!vocabulary.data} retry={() => void vocabulary.refetch()} />
          <div className="detail-dialog-body" role="group" aria-label="标签候选">
            {candidates.map(tag => (
              <button key={tag} type="button" className={`pill${selected.includes(tag) ? " active" : ""}`} disabled={busy} aria-pressed={selected.includes(tag)} onClick={() => setSelected(values => values.includes(tag) ? values.filter(value => value !== tag) : [...values, tag])}>{tag}</button>
            ))}
            {vocabulary.isPending ? <Loading text="标签加载中…" /> : !candidates.length && !vocabulary.isError ? <p className="grid-empty">暂无标签</p> : null}
            {vocabulary.hasNextPage && <button className="pill" disabled={vocabulary.isFetchingNextPage} onClick={() => void vocabulary.fetchNextPage()}>更多标签</button>}
          </div>
          <div className="tag-newrow">
            <input className="tag-input" aria-label="新标签名称" value={draft} maxLength={100} disabled={busy} placeholder="新建标签，回车确认" onChange={event => setDraft(event.target.value)} onKeyDown={event => { if (event.key === "Enter" && !event.nativeEvent.isComposing) { event.preventDefault(); create(); } }} />
            <button className="confirm-btn confirm-btn--cancel" disabled={busy || !draft.trim()} onClick={create}>新建</button>
          </div>
          <ErrorNotice error={createTag.error} />
          <ErrorNotice error={actions.tags.error} />
          <div className="detail-dialog-foot">
            <button className="confirm-btn confirm-btn--cancel" disabled={busy} onClick={dismiss}>取消</button>
            <button className="confirm-btn confirm-btn--primary" aria-label="保存标签" disabled={busy || !dirty || !vocabulary.data} onClick={save}>保存</button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
      <UnsavedGuard dirty={dirty || !!draft.trim()} />
      {discard && <ConfirmDialog title="放弃标签修改？" onCancel={() => setDiscard(false)} onConfirm={close}><p>尚未保存的标签草稿将被放弃。</p></ConfirmDialog>}
    </Dialog.Root>
  );
}
