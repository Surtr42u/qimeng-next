import { beforeEach, describe, expect, it, vi } from 'vitest';
const mocks = vi.hoisted(() => ({ post: vi.fn(), all: vi.fn(), write: vi.fn(), invalidate: vi.fn() }));
vi.mock('../src/data/client', () => ({ api: { POST: mocks.post }, read: (value: unknown) => Promise.resolve(value) }));
vi.mock('../src/data/storage', () => ({ localReadAll: mocks.all, localWrite: mocks.write }));
vi.mock('../src/data/query', () => ({ queryClient: { invalidateQueries: mocks.invalidate } }));
import { EventLedger } from '../src/data/event-ledger';

const event = { eventId: 'synthetic-event-1', sessionId: 'synthetic-session-1', kind: 'view' as const, contentVersion: 1, occurredAt: '2026-01-01T00:00:00Z' };
beforeEach(() => { vi.clearAllMocks(); mocks.all.mockResolvedValue([]); mocks.write.mockResolvedValue(undefined); mocks.post.mockResolvedValue({}); mocks.invalidate.mockResolvedValue(undefined); });
describe('offline event receipts', () => {
  it('replays an unconfirmed event with the same identity after connection failure', async () => {
    mocks.post.mockRejectedValueOnce(new Error('offline'));
    const ledger = new EventLedger('instance-a'); await ledger.record('asset-a', event);
    await vi.waitFor(() => expect(ledger.getSnapshot().error).toBe('offline'));
    await ledger.flush(); expect(ledger.getSnapshot().pending).toBe(0);
    expect(mocks.post).toHaveBeenCalledTimes(2);
    expect(mocks.post.mock.calls[0]?.[1].body).toEqual(event); expect(mocks.post.mock.calls[1]?.[1].body).toEqual(event);
    ledger.dispose();
  });
  it('retries receipt cleanup without posting an already confirmed event again', async () => {
    let firstDelete = true;
    mocks.write.mockImplementation((_store: string, item: unknown) => { if (typeof item === 'string' && firstDelete) { firstDelete = false; return Promise.reject(new Error('storage unavailable')); } return Promise.resolve(); });
    const ledger = new EventLedger('instance-a'); await ledger.record('asset-a', event);
    await vi.waitFor(() => expect(ledger.getSnapshot().error).toBe('storage unavailable'));
    await ledger.flush(); expect(ledger.getSnapshot().pending).toBe(0); expect(mocks.post).toHaveBeenCalledTimes(1); ledger.dispose();
  });
  it('never replays another server instance records into the current server', async () => {
    mocks.all.mockResolvedValue([{ id: 'other:event', scope: 'other-instance', assetId: 'asset-other', body: event, confirmed: false }]);
    const ledger = new EventLedger('current-instance'); await ledger.flush(); expect(mocks.post).not.toHaveBeenCalled(); ledger.dispose();
  });
});
