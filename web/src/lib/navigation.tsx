import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
} from "react";
import type { ReactNode } from "react";
import {
  useBeforeUnload,
  useBlocker,
  useLocation,
  useNavigationType,
} from "react-router-dom";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { backdropLocation, isAssetDetail } from "./detail-navigation";

const positions = new Map<
  string,
  { x: number; y: number; focusHref?: string }
>();
export function usePageRestoration() {
  const location = useLocation();
  const direction = useNavigationType();
  const key = location.key;
  const currentKey = useRef(key);
  const previousPath = useRef(location.pathname);
  const previousState = useRef(location.state);
  useLayoutEffect(() => {
    history.scrollRestoration = "manual";
    const pathChanged = previousPath.current !== location.pathname;
    const overlay = isAssetDetail(location.pathname);
    const returningToList = isAssetDetail(previousPath.current) && backdropLocation(previousState.current).pathname === location.pathname;
    previousPath.current = location.pathname;
    previousState.current = location.state;
    currentKey.current = key;
    const saved = direction === "POP" ? positions.get(key) : undefined;
    let cancelled = false;
    let observer: ResizeObserver | undefined;
    const container = document.querySelector<HTMLElement>("[data-scroll-root]");
    if (overlay || returningToList) {
      const frame = requestAnimationFrame(() => {
        if (overlay) document.querySelector<HTMLElement>(".asset-overlay [data-page-heading]")?.focus({ preventScroll: true });
        else if (saved?.focusHref) Array.from(document.querySelectorAll<HTMLAnchorElement>(".overlay-backdrop a[href]")).find(link => link.getAttribute("href") === saved.focusHref)?.focus({ preventScroll: true });
      });
      return () => cancelAnimationFrame(frame);
    }
    const restore = () => {
      if (cancelled) return;
      (container ?? window).scrollTo({
        left: saved?.x ?? 0,
        top: saved?.y ?? 0,
        behavior: "instant",
      });
      const focusTarget = saved?.focusHref
        ? Array.from(document.links).find(
            (link) => link.getAttribute("href") === saved.focusHref,
          )
        : undefined;
      if (
        saved &&
        Math.abs((container?.scrollTop ?? window.scrollY) - saved.y) <= 1 &&
        (!saved.focusHref || focusTarget)
      ) {
        focusTarget?.focus({ preventScroll: true });
        cancelled = true;
        observer?.disconnect();
      }
    };
    restore();
    // Cached lists normally restore immediately; delayed image layout can enlarge the document afterwards.
    observer = new ResizeObserver(restore);
    if (saved && !cancelled)
      observer.observe(
        container?.firstElementChild ?? document.body,
      );
    const cancel = () => {
      cancelled = true;
      observer?.disconnect();
    };
    window.addEventListener("wheel", cancel, { once: true, passive: true });
    window.addEventListener("touchstart", cancel, {
      once: true,
      passive: true,
    });
    window.addEventListener("keydown", cancel, { once: true });
    const focusFrame = requestAnimationFrame(() => {
      if (direction !== "POP" && pathChanged)
        document
          .querySelector<HTMLElement>("[data-page-heading]")
          ?.focus({ preventScroll: true });
    });
    return () => {
      cancel();
      cancelAnimationFrame(focusFrame);
      window.removeEventListener("wheel", cancel);
      window.removeEventListener("touchstart", cancel);
      window.removeEventListener("keydown", cancel);
    };
  }, [key, direction, location.pathname]);
  useEffect(() => {
    const container = document.querySelector<HTMLElement>("[data-scroll-root]");
    const save = () => {
      const focused =
        document.activeElement?.closest<HTMLAnchorElement>("a[href]");
      positions.set(currentKey.current, {
        x: container?.scrollLeft ?? window.scrollX,
        y: container?.scrollTop ?? window.scrollY,
        focusHref:
          focused?.getAttribute("href") ??
          positions.get(currentKey.current)?.focusHref,
      });
      if (positions.size > 64) {
        const oldest = positions.keys().next().value;
        if (oldest) positions.delete(oldest);
      }
    };
    (container ?? window).addEventListener("scroll", save, { passive: true });
    document.addEventListener("focusin", save);
    return () => {
      (container ?? window).removeEventListener("scroll", save);
      document.removeEventListener("focusin", save);
    };
  }, []);
}

const DirtyContext = createContext<
  ((id: string, dirty: boolean) => void) | null
>(null);
export function UnsavedProvider({ children }: { children: ReactNode }) {
  // The router can navigate in a child effect immediately after a save. Read the
  // registered layout state directly instead of waiting for a parent render/effect.
  const drafts = useRef(new Set<string>());
  const setDirty = useCallback((id: string, dirty: boolean) => {
    if (dirty) drafts.current.add(id); else drafts.current.delete(id);
  }, []);
  const blocker = useBlocker(useCallback(() => drafts.current.size > 0, []));
  useBeforeUnload(useCallback((event: BeforeUnloadEvent) => {
    if (drafts.current.size) { event.preventDefault(); event.returnValue = ""; }
  }, []));
  return (
    <DirtyContext.Provider value={setDirty}>
      {children}
      {blocker.state === "blocked" && (
        <ConfirmDialog
          title="离开未保存的编辑？"
          confirmLabel="放弃并离开"
          onCancel={() => blocker.reset()}
          onConfirm={() => blocker.proceed()}
        >
          <p>尚未保存的修改会丢失。</p>
        </ConfirmDialog>
      )}
    </DirtyContext.Provider>
  );
}
export function UnsavedGuard({ dirty }: { dirty: boolean }) {
  const id = useId();
  const update = useContext(DirtyContext);
  useLayoutEffect(() => {
    update?.(id, dirty);
    return () => update?.(id, false);
  }, [id, dirty, update]);
  return null;
}
