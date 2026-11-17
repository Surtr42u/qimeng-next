import { useEffect, useRef } from "react";
import type { ReactNode } from "react";

export function ConfirmDialog({
  title,
  children,
  onConfirm,
  onCancel,
  busy = false,
  confirmLabel = "确认",
  danger = false,
}: {
  title: string;
  children: ReactNode;
  onConfirm: () => void;
  onCancel: () => void;
  busy?: boolean;
  confirmLabel?: string;
  danger?: boolean;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const cancel = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const previous = document.activeElement;
    dialog.current?.showModal();
    cancel.current?.focus();
    return () => {
      if (previous instanceof HTMLElement && previous.isConnected)
        previous.focus();
    };
  }, []);
  return (
    <dialog
      ref={dialog}
      className="dialog qm-glass"
      aria-labelledby="confirm-title"
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) onCancel();
      }}
    >
      <h2 id="confirm-title">{title}</h2>
      <div className="dialog-content">{children}</div>
      <div className="actions">
        <button
          ref={cancel}
          className="button"
          disabled={busy}
          onClick={onCancel}
        >
          取消
        </button>
        <button
          className={`button ${danger ? "button--danger" : "button--primary"}`}
          disabled={busy}
          onClick={onConfirm}
        >
          {busy ? "正在处理…" : confirmLabel}
        </button>
      </div>
    </dialog>
  );
}
