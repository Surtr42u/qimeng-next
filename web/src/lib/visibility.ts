import { useEffect, useRef, useState } from "react";

export function useNearViewport<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    const element = ref.current;
    if (!element || visible) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          setVisible(true);
          observer.disconnect();
        }
      },
      {
        root: document.querySelector("[data-scroll-root]"),
        rootMargin: "320px",
      },
    );
    observer.observe(element);
    return () => observer.disconnect();
  }, [visible]);
  return { ref, visible };
}
