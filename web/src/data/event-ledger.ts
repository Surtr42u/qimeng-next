import { api, read } from "./client";
import type { Schema } from "./client";
import { ApiError } from "../lib/errors";
import { localReadAll, localWrite } from "./storage";
import { invalidatePlaybackDetail, queryClient } from "./query";

type EventRow = {
  id: string;
  scope: string;
  assetId: string;
  body: Schema<"EventCreate">;
  confirmed: boolean;
};
type RejectedEvent = EventRow & { rejection: { code: string; message: string } };
type StoredEvent = EventRow | RejectedEvent;
type Health = {
  pending: number;
  rejected: readonly RejectedEvent[];
  error?: string;
  storageError?: string;
};
const capacity = 2000;
const capacityError = "本机浏览记录达到 2000 条，请先补发待处理的记录或清除已拒绝的记录。";
const eventFields = ["kind", "contentVersion", "sessionId", "seconds", "occurredAt", "positionMs"] as const;

function isPermanentRejection(error: unknown): error is ApiError {
  return error instanceof ApiError && (
    (error.status === 409 && ["content_changed", "event_conflict"].includes(error.code)) ||
    (error.status === 400 && ["invalid_event", "missing_progress_position"].includes(error.code))
  );
}

export class EventLedger {
  private rows = new Map<string, StoredEvent>();
  // A rejected row only needs work until its local receipt is durable.
  private outstanding = new Set<string>();
  private listeners = new Set<() => void>();
  private health: Health = { pending: 0, rejected: [] };
  private running = false;
  private disposed = false;
  private restoreFailed = false;
  private restoring = true;
  private ready: Promise<void>;
  private controller?: AbortController;
  constructor(readonly scope: string) {
    this.ready = this.restore();
  }
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  getSnapshot = () => this.health;
  hasEarlierProgress(assetId: string, version: number, sessionId: string) {
    if (this.restoring) return true;
    for (const id of this.outstanding) {
      const entry = this.rows.get(id);
      if (entry && !entry.confirmed && !("rejection" in entry) && entry.assetId === assetId &&
          entry.body.kind === "progress" && entry.body.contentVersion === version && entry.body.sessionId !== sessionId) return true;
    }
    return false;
  }
  private publish(patch: Partial<Health> = {}) {
    const rejected = patch.rejected ?? this.health.rejected;
    this.health = { ...this.health, ...patch, pending: this.rows.size - rejected.length };
    this.listeners.forEach((listener) => listener());
  }
  private publishRejections(patch: Partial<Health> = {}) {
    const rejected = [...this.rows.values()].filter((entry): entry is RejectedEvent => "rejection" in entry);
    this.publish({ ...patch, rejected });
  }
  private async restore() {
    try {
      for (const entry of await localReadAll<StoredEvent>("events")) {
        if (entry.scope !== this.scope || this.rows.has(entry.id)) continue;
        this.rows.set(entry.id, entry);
        if (!("rejection" in entry)) this.outstanding.add(entry.id);
      }
      this.restoreFailed = false;
      this.restoring = false;
      this.publishRejections({ storageError: undefined });
    } catch {
      this.restoreFailed = true;
      this.restoring = false;
      this.publish({
        storageError:
          "浏览记录的本地恢复不可用，关闭页面可能丢失尚未补发的记录。",
      });
    }
  }
  async record(assetId: string, body: Schema<"EventCreate">) {
    await this.ready;
    if (this.disposed)
      throw new Error("影库连接已改变，未将旧记录发送到新影库。");
    const id = `${this.scope}:${body.eventId}`;
    const existing = this.rows.get(id);
    if (existing) {
      if (existing.assetId !== assetId || eventFields.some(key => existing.body[key] !== body[key]))
        throw new Error("本机已有同一事件身份的不同记录，已保留原记录。");
      void this.flush();
      return;
    }
    if (this.rows.size >= capacity) {
      this.publish({ error: capacityError });
      throw new Error(capacityError);
    }
    const entry: EventRow = { id, scope: this.scope, assetId, body, confirmed: false };
    this.rows.set(id, entry);
    this.outstanding.add(id);
    try {
      await localWrite("events", entry);
    } catch {
      this.publish({
        storageError:
          "浏览记录只能暂存在当前页面；本地存储不可用，关闭前请恢复连接。",
      });
    }
    this.publish();
    void this.flush();
  }
  private async post(entry: EventRow, signal: AbortSignal): Promise<StoredEvent> {
    try {
      await read(api.POST("/assets/{id}/events", {
        params: { path: { id: entry.assetId } },
        body: entry.body,
        signal,
      }));
    } catch (error) {
      if (!isPermanentRejection(error)) throw error;
      if (entry.body.kind === "progress" && error.code === "content_changed" && !this.disposed)
        invalidatePlaybackDetail(this.scope, entry.assetId);
      return { ...entry, rejection: { code: error.code, message: error.message } };
    }
    entry.confirmed = true;
    if (entry.body.kind === "progress" && !this.disposed) invalidatePlaybackDetail(this.scope, entry.assetId);
    try {
      await localWrite("events", entry);
    } catch {
      this.publish({
        storageError:
          "浏览记录已获服务端确认，但本地收据尚未保存；重复补发仍沿用原事件身份。",
      });
    }
    return entry;
  }
  async flush() {
    await this.ready;
    if (this.running || this.disposed || (!this.outstanding.size && !this.restoreFailed)) return;
    this.running = true;
    this.controller = new AbortController();
    let received = false;
    try {
      if (this.restoreFailed) await this.restore();
      for (const id of this.outstanding) {
        if (this.disposed) break;
        let entry = this.rows.get(id)!;
        if (!entry.confirmed && !("rejection" in entry))
          entry = await this.post(entry, this.controller.signal);
        if ("rejection" in entry) {
          this.rows.set(id, entry);
          try {
            await localWrite("events", entry);
            this.outstanding.delete(id);
          } catch {
            this.publish({ storageError: "服务器的拒绝原因尚未保存到本机，请重试保存。" });
          }
          this.publishRejections();
          continue;
        }
        received = true;
        await localWrite("events", id);
        this.outstanding.delete(id);
        this.rows.delete(id);
        this.publish();
      }
      this.publish({
        error: undefined,
        ...(this.outstanding.size === 0 && !this.restoreFailed ? { storageError: undefined } : {}),
      });
    } catch (error) {
      if (!this.disposed)
        this.publish({
          error:
            error instanceof Error
              ? error.message
              : "浏览记录尚未补发成功，请重试。",
        });
    } finally {
      this.running = false;
    }
    if (received && !this.disposed) {
      await queryClient.invalidateQueries({
        queryKey: ["remote", this.scope],
        predicate: (query) =>
          ["stats", "history", "overview", "trends", "review", "rankings"].includes(String(query.queryKey[3])),
      });
    }
  }
  async clearRejected(id: string) {
    const row = this.rows.get(id);
    if (this.disposed || !row || !("rejection" in row)) return;
    const unsaved = this.outstanding.delete(id);
    try {
      await localWrite("events", id);
    } catch (error) {
      if (unsaved && this.rows.get(id) === row) this.outstanding.add(id);
      throw error;
    }
    this.rows.delete(id);
    this.publishRejections({
      ...(this.health.error === capacityError ? { error: undefined } : {}),
      ...(!this.outstanding.size && !this.restoreFailed ? { storageError: undefined } : {}),
    });
  }
  dispose() {
    this.disposed = true;
    this.controller?.abort();
  }
}
