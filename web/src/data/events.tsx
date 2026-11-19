import {
  createContext,
  useContext,
  useEffect,
  useState,
  useSyncExternalStore,
} from "react";
import type { ReactNode } from "react";
import { EventLedger } from "./event-ledger";
import { useSession } from "./session";
import { Loading } from "../components/Feedback";

const Context = createContext<EventLedger | null>(null);
export function EventsProvider({ children }: { children: ReactNode }) {
  const { instance } = useSession();
  const [ledger, setLedger] = useState<EventLedger | null>(null);
  useEffect(() => {
    const owner = new EventLedger(instance.id);
    setLedger(owner);
    const online = () => {
      void owner.flush();
    };
    window.addEventListener("online", online);
    void owner.flush();
    return () => {
      owner.dispose();
      window.removeEventListener("online", online);
    };
  }, [instance.id]);
  return (
    <Context.Provider value={ledger?.scope === instance.id ? ledger : null}>
      {ledger?.scope === instance.id ? (
        children
      ) : (
        <Loading text="正在准备浏览记录…" />
      )}
    </Context.Provider>
  );
}
export function useEventLedger() {
  return useContext(Context);
}
const empty: ReturnType<EventLedger["getSnapshot"]> = { pending: 0, rejected: [] };
const kindLabels = { view: "浏览", play: "播放", progress: "续播进度", dwell: "停留" };
const idle = () => () => {};
export function EventSyncStatus() {
  const ledger = useEventLedger();
  const [clearError, setClearError] = useState("");
  const state = useSyncExternalStore(
    ledger?.subscribe ?? idle,
    ledger?.getSnapshot ?? (() => empty),
  );
  if (!state.error && !state.storageError && !state.rejected.length) return null;
  const clear = async (id: string) => {
    try {
      await ledger?.clearRejected(id);
      setClearError("");
    } catch {
      setClearError("清除失败，记录仍保留在本机，请重试清除。");
    }
  };
  return (
    <div className="connection-warning" role="status">
      {(state.error || state.storageError) && <div>
        {state.storageError} {state.error} {state.pending > 0 && `${state.pending} 条浏览记录等待处理。`}{" "}
        {(state.pending > 0 || state.storageError) && <button className="button button--small" onClick={() => void ledger?.flush()}>
          {state.pending > 0 ? "重试补发" : "重试本机存储"}
        </button>}
      </div>}
      {state.rejected.length > 0 && <details>
        <summary>服务器未接收 {state.rejected.length} 条浏览记录</summary>
        <ul>{state.rejected.map(entry => <li key={entry.id} className="break-word">
          {kindLabels[entry.body.kind]} · 媒体 {entry.assetId} · 内容版本 {entry.body.contentVersion} · 事件 {entry.body.eventId}
          <div>{entry.rejection.message}（{entry.rejection.code}）{" "}
            <button className="button button--small" onClick={() => void clear(entry.id)}>清除此条记录</button>
          </div>
        </li>)}</ul>
        {clearError && <p>{clearError}</p>}
      </details>}
    </div>
  );
}
