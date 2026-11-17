import { beforeEach, describe, expect, it, vi } from 'vitest';
const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), remove: vi.fn(), readAll: vi.fn(), write: vi.fn(), digest: vi.fn(), invalidate: vi.fn() }));
vi.mock('../src/data/client', () => ({ api: { GET: mocks.get, POST: mocks.post, PUT: mocks.put, DELETE: mocks.remove }, read: (value: unknown) => Promise.resolve(value), operationHeaders: (id: string) => ({ 'Idempotency-Key': id }) }));
vi.mock('../src/data/storage', () => ({ localReadAll: mocks.readAll, localWrite: mocks.write }));
vi.mock('../src/data/hash', () => ({ fileDigest: mocks.digest }));
vi.mock('../src/data/query', () => ({ invalidateLibrary: mocks.invalidate }));
import { UploadQueue } from '../src/data/upload-queue';
import type { QueueEntry } from '../src/data/upload-queue';

const file = () => new File(['synthetic media'], 'fixture.jpg', { type: 'image/jpeg', lastModified: 1000 });
const target = { libraryId: 'library-fixture', mode: 'ordinary' as const };
const upload = (state = 'receiving', offset = 0) => ({ id: 'upload-fixture', state, offset, size: file().size, assetId: state === 'ingested' ? 'asset-fixture' : undefined });
beforeEach(() => {
  vi.clearAllMocks(); mocks.readAll.mockResolvedValue([]); mocks.write.mockResolvedValue(undefined); mocks.digest.mockResolvedValue('source-digest');
  mocks.get.mockResolvedValue(upload()); mocks.put.mockResolvedValue(upload('receiving', file().size)); mocks.remove.mockResolvedValue(undefined);
  mocks.post.mockImplementation((path: string) => path === '/batches' ? Promise.resolve({ id: 'batch-fixture' }) : path === '/uploads' ? Promise.resolve(upload()) : Promise.resolve({ upload: upload('ingested', file().size) }));
});
async function initialized() { const queue = new UploadQueue('instance-fixture:device-fixture', 8 * 1024 * 1024, 100); await vi.waitFor(() => expect(queue.getSnapshot().initialized).toBe(true)); return queue; }

describe('durable upload behavior', () => {
  it('reconciles a lost completion response without creating or sending the file again', async () => {
    let completionAttempts = 0;
    mocks.post.mockImplementation((path: string) => {
      if (path === '/batches') return Promise.resolve({ id: 'batch-fixture' });
      if (path === '/uploads') return Promise.resolve(upload());
      completionAttempts++; return Promise.reject(new Error('completion response lost'));
    });
    const queue = await initialized(); await queue.add([file()], target, '合成库');
    await vi.waitFor(() => expect(queue.getSnapshot().entries[0]?.phase).toBe('failed'));
    const entry = queue.getSnapshot().entries[0]!;
    mocks.get.mockResolvedValue(upload('ingested', file().size)); queue.retry(entry.id);
    await vi.waitFor(() => expect(queue.getSnapshot().entries[0]?.phase).toBe('ingested'));
    expect(mocks.post.mock.calls.filter(call => call[0] === '/uploads')).toHaveLength(1);
    expect(mocks.put).toHaveBeenCalledTimes(1); expect(completionAttempts).toBe(1); queue.dispose();
  });
  it('rejects a changed file even when its name, size and modification time match', async () => {
    const restored: QueueEntry = { id: 'operation-fixture', scope: 'instance-fixture:device-fixture', request: { ...target, fileName: file().name, size: file().size, mimeType: file().type }, libraryName: '合成库', name: file().name, size: file().size, modified: 1000, digest: 'original-digest', uploadId: 'upload-fixture', phase: 'uploading', offset: 4, hashProgress: 0, createdAt: '2026-01-01T00:00:00Z' };
    mocks.readAll.mockResolvedValue([restored]); mocks.digest.mockResolvedValue('changed-digest');
    const queue = await initialized(); queue.retry(restored.id, file());
    await vi.waitFor(() => expect(queue.getSnapshot().entries[0]?.error).toContain('内容已改变'));
    expect(mocks.put).not.toHaveBeenCalled(); expect(mocks.get).not.toHaveBeenCalled(); queue.dispose();
  });
  it('storage retry does not repeat an already committed business request', async () => {
    mocks.write.mockRejectedValue(new Error('quota exceeded'));
    const queue = await initialized(); await queue.add([file()], target, '合成库');
    await vi.waitFor(() => expect(queue.getSnapshot().entries[0]?.phase).toBe('ingested'));
    expect(queue.getSnapshot().persistenceError).toBeTruthy();
    const requests = mocks.post.mock.calls.length; mocks.write.mockResolvedValue(undefined); await queue.retryStorage();
    expect(queue.getSnapshot().persistenceError).toBeUndefined(); expect(mocks.post).toHaveBeenCalledTimes(requests); queue.dispose();
  });
});
