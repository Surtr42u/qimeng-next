import { beforeEach, describe, expect, it, vi } from "vitest";
const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  remove: vi.fn(),
  readAll: vi.fn(),
  write: vi.fn(),
  writeGroup: vi.fn(),
  digest: vi.fn(),
  invalidate: vi.fn(),
}));
vi.mock("../src/data/client", () => ({
  api: {
    GET: mocks.get,
    POST: mocks.post,
    PUT: mocks.put,
    DELETE: mocks.remove,
  },
  read: (value: unknown) => Promise.resolve(value),
  operationHeaders: (id: string) => ({ "Idempotency-Key": id }),
}));
vi.mock("../src/data/storage", () => ({
  localReadAll: mocks.readAll,
  localWrite: mocks.write,
  localWriteGroup: mocks.writeGroup,
}));
vi.mock("../src/data/hash", () => ({ fileDigest: mocks.digest }));
vi.mock("../src/data/query", () => ({ invalidateLibrary: mocks.invalidate }));
import { UploadQueue } from "../src/data/upload-queue";
import type { QueueEntry, UploadBatchPreparation } from "../src/data/upload-queue";

const file = () =>
  new File(["synthetic media"], "fixture.jpg", {
    type: "image/jpeg",
    lastModified: 1000,
  });
const target = { libraryId: "library-fixture", mode: "ordinary" as const };
const batch = { id: 'batch-fixture', manifestRevision: 1, manifestState: 'open' };
const sealed = (count: number) => ({ ...batch, manifestRevision: 2, manifestState: 'sealed', manifestFingerprint: 'a'.repeat(64), expectedInputs: count });
const upload = (state = "receiving", offset = 0) => ({
  id: "upload-fixture",
  state,
  offset,
  size: file().size,
  assetId: state === "ingested" ? "asset-fixture" : undefined,
});
beforeEach(() => {
  vi.clearAllMocks();
  mocks.readAll.mockResolvedValue([]);
  mocks.write.mockResolvedValue(undefined);
  mocks.writeGroup.mockResolvedValue(undefined);
  mocks.digest.mockResolvedValue("source-digest");
  mocks.get.mockImplementation((path: string) => {
    if (path !== '/batches/{id}') return Promise.resolve(upload());
    const manifest = mocks.put.mock.calls.filter(call => call[0] === '/batches/{id}/inputs').at(-1)![1].body.inputs;
    return Promise.resolve({ batch: sealed(manifest.length), inputs: manifest.map((input: { id: string }) => ({ ...input, state: 'pending' })) });
  });
  mocks.put.mockImplementation((path: string, options: { body: { inputs?: unknown[] } }) => Promise.resolve(path === '/batches/{id}/inputs' ? sealed(options.body.inputs?.length ?? 0) : upload('receiving', file().size)));
  mocks.remove.mockResolvedValue(undefined);
  mocks.post.mockImplementation((path: string) =>
    path === "/batches"
      ? Promise.resolve(batch)
      : path === "/uploads"
        ? Promise.resolve(upload())
        : Promise.resolve({ upload: upload("ingested", file().size) }),
  );
});
async function initialized() {
  const queue = new UploadQueue(
    "instance-fixture:device-fixture",
    8 * 1024 * 1024,
    100,
  );
  await vi.waitFor(() => expect(queue.getSnapshot().initialized).toBe(true));
  return queue;
}

describe("durable upload behavior", () => {
  const storedUpload = (phase: QueueEntry['phase'], mode: 'ordinary' | 'intelligent' = 'ordinary'): QueueEntry => ({
    id: 'stored-selection', scope: 'instance-fixture:device-fixture', request: { ...target, mode, fileName: file().name, size: file().size, mimeType: file().type, expectedHash: 'source-digest' },
    name: file().name, libraryName: '合成库', size: file().size, modified: file().lastModified, digest: 'source-digest', uploadId: 'upload-fixture', phase, offset: file().size, hashProgress: file().size, createdAt: '2026-01-01T00:00:00Z',
  });
  it('restores rejected inputs from an older sealed queue without needing the files again', async () => {
    const names = ['empty.png', 'good.png', 'notes.txt'];
    const entries = names.map((name, index): QueueEntry => {
      const original = storedUpload('failed', 'intelligent');
      return { ...original, id: `input-${index}`, uploadId: undefined, name, size: index === 0 ? 0 : 8, offset: 0,
        batchPlanId: 'old-plan', request: { ...original.request, batchId: batch.id, fileName: name, inputId: `input-${index}` } };
    });
    const inputs = entries.map(entry => ({ id: entry.id, nameState: 'known' as const, originalFileName: entry.name, size: entry.size }));
    const plan: UploadBatchPreparation = { id: 'old-plan', scope: entries[0]!.scope, input: { ...target, mode: 'intelligent', label: 'old selection', maxResearchCalls: 8 },
      inputs, batchId: batch.id, manifestKey: 'original-key', sealed: true, fingerprint: sealed(3).manifestFingerprint };
    mocks.readAll.mockImplementation((store: string) => Promise.resolve(store === 'uploads' ? entries : [plan]));
    mocks.get.mockResolvedValue({ batch: sealed(3), inputs: inputs.map((input, index) => ({ ...input,
      state: index === 1 ? 'pending' : 'failed', errorCode: ['empty_input', undefined, 'unsupported_media'][index] })) });
    const queue = await initialized();
    await vi.waitFor(() => expect(queue.getSnapshot().entries.map(entry => entry.phase)).toEqual(['rejected', 'waiting_file', 'rejected']));
    queue.retry('input-0');
    await queue.cancel('input-2');
    await queue.clearFinished();
    expect(queue.getSnapshot().entries.map(entry => entry.id)).toEqual(['input-1']);
    expect(mocks.get).toHaveBeenCalledTimes(1);
    expect(mocks.post).not.toHaveBeenCalled();
    expect(mocks.put).not.toHaveBeenCalled();
    queue.dispose();
  });
  it('publishes an earlier ordinary staged row with its original completion key and no local file', async () => {
    const stored = storedUpload('staged');
    mocks.readAll.mockImplementation((store: string) => Promise.resolve(store === 'uploads' ? [stored] : []));
    mocks.get.mockResolvedValue(upload('staged', file().size));
    const queue = await initialized();
    await vi.waitFor(() => expect(queue.getSnapshot().entries[0]).toMatchObject({ phase: 'ingested', assetId: 'asset-fixture' }));
    expect(mocks.post).toHaveBeenCalledTimes(1);
    expect(mocks.post.mock.calls[0]).toMatchObject(['/uploads/{id}/complete', { params: { header: { 'Idempotency-Key': 'stored-selection-complete' } } }]);
    expect(mocks.put).not.toHaveBeenCalled(); expect(mocks.digest).not.toHaveBeenCalled();
    queue.dispose();
  });
  it('keeps intelligent staged rows and unarchived source copies until their own work finishes', async () => {
    const stored = { ...storedUpload('staged', 'intelligent'), archiveBinding: 'synthetic-folder', archiveState: 'complete' as const, archiveNote:'同名文件已保留，本次未覆盖归档' };
    mocks.readAll.mockImplementation((store: string) => Promise.resolve(store === 'uploads' ? [stored] : []));
    const queue = await initialized();
    await queue.clearFinished();
    expect(queue.getSnapshot().entries[0]?.phase).toBe('staged');
    expect(queue.getSnapshot().entries[0]?.archiveState).toBe('failed');
    expect(mocks.post).not.toHaveBeenCalled(); expect(mocks.get).not.toHaveBeenCalled();
    queue.dispose();
  });
  it('requires a real asset ID before treating an ordinary upload as ingested', async () => {
    const stored = storedUpload('ingested');
    mocks.readAll.mockImplementation((store: string) => Promise.resolve(store === 'uploads' ? [stored] : []));
    mocks.get.mockResolvedValue({ ...upload('ingested', file().size), assetId: undefined });
    const queue = await initialized();
    await vi.waitFor(() => expect(queue.getSnapshot().entries[0]?.phase).toBe('failed'));
    expect(queue.getSnapshot().entries[0]?.error).toContain('资产标识');
    expect(mocks.post).not.toHaveBeenCalled(); expect(mocks.digest).not.toHaveBeenCalled();
    queue.dispose();
  });
  it('does not publish a later collection page while an earlier selection needs recovery', async () => {
    const next = new File(['second synthetic page'], 'second.jpg', { type: 'image/jpeg', lastModified: 1001 });
    let completes = 0;
    mocks.post.mockImplementation((path: string) => {
      if (path === '/batches') return Promise.resolve(batch);
      if (path === '/uploads') return Promise.resolve(upload());
      completes++;
      if (completes === 1) return Promise.reject(new Error('first page result unknown'));
      return Promise.resolve({ upload: { ...upload('ingested', next.size), size: next.size } });
    });
    mocks.put.mockImplementation((path: string, options: { body: Blob & { inputs?: unknown[] } }) => Promise.resolve(path === '/batches/{id}/inputs' ? sealed(options.body.inputs?.length ?? 0) : { ...upload('receiving', options.body.size), size: options.body.size }));
    const queue = await initialized();
    await queue.add([file(), next], { ...target, collectionId: 'synthetic-group' }, '合成库');
    await vi.waitFor(() => expect(queue.getSnapshot().entries[0]?.phase).toBe('failed'));
    expect(mocks.post.mock.calls.filter(call => call[0] === '/uploads')).toHaveLength(1);
    expect(queue.getSnapshot().entries[1]?.waitingFor).toBe(file().name);
    mocks.get.mockResolvedValue(upload('ingested', file().size));
    queue.retry(queue.getSnapshot().entries[0]!.id);
    await vi.waitFor(() => expect(queue.getSnapshot().entries[1]?.phase).toBe('ingested'));
    expect(completes).toBe(2);
    queue.dispose();
  });
  it("reconciles a lost completion response without creating or sending the file again", async () => {
    let completionAttempts = 0;
    mocks.post.mockImplementation((path: string) => {
      if (path === "/batches") return Promise.resolve(batch);
      if (path === "/uploads") return Promise.resolve(upload());
      completionAttempts++;
      return Promise.reject(new Error("completion response lost"));
    });
    const queue = await initialized();
    await queue.add([file()], target, "合成库");
    await vi.waitFor(() =>
      expect(queue.getSnapshot().entries[0]?.phase).toBe("failed"),
    );
    const entry = queue.getSnapshot().entries[0]!;
    mocks.get.mockResolvedValue(upload("ingested", file().size));
    queue.retry(entry.id);
    await vi.waitFor(() =>
      expect(queue.getSnapshot().entries[0]?.phase).toBe("ingested"),
    );
    expect(
      mocks.post.mock.calls.filter((call) => call[0] === "/uploads"),
    ).toHaveLength(1);
    expect(mocks.put.mock.calls.filter(call => call[0] === '/uploads/{id}/content')).toHaveLength(1);
    expect(completionAttempts).toBe(1);
    queue.dispose();
  });
  it("rejects a changed file even when its name, size and modification time match", async () => {
    const restored: QueueEntry = {
      id: "operation-fixture",
      scope: "instance-fixture:device-fixture",
      request: {
        ...target,
        fileName: file().name,
        size: file().size,
        mimeType: file().type,
      },
      libraryName: "合成库",
      name: file().name,
      size: file().size,
      modified: 1000,
      digest: "original-digest",
      uploadId: "upload-fixture",
      phase: "uploading",
      offset: 4,
      hashProgress: 0,
      createdAt: "2026-01-01T00:00:00Z",
    };
    mocks.readAll.mockImplementation((store: string) => Promise.resolve(store === 'uploads' ? [restored] : []));
    mocks.digest.mockResolvedValue("changed-digest");
    const queue = await initialized();
    queue.retry(restored.id, file());
    await vi.waitFor(() =>
      expect(queue.getSnapshot().entries[0]?.error).toContain("内容已改变"),
    );
    expect(mocks.put).not.toHaveBeenCalled();
    expect(mocks.get).not.toHaveBeenCalled();
    queue.dispose();
  });
  it("storage retry does not repeat an already committed business request", async () => {
    mocks.write.mockRejectedValue(new Error("quota exceeded"));
    mocks.writeGroup.mockRejectedValue(new Error('quota exceeded'));
    const queue = await initialized();
    await queue.add([file()], target, "合成库");
    await vi.waitFor(() =>
      expect(queue.getSnapshot().entries[0]?.phase).toBe("ingested"),
    );
    expect(queue.getSnapshot().persistenceError).toBeTruthy();
    const requests = mocks.post.mock.calls.length;
    mocks.write.mockResolvedValue(undefined);
    await queue.retryStorage();
    expect(queue.getSnapshot().persistenceError).toBeUndefined();
    expect(mocks.post).toHaveBeenCalledTimes(requests);
    queue.dispose();
  });
});

it('keeps the complete input list and operation identity after a lost seal response', async () => {
  const next = new File(['synthetic media'], 'second.jpg', { type: 'image/jpeg', lastModified: 1001 });
  let attempts = 0;
  mocks.put.mockImplementation((path: string, options: { body: { inputs?: unknown[] } }) => {
    if (path === '/batches/{id}/inputs') {
      attempts++;
      return attempts === 1 ? Promise.reject(new Error('manifest response lost')) : Promise.resolve(sealed(options.body.inputs!.length));
    }
    return Promise.resolve(upload('receiving', file().size));
  });
  const queue = await initialized();
  await expect(queue.add([file(), next], target, '合成库')).rejects.toThrow('manifest response lost');
  expect(mocks.post.mock.calls.filter(call => call[0] === '/uploads')).toHaveLength(0);
  const entries = queue.getSnapshot().entries;
  expect(entries.map(entry => entry.phase)).toEqual(['failed', 'failed']);
  queue.retry(entries[0]!.id);
  await vi.waitFor(() => expect(queue.getSnapshot().entries.every(entry => entry.phase === 'ingested')).toBe(true));
  const manifests = mocks.put.mock.calls.filter(call => call[0] === '/batches/{id}/inputs');
  expect(manifests[1]![1]).toMatchObject({ body: manifests[0]![1].body, params: manifests[0]![1].params });
  expect(manifests[0]![1].body.inputs.map((input: { id: string }) => input.id)).toEqual(entries.map(entry => entry.id));
  expect(mocks.post.mock.calls.filter(call => call[0] === '/uploads').every(call => entries.some(entry => entry.id === call[1].body.inputId))).toBe(true);
  queue.dispose();
});

it('restores an unconfirmed manifest without deriving its membership from reselected files', async () => {
  const stores = new Map<string, Map<string, { id: string }>>();
  const put = (store: string, item: { id: string } | string) => {
    const values = stores.get(store) ?? new Map();
    if (typeof item === 'string') values.delete(item); else values.set(item.id, structuredClone(item));
    stores.set(store, values);
  };
  mocks.write.mockImplementation(async (store: string, item: { id: string } | string) => put(store, item));
  mocks.writeGroup.mockImplementation(async (records: { store: string; item: { id: string } }[]) => records.forEach(record => put(record.store, record.item)));
  mocks.readAll.mockImplementation(async (store: string) => [...(stores.get(store)?.values() ?? [])]);
  mocks.put.mockRejectedValue(new Error('manifest response lost'));
  const queue = await initialized();
  const next = new File(['synthetic media'], 'second.jpg', { type: 'image/jpeg', lastModified: 1001 });
  await expect(queue.add([file(), next], target, '合成库')).rejects.toThrow();
  const firstId = queue.getSnapshot().entries[0]!.id;
  await vi.waitFor(() => expect(stores.get('uploads')?.size).toBe(2));
  queue.dispose();
  mocks.put.mockImplementation((path: string, options: { body: { inputs?: unknown[] } }) => Promise.resolve(path === '/batches/{id}/inputs' ? sealed(options.body.inputs!.length) : upload('receiving', file().size)));
  const restored = await initialized();
  restored.retry(firstId, file());
  await vi.waitFor(() => expect(restored.getSnapshot().entries.find(entry => entry.id === firstId)?.phase).toBe('ingested'));
  expect(restored.getSnapshot().entries.find(entry => entry.id !== firstId)?.phase).toBe('waiting_file');
  const last = mocks.put.mock.calls.filter(call => call[0] === '/batches/{id}/inputs').at(-1)!;
  expect(last[1].body.inputs.map((input: { originalFileName: string }) => input.originalFileName)).toEqual(['fixture.jpg', 'second.jpg']);
  restored.dispose();
});

it('cancels an input before hashing finishes without creating an unhashed upload', async () => {
  mocks.digest.mockImplementation((_file: File, signal: AbortSignal) => new Promise((_resolve, reject) => signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')), { once: true })));
  const queue = await initialized();
  await queue.add([file()], target, '合成库');
  await vi.waitFor(() => expect(queue.getSnapshot().entries[0]?.phase).toBe('hashing'));
  const entry = queue.getSnapshot().entries[0]!;
  await queue.cancel(entry.id);
  expect(queue.getSnapshot().entries[0]?.phase).toBe('cancelled');
  const cancelled = mocks.post.mock.calls.find(call => call[0] === '/batches/{id}/inputs/{inputId}/cancel')!;
  expect(cancelled[1]).toMatchObject({ params: { path: { inputId: entry.id, id: 'batch-fixture' } }, body: { manifestFingerprint: 'a'.repeat(64) } });
  expect(mocks.post.mock.calls.filter(call => call[0] === '/uploads')).toHaveLength(0);
  queue.dispose();
});

it('rejects more than one hundred picked inputs before registering anything', async () => {
  const queue = await initialized();
  await expect(queue.add(Array.from({ length: 101 }, file), target, '合成库')).rejects.toThrow('不会截取');
  expect(queue.getSnapshot().entries).toHaveLength(0);
  expect(mocks.post).not.toHaveBeenCalled();
  expect(mocks.writeGroup).not.toHaveBeenCalled();
  queue.dispose();
});

it('replays the same batch creation key after its acknowledgement is lost', async () => {
  let creations = 0;
  mocks.post.mockImplementation((path: string) => {
    if (path === '/batches') return ++creations === 1 ? Promise.reject(new Error('batch response lost')) : Promise.resolve(batch);
    if (path === '/uploads') return Promise.resolve(upload());
    return Promise.resolve({ upload: upload('ingested', file().size) });
  });
  const queue = await initialized();
  await expect(queue.add([file()], target, '合成库')).rejects.toThrow('batch response lost');
  queue.retry(queue.getSnapshot().entries[0]!.id);
  await vi.waitFor(() => expect(queue.getSnapshot().entries[0]?.phase).toBe('ingested'));
  const calls = mocks.post.mock.calls.filter(call => call[0] === '/batches');
  expect(calls).toHaveLength(2);
  expect(calls[1]![1]).toMatchObject({ body: calls[0]![1].body, params: calls[0]![1].params });
  queue.dispose();
});

it('a cancellation race reads an already published input without marking it cancelled', async () => {
  mocks.digest.mockImplementation((_file: File, signal: AbortSignal) => new Promise((_resolve, reject) => signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')), { once: true })));
  const queue = await initialized();
  await queue.add([file()], target, '合成库');
  await vi.waitFor(() => expect(queue.getSnapshot().entries[0]?.phase).toBe('hashing'));
  const entry = queue.getSnapshot().entries[0]!;
  mocks.post.mockRejectedValue(new Error('already published'));
  mocks.get.mockResolvedValue({ inputs: [{ id: entry.id, state: 'ingested', assetId: 'asset-fixture' }] });
  await queue.cancel(entry.id);
  expect(queue.getSnapshot().entries[0]).toMatchObject({ phase: 'ingested', assetId: 'asset-fixture' });
  queue.dispose();
});

it('does not register a batch after the session closes while its local snapshot is being saved', async () => {
  let finishStorage!: () => void;
  mocks.writeGroup.mockImplementation(() => new Promise<void>(resolve => { finishStorage = resolve; }));
  const queue = await initialized();
  const adding = queue.add([file()], target, '合成库');
  const rejected = expect(adding).rejects.toThrow('设备会话已离开');
  await vi.waitFor(() => expect(mocks.writeGroup).toHaveBeenCalled());
  queue.dispose();
  finishStorage();
  await rejected;
  expect(mocks.post).not.toHaveBeenCalled();
  expect(mocks.digest).not.toHaveBeenCalled();
});

it('cancels the existing selection serially without publishing queued rows', async () => {
  mocks.digest.mockImplementation((_file: File, signal: AbortSignal) => new Promise((_resolve, reject) => signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')), { once: true })));
  let active = 0;
  let maximum = 0;
  mocks.post.mockImplementation(async (path: string) => {
    if (path === '/batches') return batch;
    if (path === '/batches/{id}/inputs/{inputId}/cancel') {
      maximum = Math.max(maximum, ++active);
      await Promise.resolve();
      active--;
      return { state: 'cancelled' };
    }
    throw new Error('No file publication was authorised by this cancellation test');
  });
  const queue = await initialized();
  await queue.add([file(), file(), file()], target, '合成库');
  const cancelling = queue.cancelAll();
  expect(queue.cancelAll()).toBe(cancelling);
  await cancelling;
  expect(maximum).toBe(1);
  expect(queue.getSnapshot().entries.map(entry => entry.phase)).toEqual(['cancelled', 'cancelled', 'cancelled']);
  expect(mocks.post.mock.calls.filter(call => call[0] === '/uploads')).toHaveLength(0);
  queue.dispose();
});

it('aborts cancellation with the session and does not reconcile using a later session', async () => {
  mocks.digest.mockImplementation((_file: File, signal: AbortSignal) => new Promise((_resolve, reject) => signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')), { once: true })));
  const queue = await initialized();
  await queue.add([file()], target, '合成库');
  mocks.get.mockClear();
  mocks.post.mockImplementation((_path: string, options: { signal: AbortSignal }) => new Promise((_resolve, reject) => options.signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')), { once: true })));
  const cancelling = queue.cancel(queue.getSnapshot().entries[0]!.id);
  await vi.waitFor(() => expect(mocks.post.mock.calls.some(call => call[0] === '/batches/{id}/inputs/{inputId}/cancel')).toBe(true));
  queue.dispose();
  await cancelling;
  expect(mocks.get).not.toHaveBeenCalled();
});
