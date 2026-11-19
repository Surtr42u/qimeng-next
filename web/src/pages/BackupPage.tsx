import { useEffect, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { FolderOpen } from "lucide-react";
import { useBackups, usePortableExport, backupDownloadURL } from "../data/backups";
import { useMaintenanceFolder } from "../data/maintenance-folder";
import { folderSupported, recoveryFile, folderAuthorLists, folderLock } from "../data/maintenance-files";
import { ManagePageHeader } from "../components/LegacyManageHeader";
import { ErrorNotice } from "../components/Feedback";
import { AuthorListsCard } from "./AuthorListsCard";
import { NativeDeliveryCard } from "./NativeDeliveryCard";
import { bytes, dateTime } from "../lib/format";
import { newId } from "../lib/id";
import "./BackupPage.css";

export function BackupPage() {
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") ?? "backup";
  const folder = useMaintenanceFolder();
  return <div className="page manage-workspace" id="page-maintenance-files-backup">
    <ManagePageHeader title="资料备份与归档" description="一个目录管理作者资料、完整备份和上传归档" />
    <section className="rank-card maintenance-folder-card" aria-label="维护根目录">
      <div className="rank-head"><h3><FolderOpen aria-hidden /> 维护根目录</h3><span className="rank-note">电脑本地目录</span></div>
      <p>{folder.binding?.root.name ?? "选择一个文件夹，统一保存作者资料、备份和上传归档。"}</p>
      <div className="maintenance-folder-layout">
        <span>当前/作者资料 <small>作者主表 · 双语简介 · 来源与作品清单</small></span>
        <span>当前/备份 <small>完整 Next 归档与每代独立摘要</small></span>
        <span>上次 <small>内容变化前的上一代 · 当前与上次各自独立</small></span>
        <span>上传归档 <small>按库及目标目录保存原件副本</small></span>
      </div>
      {folderSupported() ? <button type="button" className="pill" disabled={folder.busy || folder.loading} onClick={() => void folder.choose()}>{folder.binding ? "更换维护根目录" : "选择根目录并生成资料"}</button> : <p className="rank-note">当前浏览器未提供目录读写，可使用下方文件下载与导入。支持目录授权的浏览器可开启统一目录。</p>}
      {folder.status && <p className="rank-note" role="status">{folder.status}</p>}<ErrorNotice error={folder.error} />
      <p className="rank-note">自动与手动共用两代备份。核对当前代后先提交为上次，再写新当前。独立摘要不符时暂停写回并保留文件。</p>
    </section>
    <nav className="maintenance-tabs" aria-label="资料维护任务">{[["backup", "备份恢复"], ["authors", "作者TXT"], ["archive", "上传归档"]].map(([value, label]) => <button key={value} className="pill" type="button" aria-pressed={tab === value} onClick={() => setParams({ tab: value! }, { replace: true })}>{label}</button>)}</nav>
    {tab === "authors" ? <AuthorListsCard folderFiles={folder.binding ? async () => {
      const root = folder.binding!.root;
      if (await root.requestPermission({ mode: "readwrite" }) !== "granted") throw new Error("维护目录尚未授权。");
      return folderLock(() => folderAuthorLists(root));
    } : undefined} /> : tab === "archive" ? <section className="rank-card"><h3>上传归档</h3><p>{folder.binding ? folder.binding.root.name + " / 上传归档" : "选择维护根目录后启用上传原件副本归档。"}</p><p className="rank-note">按库及目标目录归档。同名文件保留并提示，归档失败会单独显示在上传任务中。浏览器选择的原文件继续保留。</p><Link className="pill" to="/manage/upload">前往上传文件</Link></section> : <RecoveryPanel />}
  </div>;
}

function RecoveryPanel() {
  const folder = useMaintenanceFolder();
  const backups = useBackups();
  const exporter = usePortableExport();
  const [offline, setOffline] = useState<{ name: string; hash: string }>();
  const [error, setError] = useState<unknown>();
  const [checking, setChecking] = useState(false);
  const active = useRef<AbortController | undefined>(undefined);
  const keys = useRef(new Map<string, string>());
  useEffect(() => () => active.current?.abort(), []);
  const open = async (slot: "当前" | "上次") => {
    const root = folder.binding?.root;
    if (!root || active.current) return;
    const controller = new AbortController(); active.current = controller; setChecking(true); setError(undefined);
    try {
      if (await root.requestPermission({ mode: "readwrite" }) !== "granted") throw new Error("维护目录尚未授权。");
      const result = await folderLock(() => recoveryFile(root, slot, folder.instanceId, controller.signal), controller.signal);
      controller.signal.throwIfAborted();
      setOffline({ name: slot + "/备份/" + result.file.name, hash: result.hash });
    } catch (failure) { if (!controller.signal.aborted) setError(failure); }
    finally { active.current = undefined; if (!controller.signal.aborted) setChecking(false); }
  };
  const entries = backups.data?.pages.flatMap(page => page.items) ?? [];
  const busy = folder.loading || folder.busy || checking || exporter.isPending;
  return <section className="rank-card backup-recovery" aria-labelledby="backup-recovery-title">
    <div className="backup-intro">
      <div className="rank-head"><h3 id="backup-recovery-title">备份导入 / 导出</h3><span className="rank-note">Next 完整归档</span></div>
      <p className="rank-note">.qnext 包含原件与数据库，在新目录中恢复。</p>
    </div>
    <div className="settings-actions">
      <button className="save-btn" disabled={busy} onClick={() => folder.binding ? void folder.sync() : exporter.mutate(undefined)}>{busy ? "处理中…" : folder.binding ? "更新全部归档" : "导出全量备份"}</button>
      {folder.binding && <><button className="pill" disabled={busy} onClick={() => void open("当前")}>恢复当前备份</button><button className="pill" disabled={busy} onClick={() => void open("上次")}>恢复上次备份</button></>}
    </div>
    <ErrorNotice error={error ?? exporter.error ?? backups.error} retry={backups.isError ? () => void backups.refetch() : undefined} />
    {exporter.data && <p className="rank-note" role="status">已受理完整归档，请在下方查看处理结果。</p>}
    {offline && <div className="manage-notice"><b>已核对独立归档</b><p>{offline.name}</p><code>{offline.hash}</code><p>将 .qnext 与对应 SHA-256 交给 qimeng-backup restore，在未存在的新目录恢复独立实例。</p></div>}
    {!!entries.length && <details className="backup-disclosure" open={!!exporter.data || undefined}><summary>服务器完整归档</summary><ul className="backup-archives">{entries.map(item => <li key={item.id}><span><b>{dateTime(item.createdAt)}</b> · {item.assetCount} 个文件 · {({ not_started: "快照已建立", queued: "已排队", running: "导出中", completed: "导出完成", failed: "导出失败", interrupted: "已中断", unavailable: "归档不可用" })[item.exportState ?? "unavailable"]}</span>
      {item.exportState === "completed" && item.archiveHash ? <div className="backup-archive-file"><a className="pill" href={backupDownloadURL(item.id)} download={"影库备份-" + item.id + ".qnext"}>下载 .qnext{item.archiveBytes !== undefined && " · " + bytes(item.archiveBytes)}</a><code>{item.archiveHash}</code></div> : ["failed", "interrupted", "not_started"].includes(item.exportState ?? "") ? <button className="pill" disabled={busy} onClick={() => { const key = keys.current.get(item.id) ?? newId(); keys.current.set(item.id, key); exporter.mutate({ id: item.id, key }, { onSuccess: () => keys.current.delete(item.id) }); }}>生成此快照归档</button> : null}
      {item.exportError && <p role="alert">{item.exportError}</p>}</li>)}</ul>{backups.hasNextPage && <button className="pill" disabled={backups.isFetchingNextPage} onClick={() => void backups.fetchNextPage()}>更多归档</button>}</details>}
    <NativeDeliveryCard />
  </section>;
}
