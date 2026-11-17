let database: Promise<IDBDatabase> | undefined;
function open() {
  if (!database)
    database = new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open("qimeng-device-state", 1);
      request.onupgradeneeded = () => {
        request.result.createObjectStore("uploads", { keyPath: "id" });
        request.result.createObjectStore("events", { keyPath: "id" });
      };
      request.onsuccess = () => {
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
        database = undefined;
        reject(new Error("请关闭其他旧版影库标签页后重试存储"));
      };
    });
  return database;
}
export async function localReadAll<T>(
  store: "uploads" | "events",
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
  store: "uploads" | "events",
  item: { id: string } | string,
) {
  const db = await open();
  return new Promise<void>((resolve, reject) => {
    const transaction = db.transaction(store, "readwrite");
    if (typeof item === "string") transaction.objectStore(store).delete(item);
    else transaction.objectStore(store).put(item);
    // Request success is not transaction success: quota errors can still abort afterwards.
    transaction.oncomplete = () => resolve();
    transaction.onabort = () =>
      reject(transaction.error ?? new Error("本地写入被中止"));
    transaction.onerror = () => reject(transaction.error);
  });
}
