import { createContext, useContext, useEffect, useState, useSyncExternalStore } from 'react';
import type { ReactNode } from 'react';
import { EventLedger } from './event-ledger';
import { useSession } from './session';
import { Loading } from '../components/Feedback';

const Context = createContext<EventLedger | null>(null);
export function EventsProvider({ children }: { children: ReactNode }) {
  const { instance } = useSession();
  const [ledger, setLedger] = useState<EventLedger | null>(null);
  useEffect(() => {
    const owner = new EventLedger(instance.id); setLedger(owner);
    const online = () => { void owner.flush(); };
    window.addEventListener('online', online); void owner.flush();
    return () => { owner.dispose(); window.removeEventListener('online', online); };
  }, [instance.id]);
  return <Context.Provider value={ledger?.scope === instance.id ? ledger : null}>{ledger?.scope === instance.id ? children : <Loading text="正在准备浏览记录…" />}</Context.Provider>;
}
export function useEventLedger() { return useContext(Context); }
const empty: { pending: number; error?: string; storageError?: string } = { pending: 0 };
const idle = () => () => {};
export function EventSyncStatus() {
  const ledger = useEventLedger();
  const state = useSyncExternalStore(ledger?.subscribe ?? idle, ledger?.getSnapshot ?? (() => empty));
  if (!state.error && !state.storageError) return null;
  return <div className="connection-warning" role="status">{state.storageError ?? state.error} {state.pending} 条浏览记录等待处理。 <button className="button button--small" onClick={() => void ledger?.flush()}>重试补发</button></div>;
}
