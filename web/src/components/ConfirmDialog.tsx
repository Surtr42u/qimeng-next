import type { ReactNode } from "react";
import { AlertDialog } from "radix-ui";

// The existing confirm-* presentation is retained; mutation lifetime belongs to the caller.
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
  return (
    <AlertDialog.Root
      open
      onOpenChange={(open) => {
        if (!open && !busy) onCancel();
      }}
    >
      <AlertDialog.Portal>
        <AlertDialog.Overlay className="confirm-overlay" />
        <AlertDialog.Content className="confirm-panel">
          <AlertDialog.Title className="confirm-title">
            {title}
          </AlertDialog.Title>
          <AlertDialog.Description asChild>
            <div className="confirm-desc">{children}</div>
          </AlertDialog.Description>
          <div className="confirm-actions">
            <AlertDialog.Cancel
              className="confirm-btn confirm-btn--cancel"
              disabled={busy}
            >
              取消
            </AlertDialog.Cancel>
            <AlertDialog.Action
              className={`confirm-btn ${danger ? "confirm-btn--danger" : "confirm-btn--primary"}`}
              disabled={busy}
              onClick={(event) => {
                event.preventDefault();
                onConfirm();
              }}
            >
              {busy ? "正在处理…" : confirmLabel}
            </AlertDialog.Action>
          </div>
        </AlertDialog.Content>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
