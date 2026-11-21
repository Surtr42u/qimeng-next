import { useEffect, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { FolderOpen } from "lucide-react";
import { useBackups, usePortableExport, backupDownloadURL } from "../data/backups";
import { useMaintenanceFolder } from "../data/maintenance-folder";
import { folderSupported, recoveryFile, folderAuthorLists, folderLock } from "../data/maintenance-files";
import { postPortableImport } from "../data/portable-import";
import type { PortableImportReport, PortableMediaSample } from "../data/portable-import";
import { ManagePageHeader } from "../components/LegacyManageHeader";
import { ErrorNotice } from "../components/Feedback";
import { AuthorListsCard } from "./AuthorListsCard";
import { NativeDeliveryCard } from "./NativeDeliveryCard";
import { bytes, dateTime } from "../lib/format";
import {
  issueLabel,
  libraryMissReason,
  mediaMatchRows,
  mediaReasonLabel,
  reportHeadline,
  sampleLibraryLabel,
  skippedSectionLabel,
  skippedSectionReason,
  writeRows,
} from "../lib/portable-import";
import { newId } from "../lib/id";
import "./BackupPage.css";

export function BackupPage() {
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") ?? "backup";
  const folder = useMaintenanceFolder();
  return <div className="page manage-workspace" id="page-maintenance-files-backup">
    <ManagePageHeader title="资料备份与归档" description="一个目录管理作者资料、记录备份和上传归档" />
    <section className="rank-card maintenance-folder-card" aria-label="维护根目录">
      <div className="rank-head"><h3><FolderOpen aria-hidden /> 维护根目录</h3><span className="rank-note">电脑本地目录</span></div>
      <p>{folder.binding?.root.name ?? "选择一个文件夹，统一保存作者资料、备份和上传归档。"}</p>
      <div className="maintenance-folder-layout">
        <span>当前/作者资料 <small>作者主表 · 双语简介 · 来源与作品清单</small></span>
        <span>当前/备份 <small>Next 记录备份与每代独立摘要</small></span>
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
    } : undefined} /> : tab === "archive" ? <section className="rank-card"><h3>上传归档</h3><p>{folder.binding ? folder.binding.root.name + " / 上传归档" : "选择维护根目录后启用上传原件副本归档。"}</p><p className="rank-note">按库及目标目录归档。同名文件保留并提示，归档失败会单独显示在上传任务中。浏览器选择的原文件继续保留。</p><Link className="pill" to="/manage/upload">前往上传文件</Link></section> : <><RecoveryPanel /><PortableImportCard /></>}
  </div>;
}

// 旧版记录导入卡片：先预览匹配与写入，再确认落库。文件正文不进入查询缓存，
// 一次导入一个文件；重复提交由服务端按文件摘要去重。
function PortableImportCard() {
  const [file, setFile] = useState<File>();
  const [report, setReport] = useState<PortableImportReport>();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState<"preview" | "apply" | undefined>();
  const input = useRef<HTMLInputElement>(null);
  const active = useRef<AbortController | undefined>(undefined);
  useEffect(() => () => active.current?.abort(), []);
  const choose = (next: File | undefined) => {
    setFile(next);
    setReport(undefined);
    setError(undefined);
    if (input.current) input.current.value = "";
  };
  const run = async (dryRun: boolean) => {
    if (!file || busy) return;
    const controller = new AbortController();
    active.current = controller;
    setBusy(dryRun ? "preview" : "apply");
    setError(undefined);
    try {
      const next = await postPortableImport(file, dryRun, controller.signal);
      controller.signal.throwIfAborted();
      setReport(next);
    } catch (failure) {
      if (!controller.signal.aborted) setError(failure);
    } finally {
      active.current = undefined;
      if (!controller.signal.aborted) setBusy(undefined);
    }
  };
  const previewed = report?.dryRun === true;
  const canApply = Boolean(file) && previewed && !report?.alreadyImported && !busy;
  return <section className="rank-card" aria-labelledby="portable-import-title">
    <div className="rank-head"><h3 id="portable-import-title">导入旧版记录</h3><span className="rank-note">旧版绮梦 JSON · 合并到已扫描媒体库</span></div>
    <p className="rank-note">选择旧版导出的 <code>影库备份.json</code>。只写作者、标签、收藏、点赞、浏览事件与时间轴记录，不移动或删除任何文件。先预览匹配结果，再确认写入。</p>
    <input ref={input} type="file" accept=".json,application/json" hidden onChange={event => choose(event.target.files?.[0])} />
    <div className="settings-actions">
      <button type="button" className="pill" disabled={!!busy} onClick={() => input.current?.click()}>{file ? "更换旧版文件" : "选择旧版导出文件"}</button>
      <button type="button" className="pill" disabled={!file || !!busy} onClick={() => void run(true)}>{busy === "preview" ? "预览中…" : "预览导入"}</button>
      <button type="button" className="save-btn" disabled={!canApply} onClick={() => void run(false)}>{busy === "apply" ? "导入中…" : "确认导入"}</button>
    </div>
    {file && <p className="rank-note">{file.name} · {bytes(file.size)}</p>}
    <ErrorNotice error={error} />
    {report && <PortableReport report={report} />}
  </section>;
}

function PortableReport({ report }: { report: PortableImportReport }) {
  const missing = report.media.missingLibraries ?? [];
  const skipped = report.skipped ?? [];
  const warnings = report.warnings ?? [];
  const issues = report.txt.issues ?? [];
  const samples = report.samples ?? {};
  return <div className="form-stack">
    <p className="rank-note" role="status">{reportHeadline(report)}</p>
    {report.alreadyImported && <div className="manage-notice"><b>已导入</b><p>此文件此前已导入。重复提交不会增加标签、作者关联或资产修订。</p></div>}
    <details className="backup-disclosure" open>
      <summary>目录匹配</summary>
      <div style={{ overflowX: "auto" }}><table className="log-table">
        <thead><tr><th scope="col">项目</th><th scope="col">数量</th></tr></thead>
        <tbody>{mediaMatchRows(report).map(row => <tr key={row.key}><td>{row.label}</td><td>{row.value}{row.note ? <span className="rank-note"> · {row.note}</span> : null}</td></tr>)}</tbody>
      </table></div>
      {!!missing.length && <><p className="rank-note">未命中的旧库名</p><ul>{missing.map(item => <li key={item.libraryName + item.reason}>{item.libraryName} · {libraryMissReason(item.reason)} · {item.count} 条</li>)}</ul></>}
    </details>
    <details className="backup-disclosure" open={report.dryRun || report.changed}>
      <summary>{report.alreadyImported ? "首次导入结果" : report.dryRun ? "将写入" : "写入结果"}</summary>
      <div style={{ overflowX: "auto" }}><table className="log-table">
        <thead><tr><th scope="col">记录</th><th scope="col">数量</th><th scope="col">说明</th></tr></thead>
        <tbody>{writeRows(report).map(row => <tr key={row.key}><td>{row.label}</td><td>{row.value}</td><td>{row.note ?? "—"}</td></tr>)}</tbody>
      </table></div>
    </details>
    {!!skipped.length && <details className="backup-disclosure"><summary>未导入的旧段</summary>
      <div style={{ overflowX: "auto" }}><table className="log-table">
        <thead><tr><th scope="col">段</th><th scope="col">条数</th><th scope="col">原因</th></tr></thead>
        <tbody>{skipped.map(item => <tr key={item.section}><td>{skippedSectionLabel(item.section)}</td><td>{item.count}</td><td>{skippedSectionReason(item.reason)}</td></tr>)}</tbody>
      </table></div>
    </details>}
    {!!warnings.length && <details className="backup-disclosure" open><summary>提示</summary><ul>{warnings.map((warning, index) => <li key={index}>{warning}</li>)}</ul></details>}
    <SampleDetails title="未匹配样本" items={samples.unmatched ?? []} />
    <SampleDetails title="歧义样本" items={samples.ambiguous ?? []} />
    <SampleDetails title="多对一样本" items={samples.duplicateTargets ?? []} />
    {!!issues.length && <details className="backup-disclosure"><summary>作者 TXT 提示</summary><ul>{issues.map((code, index) => <li key={index}>{issueLabel(code)}</li>)}</ul></details>}
  </div>;
}

function SampleDetails({ title, items }: { title: string; items: PortableMediaSample[] }) {
  if (!items.length) return null;
  return <details className="backup-disclosure">
    <summary>{title}（{items.length}）</summary>
    <div style={{ overflowX: "auto" }}><table className="log-table">
      <thead><tr><th scope="col">旧库名</th><th scope="col">相对路径</th><th scope="col">文件名</th><th scope="col">原因</th><th scope="col">目标候选</th></tr></thead>
      <tbody>{items.map((item, index) => <tr key={item.recordKey + index}>
        <td>{sampleLibraryLabel(item)}</td>
        <td>{item.relativePath ?? "—"}</td>
        <td>{item.fileName}</td>
        <td>{mediaReasonLabel(item.reason)}</td>
        <td>{item.destination?.join("；") || "—"}</td>
      </tr>)}</tbody>
    </table></div>
  </details>;
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
      <div className="rank-head"><h3 id="backup-recovery-title">备份导入 / 导出</h3><span className="rank-note">Next 记录备份</span></div>
      <p className="rank-note">.qnext 包含数据库记录与作者资料，不含作品原件；恢复后原件需按原库路径放回。</p>
    </div>
    <div className="settings-actions">
      <button className="save-btn" disabled={busy} onClick={() => folder.binding ? void folder.sync() : exporter.mutate(undefined)}>{busy ? "处理中…" : folder.binding ? "更新全部归档" : "导出记录备份"}</button>
      {folder.binding && <><button className="pill" disabled={busy} onClick={() => void open("当前")}>恢复当前备份</button><button className="pill" disabled={busy} onClick={() => void open("上次")}>恢复上次备份</button></>}
    </div>
    <ErrorNotice error={error ?? exporter.error ?? backups.error} retry={backups.isError ? () => void backups.refetch() : undefined} />
    {exporter.data && <p className="rank-note" role="status">已受理记录备份，请在下方查看处理结果。</p>}
    {offline && <div className="manage-notice"><b>已核对独立归档</b><p>{offline.name}</p><code>{offline.hash}</code><p>将 .qnext 与对应 SHA-256 交给 qimeng-backup restore，在未存在的新目录恢复独立实例。</p></div>}
    {!!entries.length && <details className="backup-disclosure" open={!!exporter.data || undefined}><summary>服务器记录备份</summary><ul className="backup-archives">{entries.map(item => <li key={item.id}><span><b>{dateTime(item.createdAt)}</b> · {item.assetCount} 个文件 · {({ not_started: "快照已建立", queued: "已排队", running: "导出中", completed: "导出完成", failed: "导出失败", interrupted: "已中断", unavailable: "归档不可用" })[item.exportState ?? "unavailable"]}</span>
      {item.exportState === "completed" && item.archiveHash ? <div className="backup-archive-file"><a className="pill" href={backupDownloadURL(item.id)} download={"影库备份-" + item.id + ".qnext"}>下载 .qnext{item.archiveBytes !== undefined && " · " + bytes(item.archiveBytes)}</a><code>{item.archiveHash}</code></div> : ["failed", "interrupted", "not_started"].includes(item.exportState ?? "") ? <button className="pill" disabled={busy} onClick={() => { const key = keys.current.get(item.id) ?? newId(); keys.current.set(item.id, key); exporter.mutate({ id: item.id, key }, { onSuccess: () => keys.current.delete(item.id) }); }}>生成此记录备份</button> : null}
      {item.exportError && <p role="alert">{item.exportError}</p>}</li>)}</ul>{backups.hasNextPage && <button className="pill" disabled={backups.isFetchingNextPage} onClick={() => void backups.fetchNextPage()}>更多归档</button>}</details>}
    <NativeDeliveryCard />
  </section>;
}
