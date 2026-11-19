let database: Promise<IDBDatabase> | undefined;
export type DeviceStore = 'uploads' | 'events' | 'upload_batches' | 'maintenance_folders';
function open() {
  if (!database)
    database = new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open("qimeng-device-state", 3);
      let abandoned = false;
      request.onupgradeneeded = () => {
        for (const store of ['uploads', 'events', 'upload_batches', 'maintenance_folders']) {
          if (!request.result.objectStoreNames.contains(store)) request.result.createObjectStore(store, { keyPath: 'id' });
        }
      };
      request.onsuccess = () => {
        if (abandoned) { request.result.close(); return; }
        request.result.onversionchange = () => {
          request.result.close();
          database = undefined;
        };
        resolve(request.result);
      };
      request.onerror = () => {
        database = undefined;
        reject(request.error);
      };
      request.onblocked = () => {
        abandoned = true;
        database = undefined;
        reject(new Error("请关闭其他旧版影库标签页后重试存储"));
      };
    });
  return database;
}
export async function localRead<T>(store: DeviceStore, id: string): Promise<T | undefined> {
  const db = await open();
  return new Promise((resolve, reject) => {
    const transaction = db.transaction(store, "readonly");
    const request = transaction.objectStore(store).get(id);
    transaction.oncomplete = () => resolve(request.result as T | undefined);
    transaction.onabort = () => reject(transaction.error ?? new Error("本地读取被中止"));
    transaction.onerror = () => reject(transaction.error);
  });
}
export async function localReadAll<T>(
  store: DeviceStore,
): Promise<T[]> {
  const db = await open();
  return new Promise((resolve, reject) => {
    const transaction = db.transaction(store, "readonly");
    const request = transaction.objectStore(store).getAll();
    transaction.oncomplete = () => resolve(request.result as T[]);
    transaction.onabort = () =>
      reject(transaction.error ?? new Error("本地读取被中止"));
    transaction.onerror = () => reject(transaction.error);
  });
}
export async function localWrite(
  store: DeviceStore,
  item: { id: string } | string,
) {
  const db = await open();
  return new Promise<void>((resolve, reject) => {
    const transaction = db.transaction(store, "readwrite");
    if (typeof item === "string") transaction.objectStore(store).delete(item);
    else transaction.objectStore(store).put(item);
    // Settle on the transaction outcome. On explicit abort, pending requests can
    // emit error before the abort event while transaction.error is still null.
    transaction.oncomplete = () => resolve();
    transaction.onabort = () =>
      reject(transaction.error ?? new Error("本地写入被中止"));
  });
}

/** A picked input list and its queue rows become recoverable in the same transaction. */
export async function localWriteGroup(records: readonly { store: DeviceStore; item: { id: string } | string }[]) {
  if (!records.length) return;
  const db = await open();
  return new Promise<void>((resolve, reject) => {
    const transaction = db.transaction([...new Set(records.map(record => record.store))], 'readwrite');
    for (const record of records) {
      const store = transaction.objectStore(record.store);
      if (typeof record.item === 'string') store.delete(record.item); else store.put(record.item);
    }
    transaction.oncomplete = () => resolve();
    transaction.onabort = () => reject(transaction.error ?? new Error('本地批次写入被中止'));
    transaction.onerror = () => reject(transaction.error);
  });
}
