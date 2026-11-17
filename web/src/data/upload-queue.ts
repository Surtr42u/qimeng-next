import { api, read, operationHeaders } from './client';
import type { Schema } from './client';
import { fileDigest } from './hash';
import { localReadAll, localWrite } from './storage';
import { newId } from '../lib/id';
import { invalidateLibrary } from './query';

export type UploadPhase = 'waiting_file' | 'queued' | 'hashing' | 'uploading' | 'completing' | 'ingested' | 'staged' | 'duplicate' | 'failed' | 'paused' | 'cancelled';
export type QueueEntry = {
  id: string; scope: string; request: Schema<'UploadCreate'>; libraryName: string;
  name: string; size: number; modified: number; digest?: string; uploadId?: string;
  phase: UploadPhase; offset: number; hashProgress: number; assetId?: string;
  error?: string; createdAt: string;
};
type Snapshot = { entries: readonly QueueEntry[]; initialized: boolean; persistenceError?: string; preparing: boolean };
const terminal = (phase: UploadPhase) => ['ingested', 'staged', 'cancelled', 'duplicate'].includes(phase);
export const isQueueTerminal = terminal;

export class UploadQueue {
  private entries = new Map<string, QueueEntry>();
  private files = new Map<string, File>();
  private active = new Map<string, AbortController>();
  private listeners = new Set<() => void>();
  private snapshot: Snapshot = { entries: [], initialized: false, preparing: false };
  private writes: Promise<void> = Promise.resolve();
  private disposed = false;
  private preparingBatch?: { key: string; input: Schema<'BatchCreate'> };
  constructor(readonly scope: string, private maxChunkBytes: number, private maxItems: number, private concurrency = 2) { void this.restore(); }
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  getSnapshot = () => this.snapshot;
  private publish(patch: Partial<Snapshot> = {}) {
    this.snapshot = { ...this.snapshot, ...patch, entries: [...this.entries.values()] };
    this.listeners.forEach(listener => listener());
  }
  private async restore() {
    try {
      const stored = await localReadAll<QueueEntry>('uploads');
      if (this.disposed) return;
      for (const item of stored.filter(item => item.scope === this.scope)) {
        if (!this.entries.has(item.id)) this.entries.set(item.id, { ...item, phase: terminal(item.phase) ? item.phase : 'waiting_file' });
      }
      this.publish({ initialized: true });
    } catch { this.publish({ initialized: true, persistenceError: '任务恢复不可用。本次上传仅当前页面有效，刷新可能无法恢复；请保留源文件。' }); }
  }
  private persist(entry: QueueEntry) {
    const snapshot = structuredClone(entry);
    this.writes = this.writes.then(() => localWrite('uploads', snapshot)).catch(() => {
      this.publish({ persistenceError: '本地任务保存失败。服务器已接收的内容不会因此重传；刷新前请核对任务报告，并保留源文件。' });
    });
  }
  private change(id: string, patch: Partial<QueueEntry>, persist = true) {
    const current = this.entries.get(id);
    if (!current || this.disposed) return;
    const next = { ...current, ...patch }; this.entries.set(id, next);
    if (persist) this.persist(next);
    this.publish();
  }
  async retryStorage() {
    await this.writes;
    try {
      const previous = await localReadAll<QueueEntry>('uploads');
      for (const entry of previous) if (entry.scope === this.scope && !this.entries.has(entry.id)) this.entries.set(entry.id, { ...entry, phase: terminal(entry.phase) ? entry.phase : 'waiting_file' });
      for (const entry of this.entries.values()) await localWrite('uploads', entry);
      this.publish({ persistenceError: undefined });
    } catch { this.publish({ persistenceError: '仍无法保存本地任务，请保留源文件并核对任务报告。' }); }
  }
  async add(files: readonly File[], target: Omit<Schema<'UploadCreate'>, 'fileName' | 'size' | 'mimeType'>, libraryName: string) {
    if (!this.snapshot.initialized || this.snapshot.preparing) throw new Error('正在准备上传队列，请稍后再选。');
    if (!files.length) return;
    if (files.length > this.maxItems || this.entries.size + files.length > 500) throw new Error(`本批最多 ${this.maxItems} 项；队列记录最多 500 项，请先清理已结束记录。`);
    this.publish({ preparing: true });
    try {
      const batchInput = { libraryId: target.libraryId, mode: target.mode, label: `${files.length} 个文件 · ${new Date().toLocaleString('zh-CN')}` } satisfies Schema<'BatchCreate'>;
      const pending = this.preparingBatch;
      if (pending && (pending.input.libraryId !== target.libraryId || pending.input.mode !== target.mode)) throw new Error('上次批次创建未确认，请先使用相同目标重试。');
      const attempt = pending ?? { key: newId(), input: batchInput };
      this.preparingBatch = attempt;
      const batch = await read(api.POST('/batches', { params: { header: operationHeaders(attempt.key) }, body: attempt.input }));
      this.preparingBatch = undefined;
      for (const file of files) {
        const relative = file.webkitRelativePath;
        const directory = relative.includes('/') ? relative.slice(0, relative.lastIndexOf('/')) : '';
        const entry: QueueEntry = { id: newId(), scope: this.scope,
          request: { ...target, fileName: file.name, size: file.size, mimeType: file.type || 'application/octet-stream', sourceMtime: new Date(file.lastModified).toISOString(), batchId: batch.id,
            relativeDirectory: [target.relativeDirectory, directory].filter(Boolean).join('/') || undefined },
          libraryName, name: file.name, size: file.size, modified: file.lastModified, phase: 'queued', offset: 0, hashProgress: 0, createdAt: new Date().toISOString() };
        this.entries.set(entry.id, entry); this.files.set(entry.id, file); this.persist(entry);
      }
      this.publish(); this.pump();
    } finally { this.publish({ preparing: false }); }
  }
  private pump() {
    if (this.disposed || this.active.size >= this.concurrency) return;
    for (const entry of this.entries.values()) {
      if (this.active.size >= this.concurrency) break;
      const file = this.files.get(entry.id);
      if (entry.phase !== 'queued' || !file || this.active.has(entry.id)) continue;
      const controller = new AbortController(); this.active.set(entry.id, controller);
      void this.transfer(entry.id, file, controller.signal).finally(() => { this.active.delete(entry.id); this.pump(); });
    }
  }
  private async transfer(id: string, file: File, signal: AbortSignal) {
    try {
      let entry = this.entries.get(id)!;
      this.change(id, { phase: 'hashing', error: undefined });
      const digest = await fileDigest(file, signal, value => this.change(id, { hashProgress: value }, false));
      if (entry.digest && digest !== entry.digest) throw new Error('所选文件内容已改变。没有继续旧上传，请选择原文件或作为新任务添加。');
      this.change(id, { digest });
      entry = this.entries.get(id)!;
      let upload = entry.uploadId
        ? await read(api.GET('/uploads/{id}', { params: { path: { id: entry.uploadId } }, signal }))
        : await read(api.POST('/uploads', { params: { header: operationHeaders(entry.id) }, body: entry.request, signal }));
      this.change(id, { uploadId: upload.id, offset: upload.offset });
      if (upload.state === 'ingested' || upload.state === 'staged' || upload.state === 'duplicate') { this.finish(id, upload); return; }
      if (upload.state === 'cancelled') { this.change(id, { phase: 'cancelled' }); return; }
      this.change(id, { phase: 'uploading' });
      if (upload.offset < 0 || upload.offset > file.size) throw new Error('服务器返回的上传位置超出文件范围，已停止传输。');
      while (upload.offset < file.size) {
        const previousOffset = upload.offset;
        const chunk = file.slice(upload.offset, Math.min(file.size, upload.offset + this.maxChunkBytes));
        upload = await read(api.PUT('/uploads/{id}/content', { params: { path: { id: upload.id }, header: { 'Upload-Offset': upload.offset } }, body: chunk as unknown as string,
          bodySerializer: body => body as unknown as Blob, headers: { 'Content-Type': 'application/octet-stream' }, signal }));
        if (upload.offset <= previousOffset || upload.offset > file.size) throw new Error('服务器没有确认有效分片进度，已停止传输，请重试以重新核对。');
        this.change(id, { offset: upload.offset });
      }
      this.change(id, { phase: 'completing' });
      const result = await read(api.POST('/uploads/{id}/complete', { params: { path: { id: upload.id }, header: operationHeaders(`${entry.id}-complete`) }, signal }));
      this.finish(id, result.upload);
      void invalidateLibrary();
    } catch (error) {
      if (this.disposed || signal.aborted) return;
      this.change(id, { phase: 'failed', error: error instanceof Error ? error.message : '上传失败，尚未收到入库确认。' });
    }
  }
  private finish(id: string, upload: Schema<'Upload'>) {
    const phase: UploadPhase = upload.state === 'ingested' ? 'ingested' : upload.state === 'staged' ? 'staged' : upload.state === 'duplicate' ? 'duplicate' : 'failed';
    this.change(id, { phase, offset: upload.offset, assetId: upload.assetId, error: upload.errorMessage });
    if (terminal(phase)) this.files.delete(id);
  }
  retry(id: string, file?: File) {
    const entry = this.entries.get(id);
    if (!entry || terminal(entry.phase) || this.active.has(id)) return;
    if (file) {
      if (file.name !== entry.name || file.size !== entry.size || file.lastModified !== entry.modified) {
        this.change(id, { error: '名称、大小或修改时间不同，请选择最初的文件。' }); return;
      }
      this.files.set(id, file);
    }
    if (!this.files.has(id)) { this.change(id, { phase: 'waiting_file' }); return; }
    this.change(id, { phase: 'queued', error: undefined }); this.pump();
  }
  pause(id: string) { this.active.get(id)?.abort(); this.change(id, { phase: 'paused' }); }
  async cancel(id: string) {
    const entry = this.entries.get(id);
    if (!entry || terminal(entry.phase)) return;
    this.pause(id);
    try {
      const current = entry.uploadId
        ? await read(api.GET('/uploads/{id}', { params: { path: { id: entry.uploadId } } }))
        : entry.digest ? await read(api.POST('/uploads', { params: { header: operationHeaders(entry.id) }, body: entry.request })) : undefined;
      if (current && ['ingested', 'staged', 'duplicate'].includes(current.state)) { this.finish(id, current); return; }
      if (current) await read(api.DELETE('/uploads/{id}', { params: { path: { id: current.id } } }));
      this.change(id, { phase: 'cancelled', error: undefined }); this.files.delete(id);
    } catch (error) { this.change(id, { error: `${error instanceof Error ? error.message : '取消失败'}。请重试取消或继续上传以回读实际状态。` }); }
  }
  async clearFinished() {
    await this.writes;
    for (const entry of this.entries.values()) {
      if (!terminal(entry.phase)) continue;
      try { await localWrite('uploads', entry.id); this.entries.delete(entry.id); }
      catch { this.publish({ persistenceError: '清理本地记录失败；记录已保留，媒体不会被删除。' }); }
    }
    this.publish();
  }
  dispose() { this.disposed = true; for (const active of this.active.values()) active.abort(); this.files.clear(); }
}
