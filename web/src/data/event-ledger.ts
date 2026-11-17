import { api, read } from './client';
import type { Schema } from './client';
import { localReadAll, localWrite } from './storage';
import { queryClient } from './query';

type PendingEvent = { id: string; scope: string; assetId: string; body: Schema<'EventCreate'>; confirmed: boolean };
type Health = { pending: number; error?: string; storageError?: string };
export class EventLedger {
  private pending = new Map<string, PendingEvent>();
  private listeners = new Set<() => void>();
  private health: Health = { pending: 0 };
  private running = false;
  private disposed = false;
  private ready: Promise<void>;
  private controller?: AbortController;
  constructor(readonly scope: string) { this.ready = this.restore(); }
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  getSnapshot = () => this.health;
  private publish(patch: Partial<Health> = {}) { this.health = { ...this.health, ...patch, pending: this.pending.size }; this.listeners.forEach(listener => listener()); }
  private async restore() {
    try {
      for (const entry of await localReadAll<PendingEvent>('events')) if (entry.scope === this.scope) this.pending.set(entry.id, entry);
      this.publish();
    } catch { this.publish({ storageError: '浏览记录的本地恢复不可用，关闭页面可能丢失尚未补发的记录。' }); }
  }
  async record(assetId: string, body: Schema<'EventCreate'>) {
    await this.ready;
    if (this.disposed) throw new Error('影库连接已改变，未将旧记录发送到新影库。');
    if (this.pending.size >= 2000) throw new Error('待补发浏览记录达到 2000 条，请先恢复连接并重试补发。');
    const entry: PendingEvent = { id: `${this.scope}:${body.eventId}`, scope: this.scope, assetId, body, confirmed: false };
    this.pending.set(entry.id, entry);
    try { await localWrite('events', entry); }
    catch { this.publish({ storageError: '浏览记录只能暂存在当前页面；本地存储不可用，关闭前请恢复连接。' }); }
    this.publish(); void this.flush();
  }
  async flush() {
    await this.ready;
    if (this.running || this.disposed) return;
    this.running = true; this.controller = new AbortController();
    try {
      for (const entry of this.pending.values()) {
        if (this.disposed) break;
        if (!entry.confirmed) {
          await read(api.POST('/assets/{id}/events', { params: { path: { id: entry.assetId } }, body: entry.body, signal: this.controller.signal }));
          entry.confirmed = true;
          try { await localWrite('events', entry); } catch { this.publish({ storageError: '浏览记录已获服务端确认，但本地收据尚未保存；重复补发仍沿用原事件身份。' }); }
        }
        await localWrite('events', entry.id);
        this.pending.delete(entry.id); this.publish();
      }
      this.publish({ error: undefined, ...(this.pending.size === 0 ? { storageError: undefined } : {}) });
      await queryClient.invalidateQueries({ predicate: query => query.queryKey[0] === 'remote' && ['stats', 'history', 'overview', 'trends', 'review', 'rankings'].includes(String(query.queryKey[3])) });
    } catch (error) {
      if (!this.disposed) this.publish({ error: error instanceof Error ? error.message : '浏览记录尚未补发成功，请重试。' });
    } finally { this.running = false; }
  }
  dispose() { this.disposed = true; this.controller?.abort(); }
}
