import { useEffect, useRef, useState } from "react";
import { Icon } from "./Icon";

type Point = { x: number; y: number };
export function ImageViewer({
  src,
  title,
  onClose,
}: {
  src: string;
  title: string;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const pointers = useRef(new Map<number, Point>());
  const pinch = useRef<{ distance: number; scale: number } | null>(null);
  const previous = useRef<Point | null>(null);
  const [view, setView] = useState({ scale: 1, x: 0, y: 0 });
  const changeZoom = (delta: number) =>
    setView((current) => ({
      ...current,
      scale: Math.max(1, Math.min(8, current.scale + delta)),
      ...(current.scale + delta <= 1 ? { x: 0, y: 0 } : {}),
    }));
  useEffect(() => {
    const focused = document.activeElement;
    const node = dialog.current;
    node?.showModal();
    const wheel = (event: WheelEvent) => {
      event.preventDefault();
      changeZoom(-event.deltaY / 600);
    };
    node?.addEventListener("wheel", wheel, { passive: false });
    const original = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      node?.removeEventListener("wheel", wheel);
      document.body.style.overflow = original;
      if (focused instanceof HTMLElement && focused.isConnected)
        focused.focus();
    };
  }, []);
  const reset = () => setView({ scale: 1, x: 0, y: 0 });
  return (
    <dialog
      ref={dialog}
      className="image-viewer"
      aria-label={title}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onKeyDown={(event) => {
        if (event.key === "+" || event.key === "=") changeZoom(0.25);
        if (event.key === "-") changeZoom(-0.25);
        if (event.key === "0") reset();
      }}
    >
      <div className="viewer-toolbar qm-glass">
        <span className="one-line">{title}</span>
        <div className="actions">
          <button
            className="icon-button"
            aria-label="缩小"
            onClick={() => changeZoom(-0.25)}
          >
            −
          </button>
          <button className="button button--small" onClick={reset}>
            {Math.round(view.scale * 100)}%
          </button>
          <button
            className="icon-button"
            aria-label="放大"
            onClick={() => changeZoom(0.25)}
          >
            ＋
          </button>
          <button
            className="icon-button"
            aria-label="关闭原图"
            onClick={onClose}
            autoFocus
          >
            <Icon name="close" />
          </button>
        </div>
      </div>
      <div
        className="viewer-canvas"
        onDoubleClick={() => (view.scale > 1 ? reset() : changeZoom(1))}
        onPointerDown={(event) => {
          event.currentTarget.setPointerCapture(event.pointerId);
          pointers.current.set(event.pointerId, {
            x: event.clientX,
            y: event.clientY,
          });
          previous.current = { x: event.clientX, y: event.clientY };
          const pair = [...pointers.current.values()];
          if (pair[0] && pair[1])
            pinch.current = {
              distance: Math.hypot(
                pair[1].x - pair[0].x,
                pair[1].y - pair[0].y,
              ),
              scale: view.scale,
            };
        }}
        onPointerMove={(event) => {
          if (!pointers.current.has(event.pointerId)) return;
          pointers.current.set(event.pointerId, {
            x: event.clientX,
            y: event.clientY,
          });
          const pair = [...pointers.current.values()];
          if (pair[0] && pair[1] && pinch.current) {
            const scale = Math.max(
              1,
              Math.min(
                8,
                (pinch.current.scale *
                  Math.hypot(pair[1].x - pair[0].x, pair[1].y - pair[0].y)) /
                  Math.max(1, pinch.current.distance),
              ),
            );
            setView((current) => ({ ...current, scale }));
          } else if (previous.current) {
            const dx = event.clientX - previous.current.x;
            const dy = event.clientY - previous.current.y;
            setView((current) =>
              current.scale > 1
                ? { ...current, x: current.x + dx, y: current.y + dy }
                : current,
            );
          }
          previous.current = { x: event.clientX, y: event.clientY };
        }}
        onPointerUp={(event) => {
          pointers.current.delete(event.pointerId);
          pinch.current = null;
          previous.current = null;
        }}
        onPointerCancel={(event) => {
          pointers.current.delete(event.pointerId);
          pinch.current = null;
          previous.current = null;
        }}
      >
        <img
          src={src}
          alt={title}
          draggable={false}
          style={{
            transform: `translate(${view.x}px,${view.y}px) scale(${view.scale})`,
          }}
        />
      </div>
      <p className="viewer-hint">双击或双指缩放 · 拖动查看 · Esc 关闭</p>
    </dialog>
  );
}
