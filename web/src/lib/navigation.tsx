import { useEffect, useLayoutEffect, useRef } from 'react';
import { useBeforeUnload, useBlocker, useLocation, useNavigationType } from 'react-router-dom';
import { ConfirmDialog } from '../components/ConfirmDialog';

const positions = new Map<string, { x: number; y: number }>();
export function usePageRestoration() {
  const location = useLocation();
  const direction = useNavigationType();
  const key = location.key;
  const currentKey = useRef(key);
  useLayoutEffect(() => {
    history.scrollRestoration = 'manual';
    currentKey.current = key;
    const saved = direction === 'POP' ? positions.get(key) : undefined;
    let cancelled = false;
    const restore = () => { if (!cancelled) window.scrollTo({ left: saved?.x ?? 0, top: saved?.y ?? 0, behavior: 'instant' }); };
    restore();
    // Cached lists normally restore immediately; delayed image layout can enlarge the document afterwards.
    const observer = new ResizeObserver(restore);
    if (saved) observer.observe(document.body);
    const cancel = () => { cancelled = true; observer.disconnect(); };
    window.addEventListener('wheel', cancel, { once: true, passive: true });
    window.addEventListener('touchstart', cancel, { once: true, passive: true });
    window.addEventListener('keydown', cancel, { once: true });
    const timer = window.setTimeout(cancel, 2500);
    const focusFrame = requestAnimationFrame(() => { if (direction !== 'POP') document.querySelector<HTMLElement>('[data-page-heading]')?.focus({ preventScroll: true }); });
    return () => { cancel(); clearTimeout(timer); cancelAnimationFrame(focusFrame); window.removeEventListener('wheel', cancel); window.removeEventListener('touchstart', cancel); window.removeEventListener('keydown', cancel); };
  }, [key, direction]);
  useEffect(() => {
    const save = () => {
      positions.set(currentKey.current, { x: window.scrollX, y: window.scrollY });
      if (positions.size > 64) { const oldest = positions.keys().next().value; if (oldest) positions.delete(oldest); }
    };
    window.addEventListener('scroll', save, { passive: true });
    return () => window.removeEventListener('scroll', save);
  }, []);
}

export function UnsavedGuard({ dirty }: { dirty: boolean }) {
  const blocker = useBlocker(dirty);
  useBeforeUnload(event => { if (dirty) { event.preventDefault(); event.returnValue = ''; } });
  if (blocker.state !== 'blocked') return null;
  return <ConfirmDialog title="离开未保存的编辑？" confirmLabel="放弃并离开" onCancel={() => blocker.reset()} onConfirm={() => blocker.proceed()}><p>尚未保存的修改会丢失。</p></ConfirmDialog>;
}
