import { createContext, useContext, useEffect, useMemo, useState, useSyncExternalStore } from "react";
import type { ReactNode } from "react";
import { api, read } from "./client";
import { createCheckpoint, downloadArchiveStream, getBackup, requestExport } from "./backups";
import { archiveOriginal, resumeRotation, rotate, readGeneration, verifyGeneration, pickFolder, folderLock, releaseMaterials, stageMaterials } from "./maintenance-files";
import type { Directory, Materials } from "./maintenance-files";
import { localRead, localWrite } from "./storage";
import { useSession } from "./session";
import { newId } from "../lib/id";
import type { UploadArchive } from "./upload-queue";

type Job = { createKey: string; exportKey: string; id?: string; revision?: string; materials?: Materials };
export type FolderBinding = { id: string; token: string; root: Directory; job?: Job };
type FolderState = { loading: boolean; binding?: FolderBinding; busy: boolean; status?: string; error?: unknown };
const initial: FolderState = { loading: true, busy: false };

class MaintenanceFolder {
  private subscribers = new Set<() => void>();
  private value: FolderState = initial;
  private controller = new AbortController();
  private timer?: number;
  constructor(readonly scope: string, readonly instanceId: string) {}
  subscribe = (listener: () => void) => { this.subscribers.add(listener); return () => { this.subscribers.delete(listener); }; };
  snapshot = () => this.value;
  private update(patch: Partial<FolderState>) {
    if (this.controller.signal.aborted) return;
    this.value = { ...this.value, ...patch }; this.subscribers.forEach(listener => listener());
  }
  async start() {
    try {
      const binding = await localRead<FolderBinding>("maintenance_folders", this.scope);
      this.update({ binding, loading: false }); this.schedule(0);
    } catch (error) { this.update({ error, loading: false }); }
  }
  private schedule(delay: number) {
    window.clearTimeout(this.timer);
    if (this.controller.signal.aborted || !this.value.binding) return;
    this.timer = window.setTimeout(() => {
      if (document.visibilityState === "visible" && navigator.onLine) void this.sync(false);
      else this.schedule(60000);
    }, delay);
  }
  private async store(binding: FolderBinding) {
    this.controller.signal.throwIfAborted();
    await localWrite("maintenance_folders", binding); this.update({ binding });
  }
  async choose() {
    if (this.value.busy) return;
    // The picker and permission prompt must start in the user gesture.
    this.update({ busy: true, error: undefined });
    try {
      const root = await pickFolder();
      this.controller.signal.throwIfAborted();
      await folderLock(async () => {
        const old = await localRead<FolderBinding>("maintenance_folders", this.scope);
        if (old && await old.root.isSameEntry(root)) { this.update({ binding: old }); return; }
        const current = await readGeneration(root, "当前");
        if (current && current.instanceId !== this.instanceId) throw new Error("此目录含另一影库的资料，请选择本影库的目录。");
        await this.store({ id: this.scope, token: newId(), root });
      }, this.controller.signal);
    } catch (error) {
      this.update({ busy: false, ...(error instanceof DOMException && error.name === "AbortError" ? {} : { error }) }); return;
    }
    this.update({ busy: false }); await this.sync(false);
  }
  async sync(manual: boolean) {
    const selected = this.value.binding;
    if (!selected || this.value.busy || this.controller.signal.aborted) return;
    const signal = this.controller.signal;
    this.update({ busy: true, error: undefined });
    try {
      const permission = manual ? await selected.root.requestPermission({ mode: "readwrite" }) : await selected.root.queryPermission({ mode: "readwrite" });
      if (permission !== "granted") { this.update({ status: "请点击更新全部归档，重新授权维护目录。" }); return; }
      await folderLock(async () => {
        let binding = await localRead<FolderBinding>("maintenance_folders", this.scope);
        if (!binding || binding.token !== selected.token) { this.update({ binding, status: "维护目录绑定已变更。" }); return; }
        const root = binding.root;
        const anchor = () => read(api.GET("/backups/change-state", { signal }));
        const resumed = await resumeRotation(root, this.instanceId, downloadArchiveStream, signal);
        if (resumed && binding.job?.id === resumed.backupId) { binding = { ...binding, job: undefined }; await this.store(binding); }
        const state = await anchor();
        if (state.instanceId !== this.instanceId) throw new Error("服务器身份发生变化，已停止写回。");
        const current = await readGeneration(root, "当前");
        if (current && current.instanceId !== state.instanceId) throw new Error("维护目录属于另一影库。");
        if (!binding.job && current?.businessRevision === state.businessRevision) {
          if (manual) await verifyGeneration(root, "当前", current, signal);
          this.update({ status: "当前资料已是最新一代。" }); return;
        }
        const job: Job = binding.job ?? { createKey: newId(), exportKey: newId() };
        if (!binding.job) { binding = { ...binding, job }; await this.store(binding); }
        const checkpoint = job.id ? await getBackup(job.id, signal) : await createCheckpoint(job.createKey, signal);
        if (!checkpoint.businessRevision) throw new Error("快照未返回业务修订，不能自动轮换。");
        if (!job.id) { job.id = checkpoint.id; job.revision = checkpoint.businessRevision; await this.store(binding); }
        if (!job.materials) {
          if (state.businessRevision !== checkpoint.businessRevision) {
            await this.store({ ...binding, job: undefined }); this.update({ status: "资料已变化，将捕获新的统一快照。" }); return;
          }
          const master = await read(api.GET("/author-lists/master", { parseAs: "stream", signal }));
          const report = await read(api.GET("/backups/{id}/author-documents", { params: { path: { id: checkpoint.id } }, parseAs: "stream", signal }));
          if (!master || !report) throw new Error("作者资料正文缺失，已保留原代次。");
          const materials = await stageMaterials(root, master, report, signal);
          const after = await anchor();
          if (after.instanceId !== this.instanceId || after.businessRevision !== checkpoint.businessRevision) {
            await releaseMaterials(root, materials, signal);
            await this.store({ ...binding, job: undefined }); this.update({ status: "读取作者总表时资料变化，将重新取快照。" }); return;
          }
          job.materials = materials; await this.store(binding);
        }
        let backup = checkpoint;
        if (["failed", "interrupted"].includes(backup.exportState ?? "")) {
          if (!manual) throw new Error("服务器归档中断或失败，点击更新全部归档重试。");
          job.exportKey = newId(); await this.store(binding);
          backup = await requestExport(job.id!, job.exportKey, signal);
        } else if (backup.exportState === "not_started") backup = await requestExport(job.id!, job.exportKey, signal);
        if (backup.exportState === "queued" || backup.exportState === "running") {
          this.update({ status: "服务器正在准备完整归档（" + (backup.exportedFiles ?? 0) + "/" + (backup.totalFiles ?? "—") + "）。" }); return;
        }
        if (backup.exportState !== "completed" || !backup.archiveHash) throw new Error("完整归档不可用：" + (backup.exportError || backup.exportState || "未返回状态"));
        this.update({ status: "正在核对旧代并写入维护目录…" });
        await rotate(root, { instanceId: state.instanceId, businessRevision: checkpoint.businessRevision, backupId: backup.id }, backup.archiveHash, job.materials, downloadArchiveStream, signal);
        await this.store({ ...binding, job: undefined }); this.update({ status: "当前备份与作者资料已更新。" });
      }, signal);
    } catch (error) { if (!signal.aborted) this.update({ error }); }
    finally {
      this.update({ busy: false });
      if (!this.value.error) this.schedule(this.value.binding?.job ? 3000 : 60000);
    }
  }
  dispose() { this.controller.abort(); window.clearTimeout(this.timer); }
}
const Context = createContext<MaintenanceFolder | null>(null);
function FolderOwner({ scope, instanceId, children }: { scope: string; instanceId: string; children: ReactNode }) {
  const [owner] = useState(() => new MaintenanceFolder(scope, instanceId));
  useEffect(() => { void owner.start(); return () => owner.dispose(); }, [owner]);
  return <Context.Provider value={owner}>{children}</Context.Provider>;
}
export function MaintenanceFolderProvider({ children }: { children: ReactNode }) {
  const { instance, session } = useSession();
  const scope = JSON.stringify([instance.id, session.id]);
  return <FolderOwner key={scope} scope={scope} instanceId={instance.id}>{children}</FolderOwner>;
}
export function useMaintenanceFolder() {
  const owner = useContext(Context);
  if (!owner) throw new Error("维护目录需在已登录会话内使用。");
  const state = useSyncExternalStore(owner.subscribe, owner.snapshot);
  return { ...state, choose: () => owner.choose(), sync: () => owner.sync(true), instanceId: owner.instanceId };
}
export function useUploadArchive(): UploadArchive {
  const owner = useContext(Context)!;
  return useMemo(() => ({
    capture: async () => (await localRead<FolderBinding>("maintenance_folders", owner.scope))?.token,
    write: async (token, entry, file, signal) => folderLock(async () => {
      const binding = await localRead<FolderBinding>("maintenance_folders", owner.scope);
      if (!binding || binding.token !== token) throw new Error("维护目录已更换，旧批次未写入新目录。");
      if (await binding.root.queryPermission({ mode: "readwrite" }) !== "granted") throw new Error("请先在资料备份与归档中重新授权原目录。");
      if (!entry.digest) throw new Error("原始文件的核验摘要缺失。");
      return archiveOriginal(binding.root, entry.libraryName, entry.request.relativeDirectory ?? "", file, entry.digest, signal);
    }, signal),
  }), [owner]);
}
