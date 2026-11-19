import { useRef, useState } from "react";
import { AuthorSuggestField } from "../components/AuthorSuggestField";
import { ErrorNotice } from "../components/Feedback";
import type { Schema } from "../data/client";
import { useAuthorLists } from "../data/author-lists";
import { useAuthorProfile } from "../data/author-profiles";
import { createPairingAttempt, registerDevice, useDeliveryReceipt, useDeviceDeliveries, usePublishDelivery } from "../data/native-delivery";
import { useTimeline } from "../data/catalog";
import { useAssets } from "../data/media";
import { useRemoteKey } from "../data/session";
import { dateTime, duration } from "../lib/format";

type Selection = Schema<"NativeDeliverySelection">;
type Chosen = { selection: Selection; label: string };
const itemKey = (item: Selection) => JSON.stringify([item.kind, item.sourceObjectId, item.block]);
const blockNames = { x: "X 身份", basic: "基本资料", accounts: "账号资料" };
const states = { pending: "等待设备领取", claimed: "设备已领取", applied: "已完成", partial: "部分完成", failed: "失败", expired: "已过期", cancelled: "已取消" };

function PairDevice({ device }: { device: string }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [secret, setSecret] = useState("");
  const attempt = useRef<ReturnType<typeof createPairingAttempt> | undefined>(undefined);
  const pair = async () => {
    if (!device || busy) return;
    setBusy(true); setError(undefined);
    const current = attempt.current ??= createPairingAttempt();
    try { await registerDevice(device, current); setSecret(current.secret); }
    catch (e) { setError(e); }
    finally { setBusy(false); }
  };
  return <details><summary>首次配对这台接收设备</summary>
    <p>在接收设备的「设备领取」中填写本服务地址、相同的设备标识和专用令牌。</p>
    {secret ? <><label className="settings-field">设备专用令牌<input readOnly type="text" autoComplete="off" value={secret} onFocus={e => e.target.select()} /></label>
      <button type="button" className="pill" onClick={() => { setSecret(""); attempt.current = undefined; }}>已保存，关闭令牌</button></>
      : <button type="button" className="pill" disabled={busy || !device} onClick={() => void pair()}>生成首次配对令牌</button>}
    <ErrorNotice error={error} />
  </details>;
}

function SourceChoices({ add }: { add: (item: Chosen) => void }) {
  const [author, setAuthor] = useState<Schema<"Author">>();
  const profile = useAuthorProfile(author?.id ?? "");
  const lists = useAuthorLists();
  return <>
    <AuthorSuggestField value={author} label="选择来源作者" onChoose={value => setAuthor(value ?? undefined)} />
    {author && profile.data?.blocks.filter(block => block.revision > 0).map(block =>
      <button key={block.block} type="button" className="pill" onClick={() => add({
        selection: { kind: "authorProfileBlock", sourceObjectId: author.id, block: block.block, expectedRevision: block.revision },
        label: author.name + " · " + blockNames[block.block],
      })}>加入{blockNames[block.block]}</button>)}
    {author && profile.isSuccess && !profile.data.blocks.some(block => block.revision > 0) && <p>这位作者还没有保存的资料块。</p>}
    <label className="settings-field">选择来源作者 TXT
      <select value="" onChange={e => {
        const list = lists.data?.pages.flatMap(page => page.items).find(entry => entry.id === e.target.value);
        if (list) add({ label: list.name, selection: { kind: "authorList", sourceObjectId: list.id, expectedRevision: list.revision } });
      }}><option value="">选择要加入的片段</option>{lists.data?.pages.flatMap(page => page.items).map(list => <option key={list.id} value={list.id}>{list.name}</option>)}</select>
    </label>
    {lists.hasNextPage && <button type="button" className="pill" onClick={() => void lists.fetchNextPage()}>更多片段</button>}
    <ErrorNotice error={profile.error ?? lists.error} />
  </>;
}

function TimelineChoices({ add }: { add: (item: Chosen) => void }) {
  const [search, setSearch] = useState("");
  const [asset, setAsset] = useState<Schema<"Asset">>();
  const [targetId, setTargetId] = useState("");
  const [version, setVersion] = useState("");
  const videos = useAssets({ q: search, mediaType: "video" });
  const marks = useTimeline(asset?.id ?? "", !!asset);
  const targetVersion = Number(version);
  const ready = !!targetId.trim() && Number.isSafeInteger(targetVersion) && targetVersion > 0;
  return <div className="settings-field">
    <label>搜索来源视频<input value={search} onChange={e => { setSearch(e.target.value); setAsset(undefined); }} /></label>
    <label>来源视频<select value={asset?.id ?? ""} onChange={e => setAsset(videos.data?.pages.flatMap(page => page.items).find(value => value.id === e.target.value))}>
      <option value="">选择视频</option>{videos.data?.pages.flatMap(page => page.items).map(value => <option value={value.id} key={value.id}>{value.relativePath}</option>)}</select></label>
    {videos.hasNextPage && <button className="pill" onClick={() => void videos.fetchNextPage()}>更多视频</button>}
    {asset && <>
      <p>从接收设备选中的视频读取以下两项。接收服务会核对两份原件的大小和摘要。</p>
      <label>目标视频 ID<input value={targetId} maxLength={128} onChange={e => setTargetId(e.target.value)} /></label>
      <label>目标内容版本<input type="number" min={1} step={1} value={version} onChange={e => setVersion(e.target.value)} /></label>
      {marks.data?.pages.flatMap(page => page.items).filter(mark => mark.contentVersion === asset.contentVersion).map(mark =>
        <button type="button" className="pill" key={mark.id} disabled={!ready} onClick={() => add({ label: asset.fileName + " · " + mark.label, selection: {
          kind: "timelineMark", sourceObjectId: mark.id, expectedRevision: mark.revision, sourceAssetId: asset.id,
          sourceContentVersion: asset.contentVersion, targetAssetId: targetId.trim(), targetContentVersion: targetVersion,
        } })}>{mark.label} · {duration(mark.positionMs)}</button>)}
      {marks.hasNextPage && <button className="pill" onClick={() => void marks.fetchNextPage()}>更多标记</button>}
      {marks.isSuccess && !marks.hasNextPage && !marks.data.pages.some(page => page.items.some(mark => mark.contentVersion === asset.contentVersion)) && <p>当前版本还没有时间轴标记。</p>}
    </>}
    <ErrorNotice error={videos.error ?? marks.error} />
  </div>;
}

export function NativeDeliveryCard() {
  const [open, setOpen] = useState(false);
  const [, instance, session] = useRemoteKey();
  return <details className="backup-disclosure" onToggle={e => { if (e.currentTarget.open) setOpen(true); }}>
    <summary>发送资料到另一台设备</summary>
    {open && <>
      <p className="rank-note">向已有影库发送部分资料时，可在下方选择接收设备和具体条目。</p>
      <DeliveryForm key={instance + "/" + session} />
    </>}
  </details>;
}

function DeliveryForm() {
  const [device, setDevice] = useState("");
  const [instance, setInstance] = useState("");
  const [timeline, setTimeline] = useState(false);
  const [chosen, setChosen] = useState<Chosen[]>([]);
  const [taskId, setTaskId] = useState("");
  const [historyDevice, setHistoryDevice] = useState("");
  const [notice, setNotice] = useState("");
  const publish = usePublishDelivery();
  const receipt = useDeliveryReceipt(taskId);
  const history = useDeviceDeliveries(historyDevice === device.trim() ? historyDevice : "");
  const task = receipt.data ?? publish.data;
  const add = (item: Chosen) => {
    const existing = chosen.find(entry => itemKey(entry.selection) === itemKey(item.selection));
    if (!existing && chosen.length >= 32) { setNotice("每个包最多可选 32 项。"); return; }
    setNotice("");
    setChosen(existing ? chosen.map(entry => entry === existing ? item : entry) : [...chosen, item]);
  };
  return <div className="form-stack">
    <label className="settings-field">目标设备标识<input value={device} maxLength={100} onChange={e => setDevice(e.target.value)} placeholder="与接收端登记的标识相同" /></label>
    <PairDevice key={device.trim()} device={device.trim()} />
    <details onToggle={e => { if (e.currentTarget.open) setHistoryDevice(device.trim()); }}><summary>查看这个设备的交付记录</summary>
      <button type="button" className="pill" disabled={!device.trim()} onClick={() => {
        if (historyDevice === device.trim()) void history.refetch(); else setHistoryDevice(device.trim());
      }}>读取交付记录</button>
      {historyDevice === device.trim() && history.isSuccess && <ul>{history.data.pages.flatMap(page => page.items).map(item =>
        <li key={item.id}><button type="button" className="pill" onClick={() => setTaskId(item.id)}>{dateTime(item.createdAt)} · {states[item.state]} · {item.id}</button></li>)}</ul>}
      {history.hasNextPage && <button type="button" className="pill" onClick={() => void history.fetchNextPage()}>更多记录</button>}
      <ErrorNotice error={history.error} />
    </details>
    <label className="settings-field">接收实例 ID<input value={instance} maxLength={128} onChange={e => {
      setInstance(e.target.value); setChosen(items => items.filter(item => item.selection.kind !== "timelineMark"));
    }} placeholder="从接收端的设备领取页复制" /></label>
    <SourceChoices add={add} />
    <details onToggle={e => setTimeline(e.currentTarget.open)}><summary>选择视频时间轴标记</summary>{timeline && <TimelineChoices add={add} />}</details>
    {!!chosen.length && <ul aria-label="待发送资料">{chosen.map(item => <li key={itemKey(item.selection)}>{item.label} · 修订 {item.selection.expectedRevision}{" "}
      <button type="button" className="pill" onClick={() => setChosen(chosen.filter(entry => itemKey(entry.selection) !== itemKey(item.selection)))}>移除</button></li>)}</ul>}
    {notice && <p role="status">{notice}</p>}
    <button type="button" className="save-btn" disabled={!device.trim() || !instance.trim() || !chosen.length || publish.isPending} onClick={() =>
      publish.mutate({ targetDeviceId: device.trim(), targetInstanceId: instance.trim(), items: chosen.map(item => item.selection) }, { onSuccess: value => setTaskId(value.id) })}>发送所选资料</button>
    <ErrorNotice error={publish.error ?? receipt.error} />
    {task && <div role="status"><b>{states[task.state]}</b> · 任务 <code>{task.id}</code>
      {task.segments[0]?.packageId && <p>包摘要 <code>{task.segments[0].packageId}</code></p>}
      {task.results?.map(result => <p key={result.kind}>接收结果 {result.state}{result.code && " · " + result.code}{result.receiptId && <> · 本机回执 <code>{result.receiptId}</code></>}</p>)}
    </div>}
  </div>;
}
