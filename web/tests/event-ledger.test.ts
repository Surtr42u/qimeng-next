import { beforeEach, describe, expect, it, vi } from "vitest";
const mocks = vi.hoisted(() => ({
  post: vi.fn(),
  all: vi.fn(),
  write: vi.fn(),
  invalidate: vi.fn(),
}));
vi.mock("../src/data/client", () => ({
  api: { POST: mocks.post },
  read: (value: unknown) => Promise.resolve(value),
}));
vi.mock("../src/data/storage", () => ({
  localReadAll: mocks.all,
  localWrite: mocks.write,
}));
vi.mock("../src/data/query", () => ({
  queryClient: { invalidateQueries: mocks.invalidate },
}));
import { EventLedger } from "../src/data/event-ledger";
import { ApiError } from "../src/lib/errors";
import type { Schema } from "../src/data/client";

const event = {
  eventId: "synthetic-event-1",
  sessionId: "synthetic-session-1",
  kind: "view" as const,
  contentVersion: 1,
  occurredAt: "2026-01-01T00:00:00Z",
};
type Row = { id: string; scope: string; assetId: string; body: Schema<"EventCreate">; confirmed: boolean; rejection?: { code: string; message: string } };
function progress(assetId: string): Row {
  const body = { ...event, eventId: "progress-" + assetId, kind: "progress" as const, positionMs: 1250 };
  return { id: "instance-a:" + body.eventId, scope: "instance-a", assetId, body, confirmed: false };
}
function deviceStorage(initial: Row[], fails?: (item: Row | string) => boolean) {
  const rows = new Map(initial.map(row => [row.id, structuredClone(row)]));
  mocks.all.mockImplementation(async () => structuredClone([...rows.values()]));
  mocks.write.mockImplementation(async (_store: string, item: Row | string) => {
    if (fails?.(item)) throw new Error("storage unavailable");
    if (typeof item === "string") rows.delete(item);
    else rows.set(item.id, structuredClone(item));
  });
  return rows;
}
const changed = new ApiError(409, "content_changed", "内容已替换，旧记录不能覆盖新版进度。");
beforeEach(() => {
  vi.clearAllMocks();
  mocks.all.mockResolvedValue([]);
  mocks.write.mockResolvedValue(undefined);
  mocks.post.mockResolvedValue({});
  mocks.invalidate.mockResolvedValue(undefined);
});
describe("offline event receipts", () => {
  it("replays an unconfirmed event with the same identity after connection failure", async () => {
    mocks.post.mockRejectedValueOnce(new Error("offline"));
    const ledger = new EventLedger("instance-a");
    await ledger.record("asset-a", event);
    await vi.waitFor(() => expect(ledger.getSnapshot().error).toBe("offline"));
    await ledger.flush();
    expect(ledger.getSnapshot().pending).toBe(0);
    expect(mocks.post).toHaveBeenCalledTimes(2);
    expect(mocks.post.mock.calls[0]?.[1].body).toEqual(event);
    expect(mocks.post.mock.calls[1]?.[1].body).toEqual(event);
    ledger.dispose();
  });
  it("retries receipt cleanup without posting an already confirmed event again", async () => {
    let firstDelete = true;
    mocks.write.mockImplementation((_store: string, item: unknown) => {
      if (typeof item === "string" && firstDelete) {
        firstDelete = false;
        return Promise.reject(new Error("storage unavailable"));
      }
      return Promise.resolve();
    });
    const ledger = new EventLedger("instance-a");
    await ledger.record("asset-a", event);
    await vi.waitFor(() =>
      expect(ledger.getSnapshot().error).toBe("storage unavailable"),
    );
    await ledger.flush();
    expect(ledger.getSnapshot().pending).toBe(0);
    expect(mocks.post).toHaveBeenCalledTimes(1);
    ledger.dispose();
  });
  it("never replays another server instance records into the current server", async () => {
    mocks.all.mockResolvedValue([
      {
        id: "other:event",
        scope: "other-instance",
        assetId: "asset-other",
        body: event,
        confirmed: false,
      },
    ]);
    const ledger = new EventLedger("current-instance");
    await ledger.flush();
    expect(mocks.post).not.toHaveBeenCalled();
    ledger.dispose();
  });
  it("keeps a replaced version's receipt across reload while later progress is delivered once", async () => {
    const a = progress("asset-a"), b = progress("asset-b"), c = progress("asset-c");
    let failClear = true;
    const disk = deviceStorage([a, b], item => item === a.id && failClear);
    mocks.post.mockRejectedValueOnce(changed);
    const first = new EventLedger("instance-a");
    await first.flush();
    const rejection = { ...a, rejection: { code: changed.code, message: changed.message } };
    expect(first.getSnapshot()).toMatchObject({ pending: 0, rejected: [rejection] });
    expect([...disk.values()]).toEqual([rejection]);
    first.dispose();

    const next = new EventLedger("instance-a");
    await next.flush();
    expect(next.getSnapshot().rejected).toEqual([rejection]);
    await next.record(a.assetId, a.body);
    await expect(next.record(a.assetId, { ...a.body, positionMs: 5000 })).rejects.toThrow("同一事件身份");
    await next.record(c.assetId, c.body);
    await vi.waitFor(() => expect(next.getSnapshot().pending).toBe(0));
    expect(mocks.post.mock.calls.map(([path, { params, body }]) => [path, params.path.id, body])).toEqual(
      [a, b, c].map(row => ["/assets/{id}/events", row.assetId, row.body]),
    );
    await expect(next.clearRejected(a.id)).rejects.toThrow("storage unavailable");
    expect(next.getSnapshot().rejected).toEqual([rejection]);
    expect([...disk.values()]).toEqual([rejection]);
    failClear = false;
    await next.clearRejected(a.id);
    expect(next.getSnapshot().rejected).toEqual([]);
    expect(disk.size).toBe(0);
    next.dispose();
  });
  it.each([
    [409, "event_conflict"], [400, "invalid_event"], [400, "missing_progress_position"],
  ] as const)("retains the immutable event rejection %i %s", async (status, code) => {
    const a = progress("asset-a");
    const disk = deviceStorage([a]);
    mocks.post.mockRejectedValueOnce(new ApiError(status, code, "服务端拒绝了此事件"));
    const ledger = new EventLedger("instance-a");
    await ledger.flush();
    await ledger.flush();
    expect(disk.get(a.id)?.rejection?.code).toBe(code);
    expect(ledger.getSnapshot().pending).toBe(0);
    expect(mocks.post).toHaveBeenCalledTimes(1);
    ledger.dispose();
  });
  it.each([
    [409, "asset_trashed"], [409, "unknown_conflict"], [400, "event_time_invalid"], [503, "content_changed"],
  ] as const)("retries the original event after recoverable or unknown rejection %i %s", async (status, code) => {
    const a = progress("asset-a"), b = progress("asset-b");
    const disk = deviceStorage([a, b]);
    mocks.post.mockRejectedValueOnce(new ApiError(status, code, "稍后重试"));
    const ledger = new EventLedger("instance-a");
    await ledger.flush();
    expect(ledger.getSnapshot()).toMatchObject({ pending: 2, rejected: [], error: "稍后重试" });
    expect([...disk.values()]).toEqual([a, b]);
    await ledger.flush();
    expect(mocks.post.mock.calls.map(([, { body }]) => body)).toEqual([a.body, a.body, b.body]);
    expect(disk.size).toBe(0);
    ledger.dispose();
  });
  it("retries saving a rejection without resending it or blocking another receipt", async () => {
    const a = progress("asset-a"), b = progress("asset-b");
    let failSave = true;
    const disk = deviceStorage([a, b], item => typeof item !== "string" && !!item.rejection && failSave);
    mocks.post.mockRejectedValueOnce(changed);
    const ledger = new EventLedger("instance-a");
    await ledger.flush();
    expect([...disk.values()]).toEqual([a]);
    expect(ledger.getSnapshot()).toMatchObject({ pending: 0, storageError: expect.any(String), rejected: [{ id: a.id }] });
    failSave = false;
    await ledger.flush();
    expect(disk.get(a.id)?.rejection?.code).toBe("content_changed");
    expect(ledger.getSnapshot().storageError).toBeUndefined();
    expect(mocks.post.mock.calls.map(([, { body }]) => body)).toEqual([a.body, b.body]);
    ledger.dispose();
  });
  it("restores retained evidence on retry after the first IndexedDB read failed", async () => {
    const a = { ...progress("asset-a"), rejection: { code: changed.code, message: changed.message } };
    const b = progress("asset-b");
    const disk = deviceStorage([a, b]);
    mocks.all.mockRejectedValueOnce(new Error("storage unavailable"));
    const ledger = new EventLedger("instance-a");
    await ledger.flush();
    expect(ledger.getSnapshot()).toMatchObject({ pending: 0, rejected: [a], storageError: undefined });
    expect([...disk.values()]).toEqual([a]);
    expect(mocks.post.mock.calls.map(([, { body }]) => body)).toEqual([b.body]);
    ledger.dispose();
  });
  it("reuses the original event after reload when the accepted receipt and its cleanup both failed", async () => {
    const a = progress("asset-a");
    let unavailable = true;
    const disk = deviceStorage([a], () => unavailable);
    const first = new EventLedger("instance-a");
    await first.flush();
    expect(first.getSnapshot().error).toBe("storage unavailable");
    expect(disk.get(a.id)).toEqual(a);
    first.dispose();
    unavailable = false;
    const next = new EventLedger("instance-a");
    await next.flush();
    expect(mocks.post.mock.calls.map(([, { body }]) => body)).toEqual([a.body, a.body]);
    expect(disk.size).toBe(0);
    next.dispose();
  });
  it("aborts an old instance's in-flight replay and leaves its remaining rows with that instance", async () => {
    const a = progress("asset-a"), b = progress("asset-b");
    const disk = deviceStorage([a, b]);
    mocks.post.mockImplementationOnce((_path: string, input: { signal: AbortSignal }) => new Promise((_resolve, reject) => {
      input.signal.addEventListener("abort", () => reject(input.signal.reason), { once: true });
    }));
    const first = new EventLedger("instance-a");
    const sending = first.flush();
    await vi.waitFor(() => expect(mocks.post).toHaveBeenCalledTimes(1));
    first.dispose();
    await sending;
    const other = new EventLedger("instance-b");
    await other.flush();
    expect(mocks.post).toHaveBeenCalledTimes(1);
    expect([...disk.values()]).toEqual([a, b]);
    expect(other.getSnapshot()).toMatchObject({ pending: 0, rejected: [] });
    other.dispose();
  });
  it("keeps rejection receipts within the same 2000-row limit until explicitly cleared", async () => {
    const rows = Array.from({ length: 2000 }, (_, i) => ({ ...progress("asset-" + i), rejection: { code: changed.code, message: changed.message } }));
    const disk = deviceStorage(rows);
    const ledger = new EventLedger("instance-a");
    const next = progress("new-asset");
    await expect(ledger.record(next.assetId, next.body)).rejects.toThrow("2000");
    expect(disk.size).toBe(2000);
    await ledger.flush();
    expect(mocks.post).not.toHaveBeenCalled();
    await ledger.clearRejected(rows[0]!.id);
    await ledger.record(next.assetId, next.body);
    await vi.waitFor(() => expect(ledger.getSnapshot().pending).toBe(0));
    expect(ledger.getSnapshot().rejected).toHaveLength(1999);
    expect(disk.size).toBe(1999);
    expect(mocks.post).toHaveBeenCalledTimes(1);
    ledger.dispose();
  });
});
