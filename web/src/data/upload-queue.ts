import { api, read, operationHeaders } from './client';
import type { Schema } from './client';
import { fileDigest } from './hash';
import { localReadAll, localWrite, localWriteGroup } from './storage';
import { newId } from '../lib/id';
import { invalidateLibrary } from './query';

export type UploadPhase = 'waiting_file' | 'queued' | 'hashing' | 'uploading' | 'completing' | 'ingested' | 'staged' | 'duplicate' | 'failed' | 'paused' | 'cancelled';
export type QueueEntry = {
  id: string; scope: string; request: Schema<'UploadCreate'>; libraryName: string;
  name: string; size: number; modified: number; digest?: string; uploadId?: string;
  phase: UploadPhase; offset: number; hashProgress: number; assetId?: string;
  error?: string; createdAt: string; order?: number; waitingFor?: string; actualFileName?: string;
  batchPlanId?: string;
  preparationFailed?: boolean;
};
export type UploadBatchPreparation = {
  id: string; scope: string; input: Schema<'BatchCreate'>; inputs: Schema<'IntakeInput'>[];
  manifestKey: string; batchId?: string; manifestRevision?: number; fingerprint?: string; sealed: boolean;
};
type Snapshot = { entries: readonly QueueEntry[]; initialized: boolean; persistenceError?: string; preparing: boolean };
const terminal = (phase: UploadPhase) => ['ingested', 'staged', 'cancelled', 'duplicate'].includes(phase);
export const isQueueTerminal = terminal;
export class QueuedPreparationError extends Error {
  constructor(cause: unknown) {
    super(`原始输入清单尚未确认，请从已保留的队列项重试。${cause instanceof Error ? cause.message : '批次准备未完成。'}`, { cause });
    this.name = 'QueuedPreparationError';
  }
}

export class UploadQueue {
  private entries = new Map<string, QueueEntry>();
  private files = new Map<string, File>();
  private active = new Map<string, AbortController>();
  private listeners = new Set<() => void>();
  private snapshot: Snapshot = { entries: [], initialized: false, preparing: false };
  private writes: Promise<void> = Promise.resolve();
  private disposed = false;
  private batches = new Map<string, UploadBatchPreparation>();
  private preparing = new Map<string, Promise<void>>();
  private batchControllers = new Map<string, AbortController>();
  constructor(readonly scope: string, private maxChunkBytes: number, private maxItems: number, private concurrency = 2) { void this.restore(); }
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  getSnapshot = () => this.snapshot;
  private publish(patch: Partial<Snapshot> = {}) {
    this.snapshot = { ...this.snapshot, ...patch, entries: [...this.entries.values()].map(entry => ({ ...entry, waitingFor: entry.phase === 'queued' ? this.blockingEntry(entry)?.name : undefined })) };
    this.listeners.forEach(listener => listener());
  }
  private sortEntries() {
    this.entries = new Map([...this.entries].sort(([, left], [, right]) => (left.order ?? Date.parse(left.createdAt)) - (right.order ?? Date.parse(right.createdAt))));
  }
  private namingSequence(entry: QueueEntry) {
    if (entry.request.mode !== 'ordinary') return undefined;
    if (entry.request.collectionId) return `${entry.request.libraryId}:collection:${entry.request.collectionId}`;
    if (entry.request.family) return JSON.stringify([entry.request.libraryId, entry.request.relativeDirectory, entry.request.family]);
  }
  private blockingEntry(entry: QueueEntry) {
    const sequence = this.namingSequence(entry);
    if (!sequence) return undefined;
    for (const before of this.entries.values()) {
      if (before.id === entry.id) break;
      if (!terminal(before.phase) && this.namingSequence(before) === sequence) return before;
    }
  }
  private async restore() {
    try {
      const [stored, batches] = await Promise.all([localReadAll<QueueEntry>('uploads'), localReadAll<UploadBatchPreparation>('upload_batches')]);
      if (this.disposed) return;
      for (const item of stored.filter(item => item.scope === this.scope)) {
        if (!this.entries.has(item.id)) this.entries.set(item.id, { ...item, phase: terminal(item.phase) ? item.phase : 'waiting_file' });
      }
      for (const batch of batches.filter(batch => batch.scope === this.scope)) this.batches.set(batch.id, batch);
      this.sortEntries();
      this.publish({ initialized: true });
    } catch { this.publish({ initialized: true, persistenceError: '任务恢复不可用。本次上传仅当前页面有效，刷新可能无法恢复；请保留源文件。' }); }
  }
  private persist(entry: QueueEntry) {
    const snapshot = structuredClone(entry);
    this.writes = this.writes.then(() => localWrite('uploads', snapshot)).catch(() => {
      this.publish({ persistenceError: '本地任务保存失败。服务器已接收的内容不会因此重传；刷新前请核对任务报告，并保留源文件。' });
    });
  }
  private persistBatch(batch: UploadBatchPreparation) {
    const snapshot = structuredClone(batch);
    this.writes = this.writes.then(() => localWrite('upload_batches', snapshot)).catch(() => {
      this.publish({ persistenceError: '本地原始输入清单保存失败。服务器确认的批次清单仍保留；请保留全部源文件，刷新后从任务报告核对。' });
    });
  }
  private prepareBatch(id: string): Promise<void> {
    const pending = this.preparing.get(id);
    if (pending) return pending;
    const batch = this.batches.get(id);
    if (!batch) return Promise.reject(new Error('原始输入清单记录缺失，未自动新建或推断批次。请保留源文件并核对服务器任务。'));
    if (batch.sealed) {
      if (!batch.batchId) return Promise.reject(new Error('已封存清单缺少批次身份，未开始上传。'));
      for (const entry of this.entries.values()) if (entry.batchPlanId === id && (entry.request.batchId !== batch.batchId || entry.preparationFailed)) this.change(entry.id, { request: { ...entry.request, batchId: batch.batchId }, preparationFailed: false });
      return this.writes;
    }
    const controller = new AbortController();
    this.batchControllers.set(id, controller);
    const work = (async () => {
      if (!batch.batchId) {
        const created = await read(api.POST('/batches', { params: { header: operationHeaders(batch.id) }, body: batch.input, signal: controller.signal }));
        batch.batchId = created.id;
        batch.manifestRevision = created.manifestRevision;
        this.persistBatch(batch);
      }
      if (batch.manifestRevision === undefined) throw new Error('缺少服务器原始输入清单修订，尚未开始上传。');
      const sealed = await read(api.PUT('/batches/{id}/inputs', { params: { path: { id: batch.batchId }, header: operationHeaders(batch.manifestKey) }, body: { expectedManifestRevision: batch.manifestRevision, inputs: batch.inputs }, signal: controller.signal }));
      if (sealed.manifestState !== 'sealed' || sealed.expectedInputs !== batch.inputs.length) throw new Error('服务器未确认完整原始输入清单，尚未开始上传。');
      batch.sealed = true;
      batch.manifestRevision = sealed.manifestRevision;
      batch.fingerprint = sealed.manifestFingerprint;
      const entries = [...this.entries.values()].filter(entry => entry.batchPlanId === id).map((entry): QueueEntry => ({ ...entry, request: { ...entry.request, batchId: batch.batchId }, preparationFailed: false, ...(entry.preparationFailed && entry.phase === 'failed' && this.files.has(entry.id) ? { phase: 'queued', error: undefined } : {}) }));
      for (const entry of entries) this.entries.set(entry.id, entry);
      const records = [{ store: 'upload_batches' as const, item: structuredClone(batch) }, ...entries.map(entry => ({ store: 'uploads' as const, item: structuredClone(entry) }))];
      this.writes = this.writes.then(() => localWriteGroup(records)).catch(() => this.publish({ persistenceError: '清单已由服务器确认，但本地批次记录保存失败。请保留源文件，刷新后从任务报告核对。' }));
      this.publish();
      await this.writes;
    })().finally(() => { this.preparing.delete(id); this.batchControllers.delete(id); });
    this.preparing.set(id, work);
    return work;
  }
  private change(id: string, patch: Partial<QueueEntry>, persist = true) {
    const current = this.entries.get(id);
    if (!current || this.disposed) return;
    const next = { ...current, ...patch }; this.entries.set(id, next);
    if (persist) this.persist(next);
    this.publish();
  }
  async retryStorage() {
    const retry = this.writes.then(async () => {
      const previous = await localReadAll<QueueEntry>('uploads');
      const batches = await localReadAll<UploadBatchPreparation>('upload_batches');
      for (const batch of batches) if (batch.scope === this.scope && !this.batches.has(batch.id)) this.batches.set(batch.id, batch);
      for (const entry of previous) if (entry.scope === this.scope && !this.entries.has(entry.id)) this.entries.set(entry.id, { ...entry, phase: terminal(entry.phase) ? entry.phase : 'waiting_file' });
      this.sortEntries();
      for (const entry of this.entries.values()) await localWrite('uploads', entry);
      for (const batch of this.batches.values()) await localWrite('upload_batches', batch);
      this.publish({ persistenceError: undefined });
    }).catch(() => { this.publish({ persistenceError: '仍无法保存本地任务，请保留源文件并核对任务报告。' }); });
    this.writes = retry;
    await retry;
  }
  async add(files: readonly File[], target: Omit<Schema<'UploadCreate'>, 'fileName' | 'size' | 'mimeType'>, libraryName: string) {
    if (!this.snapshot.initialized || this.snapshot.preparing) throw new Error('正在准备上传队列，请稍后再选。');
    if (!files.length) return;
    const limit = Math.min(100, this.maxItems);
    if (files.length > limit) throw new Error(`本批最多 ${limit} 项，本次选择尚未登记或上传。请分批选择，不会截取前 ${limit} 项。`);
    if (this.entries.size + files.length > 500) throw new Error('队列记录最多 500 项，请先清理已结束记录。');
    if (files.some(file => file.size < 1)) throw new Error('当前选择含空文件，尚未创建批次。请移除空文件后重新选择。');
    const plan: UploadBatchPreparation = {
      id: newId(), scope: this.scope, sealed: false, manifestKey: newId(), inputs: [],
      input: { libraryId: target.libraryId, mode: target.mode, label: `${files.length} 个文件 · ${new Date().toLocaleString('zh-CN')}`, maxResearchCalls: target.mode === 'intelligent' ? 8 : 0 },
    };
    let order = Math.max(Date.now(), ...[...this.entries.values()].map(entry => entry.order ?? Date.parse(entry.createdAt)));
    const prepared = files.map((file): QueueEntry => {
      const id = newId();
      const relative = file.webkitRelativePath || file.name;
      const directory = file.webkitRelativePath?.includes('/') ? file.webkitRelativePath.slice(0, file.webkitRelativePath.lastIndexOf('/')) : '';
      // This immutable list is captured before hashing or any upload request.
      // Complete hashes bind each upload at creation; no partially hashed value is recorded here.
      plan.inputs.push({ id, originalFileName: file.name, sourceRelativePath: relative, size: file.size });
      const entry: QueueEntry = {
        id, batchPlanId: plan.id, scope: this.scope,
        request: { ...target, batchId: undefined, inputId: id, fileName: file.name, originalFileName: file.name, sourceRelativePath: relative, size: file.size, mimeType: file.type || 'application/octet-stream', sourceMtime: new Date(file.lastModified).toISOString(), relativeDirectory: [target.relativeDirectory, directory].filter(Boolean).join('/') || undefined },
        libraryName, name: file.name, size: file.size, modified: file.lastModified, phase: 'queued', offset: 0, hashProgress: 0, createdAt: new Date().toISOString(), order: ++order,
      };
      return entry;
    });
    this.publish({ preparing: true });
    prepared.forEach((entry, index) => { this.entries.set(entry.id, entry); this.files.set(entry.id, files[index]!); });
    this.batches.set(plan.id, plan);
    try {
      await this.writes;
      try {
        await localWriteGroup([{ store: 'upload_batches', item: structuredClone(plan) }, ...prepared.map(entry => ({ store: 'uploads' as const, item: structuredClone(entry) }))]);
      } catch {
        this.publish({ persistenceError: '本地原始输入清单与队列保存失败。本次仅当前页面可恢复；请保留全部源文件并核对服务器报告。' });
      }
      this.publish();
      await this.prepareBatch(plan.id);
    } catch (error) {
      for (const entry of prepared) this.change(entry.id, { phase: 'failed', preparationFailed: true, error: `原始输入清单尚未确认，未上传文件。${error instanceof Error ? error.message : '请重试当前队列项。'}` });
      throw new QueuedPreparationError(error);
    } finally { this.publish({ preparing: false }); this.pump(); }
  }
  private pump() {
    if (this.disposed || this.snapshot.preparing || this.active.size >= this.concurrency) return;
    for (const entry of this.entries.values()) {
      if (this.active.size >= this.concurrency) break;
      const file = this.files.get(entry.id);
      if (entry.phase !== 'queued' || !file || this.active.has(entry.id)) continue;
      // Completion allocates authoritative work/page numbers. An earlier
      // failed or paused member must be resolved before a later member starts.
      if (this.blockingEntry(entry)) continue;
      const controller = new AbortController(); this.active.set(entry.id, controller);
      void this.transfer(entry.id, file, controller.signal).finally(() => { this.active.delete(entry.id); this.pump(); });
    }
  }
  private async transfer(id: string, file: File, signal: AbortSignal) {
    try {
      let entry = this.entries.get(id)!;
      if (entry.batchPlanId) {
        await this.prepareBatch(entry.batchPlanId);
        if (this.disposed || signal.aborted) return;
        entry = this.entries.get(id)!;
      }
      this.change(id, { phase: 'hashing', error: undefined });
      const digest = await fileDigest(file, signal, value => this.change(id, { hashProgress: value }, false));
      const originalDigest = entry.digest ?? entry.request.expectedHash;
      if (originalDigest && digest !== originalDigest) throw new Error('所选文件内容已改变。没有继续旧上传，请选择原文件或作为新任务添加。');
      this.change(id, { digest, request: { ...entry.request, expectedHash: digest } });
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
      if (result.asset) this.change(id, { actualFileName: result.asset.fileName });
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
      if (entry.batchPlanId && entry.request.inputId) {
        await this.prepareBatch(entry.batchPlanId);
        const batch = this.batches.get(entry.batchPlanId);
        if (!batch?.batchId || !batch.fingerprint) throw new Error('原始输入清单身份尚未确认，未把本项标为已取消。');
        await read(api.POST('/batches/{id}/inputs/{inputId}/cancel', { params: { path: { id: batch.batchId, inputId: entry.request.inputId }, header: operationHeaders(`${entry.id}-cancel-input`) }, body: { manifestFingerprint: batch.fingerprint } }));
        this.change(id, { phase: 'cancelled', error: undefined });
        this.files.delete(id);
        void invalidateLibrary();
        return;
      }
      const current = entry.uploadId
        ? await read(api.GET('/uploads/{id}', { params: { path: { id: entry.uploadId } } }))
        : entry.digest ? await read(api.POST('/uploads', { params: { header: operationHeaders(entry.id) }, body: entry.request })) : undefined;
      if (current && ['ingested', 'staged', 'duplicate'].includes(current.state)) { this.finish(id, current); return; }
      if (current) await read(api.DELETE('/uploads/{id}', { params: { path: { id: current.id } } }));
      this.change(id, { phase: 'cancelled', error: undefined }); this.files.delete(id);
    } catch (error) {
      const latest = this.entries.get(id);
      let reconciliationError: unknown;
      if (latest?.request.batchId && latest.request.inputId) {
        try {
          const report = await read(api.GET('/batches/{id}', { params: { path: { id: latest.request.batchId } } }));
          const input = report.inputs.find(input => input.id === latest.request.inputId);
          if (input?.state === 'ingested' && input.assetId) { this.change(id, { phase: 'ingested', assetId: input.assetId, offset: latest.size, error: undefined }); this.files.delete(id); return; }
          if (input?.state === 'cancelled') { this.change(id, { phase: 'cancelled', error: undefined }); this.files.delete(id); return; }
        } catch (failure) { reconciliationError = failure; }
      }
      this.change(id, { error: `${error instanceof Error ? error.message : '取消失败'}。${reconciliationError ? '回读服务器状态也未成功。' : ''}请重试取消或继续上传以回读实际状态。` });
    }
    finally { this.pump(); }
  }
  async clearFinished() {
    const finished = [...this.entries.values()].filter(entry => terminal(entry.phase)).map(entry => entry.id);
    const batchIds = new Set(finished.map(id => this.entries.get(id)?.batchPlanId).filter((id): id is string => !!id));
    const cleanup = this.writes.then(async () => {
      for (const id of finished) {
        try { await localWrite('uploads', id); this.entries.delete(id); }
        catch { this.publish({ persistenceError: '清理本地记录失败；记录已保留，媒体不会被删除。' }); }
      }
      for (const id of batchIds) if (!this.preparing.has(id) && ![...this.entries.values()].some(entry => entry.batchPlanId === id)) {
        try { await localWrite('upload_batches', id); this.batches.delete(id); }
        catch { this.publish({ persistenceError: '本地已结束批次清单清理失败；服务器任务与原始输入清单均保留。' }); }
      }
      this.publish();
    });
    this.writes = cleanup;
    await cleanup;
  }
  dispose() { this.disposed = true; for (const active of this.active.values()) active.abort(); for (const controller of this.batchControllers.values()) controller.abort(); this.files.clear(); }
}
