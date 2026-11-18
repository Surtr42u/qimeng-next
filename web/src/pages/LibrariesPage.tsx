import { useRef, useState } from "react";
import { Link } from "react-router-dom";
import { FolderOpen, Plus, RefreshCw } from "lucide-react";
import { useLibraries } from "../data/manage";
import { useRegisterLibrary, useLibraryLocation, useRemoveLibrary } from "../data/libraries";
import { useStartScan } from "../data/maintenance";
import { useUpdateLibrary } from "../data/catalog";
import type { Schema } from "../data/client";
import { ManagePageHeader } from "../components/LegacyManageHeader";
import { Select, SelectContent, SelectItem, SelectTrigger } from "../components/LegacySelect";
import { Switch } from "../components/LegacySwitch";
import { ErrorNotice, Loading } from "../components/Feedback";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { UnsavedGuard } from "../lib/navigation";
import { newId } from "../lib/id";

export function LibrariesPage() {
  const query = useLibraries(true);
  const register = useRegisterLibrary();
  const retryScan = useStartScan();
  const [name, setName] = useState("");
  const [rootPath, setRootPath] = useState("");
  const [kind, setKind] = useState<"regular" | "cos">("regular");
  const [notice, setNotice] = useState("");
  const [scanRetry, setScanRetry] = useState<{ library: Schema<"Library">; key: string; error: unknown }>();
  const blocked = register.isPending || retryScan.isPending || query.isPending || query.isError;
  return (
    <div className="page manage-workspace" id="page-maintenance-files-libraries">
      <ManagePageHeader title="库管理" description="登记服务器文件夹；临时不看请停用，移除登记会清除资产关联" actions={<a className="manage-text-link" href="#register-library"><Plus aria-hidden />添加媒体库</a>} />
      <ErrorNotice error={query.error} stale={!!query.data} retry={() => void query.refetch()} />
      {notice && <p className="manage-notice" role="status">{notice}</p>}
      {scanRetry && <div className="manage-notice manage-notice--warning" role="alert"><span>「{scanRetry.library.name}」已注册，扫描尚未确认受理。无需重复注册。</span><ErrorNotice error={retryScan.error || scanRetry.error} /><button className="pill" type="button" disabled={blocked} onClick={() => retryScan.mutate({ id: scanRetry.library.id, revision: scanRetry.library.revision, key: scanRetry.key }, { onSuccess: () => { setScanRetry(undefined); setNotice("扫描请求已受理，结果请查看任务报告。"); } })}>重试扫描</button></div>}
      {query.isPending ? <div className="settings-card"><p className="rank-note">正在读取媒体库…</p></div> : (
        <div className="manage-library-list">
          {query.data?.items.map(library => <LibraryCard key={library.id} library={library} blocked={blocked} onRemove={() => { setScanRetry(current => current?.library.id === library.id ? undefined : current); setNotice("已移除「" + library.name + "」的登记，磁盘原件保留。"); }} />)}
          {!query.isError && !query.data?.items.length && <div className="settings-card manage-empty"><FolderOpen aria-hidden /><h3>先登记一个媒体文件夹</h3><p>填入服务器可访问的目录；登记成功后会申请扫描。</p><a href="#register-library">添加第一个库 →</a></div>}
        </div>
      )}
      <form className="settings-card manage-registration" id="register-library" onSubmit={event => {
        event.preventDefault();
        if (blocked || !name.trim() || !rootPath.trim()) return;
        register.mutate({ name: name.trim(), rootPath: rootPath.trim(), kind }, { onSuccess: result => {
          setName(""); setRootPath(""); setKind("regular");
          setScanRetry(result.scanError ? { library: result.library, key: result.scanKey, error: result.scanError } : undefined);
          setNotice(result.scanError ? `「${result.library.name}」已注册，扫描未触发。` : `「${result.library.name}」已注册，扫描请求已受理。`);
        } });
      }}>
        <div className="manage-group-head"><h3>添加媒体库</h3><p>填写服务器上的文件夹路径，不是当前浏览器的本地目录</p></div>
        <div className="settings-grid">
          <label className="settings-field"><span>库名称</span><input aria-label="库名称" required disabled={register.isPending} maxLength={120} value={name} onChange={event => setName(event.target.value)} placeholder="例如：图集" /></label>
          <label className="settings-field"><span>服务器根路径</span><input aria-label="服务器根路径" required disabled={register.isPending} maxLength={4096} value={rootPath} onChange={event => setRootPath(event.target.value)} placeholder="例如 D:\Media\Photos 或 /media/photos" /><small>目录必须已存在且在允许范围内，不能位于服务数据目录。</small></label>
          <label className="settings-field"><span>库类型</span><Select value={kind} disabled={register.isPending} onValueChange={value => setKind(value === "cos" ? "cos" : "regular")}><SelectTrigger aria-label="库类型" /><SelectContent><SelectItem value="regular">常规</SelectItem><SelectItem value="cos">COS 作者库</SelectItem></SelectContent></Select><small>COS 目录按「作者/作品/文件」组织。</small></label>
        </div>
        <ErrorNotice error={register.error} />
        <div className="settings-actions"><button className="save-btn" type="submit" disabled={blocked || !name.trim() || !rootPath.trim()}>{register.isPending ? "登记并申请扫描中…" : "注册并扫描"}</button><span className="save-tip">扫描是后台任务，受理成功不代表扫描完成。</span></div>
      </form>
      <UnsavedGuard dirty={!!name || !!rootPath || register.isPending} />
    </div>
  );
}

function LibraryCard({ library, blocked, onRemove }: { library: Schema<"Library">; blocked: boolean; onRemove: () => void }) {
  const location = useLibraryLocation(library.id);
  const scan = useStartScan();
  const update = useUpdateLibrary();
  const remove = useRemoveLibrary(onRemove);
  const [removing, setRemoving] = useState<{ revision: number; count: number; key: string }>();
  const [disable, setDisable] = useState<{ revision: number; key: string }>();
  const attempt = useRef({ revision: library.revision, key: newId() });
  const busy = blocked || scan.isPending || update.isPending || remove.isPending;
  const running = library.scanState === "scanning";
  const abnormal = library.scanState === "error";
  const state = running ? "扫描中" : abnormal ? "扫描异常" : "空闲";
  const setEnabled = (enabled: boolean, revision: number, key: string) => update.mutate({ id: library.id, body: { expectedRevision: revision, enabled }, key }, { onSuccess: () => setDisable(undefined) });
  return (
    <section className="settings-card manage-library">
      <div className="manage-library-head">
        <span className="manage-library-icon"><FolderOpen aria-hidden /></span>
        <div><h3>{library.name}</h3><span className="rank-note">{library.kind === "cos" ? "COS 作者库" : "常规库"} · {library.fileCount} 个文件</span></div>
        {library.latestScanId ? <Link className={`manage-state${abnormal ? " manage-state--warning" : ""}`} to={`/manage/scans/${encodeURIComponent(library.latestScanId)}`} title="查看扫描报告">{state}</Link> : <span className="manage-state">{state}</span>}
      </div>
      <p className="manage-path">{location.data?.rootPath ?? (location.isPending ? "路径读取中…" : "路径未读取")}</p>
      <ErrorNotice error={location.error} retry={() => void location.refetch()} />
      <div className="manage-library-actions">
        <label className="settings-switch"><Switch aria-label={`启用媒体库 ${library.name}`} checked={library.enabled} disabled={busy} onCheckedChange={checked => checked ? setEnabled(true, library.revision, newId()) : setDisable({ revision: library.revision, key: newId() })} /><span>{library.enabled ? "已启用" : "已停用"}</span></label>
        <div>
          <button className="pill" type="button" disabled={busy || running || !library.enabled} onClick={() => {
            if (attempt.current.revision !== library.revision || scan.isSuccess) attempt.current = { revision: library.revision, key: newId() };
            scan.mutate({ id: library.id, revision: library.revision, key: attempt.current.key });
          }}><RefreshCw aria-hidden />{scan.isPending ? "提交中…" : "重新扫描"}</button>
          <button className="pill" type="button" disabled={busy} onClick={() => { remove.reset(); setRemoving({ revision: library.revision, count: library.fileCount, key: newId() }); }}>移除库登记</button>
        </div>
      </div>
      <ErrorNotice error={scan.error} />
      <ErrorNotice error={update.error} />
      {removing && <ConfirmDialog title={"移除库「" + library.name + "」的登记？"} confirmLabel="确认移除登记" danger busy={remove.isPending} onCancel={() => setRemoving(undefined)} onConfirm={() => remove.mutate({ id: library.id, revision: removing.revision, key: removing.key })}><p>将移除该库 {removing.count} 个文件的登记及关联，磁盘原件和历史事件保留。只想暂时隐藏时可停用媒体库。</p><ErrorNotice error={remove.error} /></ConfirmDialog>}
      {disable && <ConfirmDialog title={`停用「${library.name}」？`} confirmLabel="确认停用" busy={update.isPending} onCancel={() => setDisable(undefined)} onConfirm={() => setEnabled(false, disable.revision, disable.key)}><p>该库将从浏览、搜索和推荐中隐藏，文件与记录保留，可随时重新启用。</p><ErrorNotice error={update.error} /></ConfirmDialog>}
    </section>
  );
}
