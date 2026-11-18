import { useEffect, useRef, useState } from "react";

export function useNearViewport<T extends HTMLElement>(root: "main" | "viewport" = "main") {
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
        root: root === "main" ? document.querySelector("[data-scroll-root]") : null,
        rootMargin: "320px",
      },
    );
    observer.observe(element);
    return () => observer.disconnect();
  }, [visible, root]);
  return { ref, visible };
}
