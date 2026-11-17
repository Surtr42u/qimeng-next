import { useLayoutEffect, useState } from "react";
import type { RefObject } from "react";

/** Material-only enhancement for an existing navigation host; does not arrange its children. */
export function SelectionLens({
  host,
  selected,
  selectionKey,
}: {
  host: RefObject<HTMLElement | null>;
  selected: string;
  selectionKey: string;
}) {
  const [rect, setRect] = useState<{
    x: number;
    y: number;
    width: number;
    height: number;
  }>();
  useLayoutEffect(() => {
    const root = host.current;
    if (!root) return;
    const measure = () => {
      const item = root.querySelector<HTMLElement>(selected);
      if (!item) {
        setRect(undefined);
        return;
      }
      const bounds = root.getBoundingClientRect();
      const target = item.getBoundingClientRect();
      const scale = root.offsetWidth ? bounds.width / root.offsetWidth : 1;
      setRect({
        x: (target.left - bounds.left) / scale - root.clientLeft,
        y: (target.top - bounds.top) / scale - root.clientTop,
        width: target.width / scale,
        height: target.height / scale,
      });
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(root);
    for (const element of root.querySelectorAll<HTMLElement>("a,button"))
      observer.observe(element);
    return () => observer.disconnect();
  }, [host, selected, selectionKey]);
  return (
    <span
      aria-hidden="true"
      className="qm-selection-lens"
      hidden={!rect}
      style={
        rect
          ? {
              width: rect.width,
              height: rect.height,
              transform: `translate3d(${rect.x}px,${rect.y}px,0)`,
            }
          : undefined
      }
    />
  );
}
