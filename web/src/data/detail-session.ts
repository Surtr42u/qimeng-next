import { useCallback, useEffect, useRef, useState } from 'react';
import { useEventLedger } from './events';
import type { Schema } from './client';
import { VisibleClock } from '../lib/dwell';
import { newId } from '../lib/id';

export function useDetailSession(assetId: string, contentVersion: number) {
  const ledger = useEventLedger();
  const sessionId = useRef(newId());
  const startedAt = useRef(new Date().toISOString());
  const [error, setError] = useState<unknown>(null);
  const failed = useRef<Schema<'EventCreate'> | null>(null);
  const send = useCallback((body: Schema<'EventCreate'>) => {
    if (!ledger) return;
    void ledger.record(assetId, body).then(() => { failed.current = null; setError(null); }, failure => { failed.current = body; setError(failure); });
  }, [assetId, ledger]);
  useEffect(() => {
    if (!ledger) return;
    const session = sessionId.current;
    const clock = new VisibleClock();
    send({ eventId: `${session}-view`, kind: 'view', contentVersion, sessionId: session, occurredAt: startedAt.current });
    const emit = (segment: ReturnType<VisibleClock['take']>) => { if (segment) send({ eventId: newId(), kind: 'dwell', contentVersion, sessionId: session, ...segment }); };
    const visibility = () => { if (document.visibilityState === 'visible') clock.resume(performance.now(), Date.now()); else emit(clock.pause(performance.now())); };
    visibility();
    const timer = window.setInterval(() => { emit(clock.take(performance.now())); }, 15000);
    const pageHide = () => emit(clock.pause(performance.now()));
    document.addEventListener('visibilitychange', visibility); window.addEventListener('pagehide', pageHide);
    return () => { clearInterval(timer); emit(clock.pause(performance.now())); document.removeEventListener('visibilitychange', visibility); window.removeEventListener('pagehide', pageHide); };
  }, [assetId, contentVersion, ledger, send]);
  return {
    error, retry: () => { if (failed.current) send(failed.current); },
    play: () => send({ eventId: `${sessionId.current}-play`, kind: 'play', contentVersion, sessionId: sessionId.current, occurredAt: startedAt.current }),
    progress: (positionMs: number) => send({ eventId: newId(), kind: 'progress', contentVersion, positionMs, sessionId: sessionId.current, occurredAt: new Date().toISOString() }),
  };
}
