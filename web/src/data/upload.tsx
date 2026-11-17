import {
  createContext,
  useContext,
  useEffect,
  useState,
  useSyncExternalStore,
} from "react";
import type { ReactNode } from "react";
import { useBeforeUnload } from "react-router-dom";
import { UploadQueue } from "./upload-queue";
import { useSession } from "./session";
import { useSettings } from "./manage";

const Context = createContext<UploadQueue | null>(null);
export function UploadProvider({ children }: { children: ReactNode }) {
  const { instance, session } = useSession();
  const settings = useSettings();
  const scope = `${instance.id}:${session.id}`;
  const [queue, setQueue] = useState<UploadQueue | null>(null);
  const snapshot = useSyncExternalStore(
    queue?.subscribe ?? subscribeEmpty,
    queue?.getSnapshot ?? (() => empty),
  );
  useBeforeUnload((event) => {
    if (
      snapshot.entries.some((entry) =>
        ["queued", "hashing", "uploading", "completing"].includes(entry.phase),
      )
    ) {
      event.preventDefault();
      event.returnValue = "";
    }
  });
  const chunkSize = settings.data?.maxChunkBytes;
  const maxItems = settings.data?.maxBatchItems;
  const concurrency = Math.min(2, settings.data?.maxActiveUploads ?? 1);
  useEffect(() => {
    if (!chunkSize || !maxItems) return;
    const owner = new UploadQueue(scope, chunkSize, maxItems, concurrency);
    setQueue(owner);
    return () => owner.dispose();
  }, [scope, chunkSize, maxItems, concurrency]);
  return (
    <Context.Provider value={queue?.scope === scope ? queue : null}>
      {children}
    </Context.Provider>
  );
}
const empty = { entries: [], initialized: false, preparing: false } as const;
const subscribeEmpty = () => () => {};
export function useUploadQueue() {
  const queue = useContext(Context);
  const state = useSyncExternalStore(
    queue?.subscribe ?? subscribeEmpty,
    queue?.getSnapshot ?? (() => empty),
  );
  return { queue, ...state };
}
