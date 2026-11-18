import { useRef, useState } from "react";
import type { Schema } from "../data/client";
import { useAuthorUpdates, useFeedActions } from "../data/author-updates";
import { ErrorNotice, Loading } from "../components/Feedback";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { UnsavedGuard } from "../lib/navigation";
import { newId } from "../lib/id";
import { dateTime } from "../lib/format";
import { AuthorAvatar, ProfileLink } from "./ProfilePresentation";

export function SavedUpdates({ id }: { id: string }) {
  const query = useAuthorUpdates(id);
  const { check } = useFeedActions(id);
  const attempt = useRef({ revision: -1, key: newId() });
  const feed = query.data;
  return <>
    <ErrorNotice error={query.error} stale={!!feed} retry={() => void query.refetch()} />
    {query.isPending ? <p role="status">正在读取发布来源…</p> : feed?.trackUpdates && feed.feedUrl ? <>
      <p>来自已配置的作品发布订阅，点击标题查看原始发布页。</p>
      {!!feed.items.length ? <ul>{feed.items.map(item => <li key={item.id}><ProfileLink value={item.url}>{item.title}</ProfileLink>{item.publishedAt && <span>{dateTime(item.publishedAt)}</span>}</li>)}</ul> : <p>{feed.checkedAt ? "暂无已获取的作品发布记录。" : "作品订阅尚未检查。"}</p>}
      <button className="pill" disabled={check.isPending || query.isError} onClick={() => {
        if (attempt.current.revision !== feed.revision || check.isSuccess) attempt.current = { revision: feed.revision, key: newId() };
        check.mutate(attempt.current, { onError: () => void query.refetch() });
      }}>{check.isPending ? "检查中…" : "检查作品订阅"}</button>
      <ErrorNotice error={check.error} />
    </> : feed && <p>尚未接入可用的作品发布订阅，可前往作者发布页查看新作。</p>}
  </>;
}
export function SubscriptionEditor({ id, name, avatar, verified, close }: { id: string; name: string; avatar?: Schema<"AuthorProfileAvatar">; verified: boolean; close: () => void }) {
  const query = useAuthorUpdates(id);
  if (!query.data) return query.isPending ? <Loading /> : <ErrorNotice error={query.error} retry={() => void query.refetch()} />;
  return <FeedDraft initial={query.data} name={name} avatar={avatar} verified={verified} close={close} reload={() => query.refetch()} />;
}
function FeedDraft({ initial, name, avatar, verified, close, reload }: { initial: Schema<"AuthorUpdates">; name: string; avatar?: Schema<"AuthorProfileAvatar">; verified: boolean; close: () => void; reload: () => Promise<{ data?: Schema<"AuthorUpdates">; error: unknown }> }) {
  const [base, setBase] = useState(initial);
  const [feedUrl, setUrl] = useState(initial.feedUrl);
  const [enabled, setEnabled] = useState(initial.trackUpdates);
  const [discard, setDiscard] = useState(false);
  const [comparison, setComparison] = useState<Schema<"AuthorUpdates">>();
  const [readError, setReadError] = useState<unknown>();
  const { save } = useFeedActions(initial.authorId);
  const attempt = useRef({ signature: "", key: newId() });
  const dirty = base.feedUrl !== feedUrl || base.trackUpdates !== enabled;
  const eligible = verified && feedUrl.trim().startsWith("https://");
  const back = () => dirty ? setDiscard(true) : close();
  return <section className="creator-editor" aria-label="编辑作品订阅"><div className="creator-editor-heading"><div className="creator-editor-person"><AuthorAvatar className="creator-profile-avatar" name={name} avatar={avatar} /><div><h3>{name}</h3><p>作品订阅</p></div></div><button type="button" className="pill" disabled={save.isPending} onClick={back}>返回资料</button></div>
    <form onSubmit={event => {
      event.preventDefault(); if (!dirty || save.isPending || (enabled && !eligible)) return;
      const body = { expectedRevision: base.revision, feedUrl: feedUrl.trim(), trackUpdates: enabled };
      const signature = JSON.stringify(body); if (attempt.current.signature !== signature) attempt.current = { signature, key: newId() };
      save.mutate({ body, key: attempt.current.key }, { onSuccess: close });
    }}>
      <fieldset className="creator-editor-fields" disabled={save.isPending}><label className="settings-field"><span>作品发布订阅地址（RSS / Atom）</span><input type="url" maxLength={4096} placeholder="https://…" value={feedUrl} onChange={event => setUrl(event.target.value)} /></label>
        <label className="settings-switch"><input type="checkbox" checked={enabled} disabled={!enabled && !eligible} onChange={event => setEnabled(event.target.checked)} />检查作品订阅更新</label><p className="creator-profile-note">用于已核实作者的作品发布订阅。开启后可在「作者新作」主动检查。</p></fieldset>
      <ErrorNotice error={save.error ?? readError} />
      {save.isError && <button type="button" className="pill" onClick={async () => { const latest = await reload(); if (latest.error) setReadError(latest.error); else setComparison(latest.data); }}>读取当前订阅并对照</button>}
      {comparison && <div className="manage-notice"><p>当前订阅：{comparison.feedUrl || "未配置"} · {comparison.trackUpdates ? "已开启" : "已关闭"}</p><button type="button" className="pill" onClick={() => { setBase(comparison); setComparison(undefined); save.reset(); }}>已核对，保留草稿继续</button></div>}
      <div className="creator-editor-footer"><span>{dirty ? "有未保存的修改" : "已与当前资料一致"}</span><div className="creator-editor-buttons"><button type="button" className="pill" disabled={save.isPending} onClick={back}>取消</button><button type="submit" className="save-btn" disabled={!dirty || save.isPending || (enabled && !eligible)}>保存资料</button></div></div>
    </form>
    {discard && <ConfirmDialog title="放弃未保存修改？" confirmLabel="放弃修改" onCancel={() => setDiscard(false)} onConfirm={close}><p>这组资料仍有草稿。</p></ConfirmDialog>}
    <UnsavedGuard dirty={dirty || save.isPending} />
  </section>;
}
