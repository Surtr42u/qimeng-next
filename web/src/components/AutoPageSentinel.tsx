import { useEffect, useRef } from "react";

export function AutoPageSentinel({
  enabled,
  loading,
  failed,
  load,
}: {
  enabled: boolean;
  loading: boolean;
  failed: boolean;
  load: () => Promise<unknown>;
}) {
  const element = useRef<HTMLDivElement>(null);
  const inFlight = useRef(false);
  const currentLoad = useRef(load);
  currentLoad.current = load;
  useEffect(() => {
    const target = element.current;
    if (!target || !enabled || loading || failed) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((entry) => entry.isIntersecting) || inFlight.current)
          return;
        inFlight.current = true;
        void currentLoad.current().then(
          () => {
            inFlight.current = false;
          },
          () => {
            inFlight.current = false; /* The owning query renders its failure and retry action. */
          },
        );
      },
      { rootMargin: "180px 0px" },
    );
    observer.observe(target);
    // Re-observe after each page settles, including an empty page with a continuation cursor.
    return () => observer.disconnect();
  }, [enabled, loading, failed]);
  return <div ref={element} aria-hidden="true" style={{ height: 1 }} />;
}
