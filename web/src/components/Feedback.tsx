import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { ApiError } from "../lib/errors";

export function ErrorNotice({
  error,
  retry,
  stale = false,
}: {
  error: unknown;
  retry?: () => void;
  stale?: boolean;
}) {
  if (!error) return null;
  const conflict = error instanceof ApiError && error.status === 409;
  return (
    <div
      className={`notice ${conflict ? "notice--warning" : "notice--error"}`}
      role="alert"
    >
      <strong>
        {conflict
          ? "内容已发生变化"
          : stale
            ? "更新失败，以下为上次读取的内容"
            : "未能完成操作"}
      </strong>
      <p>{error instanceof Error ? error.message : "发生未知错误，请重试。"}</p>
      {conflict && (
        <p>
          你的草稿已保留。先读取当前版本并核对，再重新保存；不会自动覆盖其他设备的修改。
        </p>
      )}
      {retry && (
        <button className="button button--small" onClick={retry}>
          重试
        </button>
      )}
    </div>
  );
}

export function EmptyState({
  title,
  children,
  action,
}: {
  title: string;
  children?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="empty-state">
      <span className="empty-orbit" aria-hidden="true" />
      <h2>{title}</h2>
      {children && <p>{children}</p>}
      {action}
    </div>
  );
}

export function Loading({ text = "正在读取…" }: { text?: string }) {
  return (
    <div className="loading" role="status">
      <span className="spinner" aria-hidden="true" />
      {text}
    </div>
  );
}

export function PageHeading({
  title,
  subtitle,
  action,
}: {
  title: string;
  subtitle?: string;
  action?: ReactNode;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    if (document.activeElement === document.body) heading.current?.focus({ preventScroll: true });
  }, []);
  return (
    <div className="page-heading">
      <div>
        <h1 ref={heading} tabIndex={-1} data-page-heading>
          {title}
        </h1>
        {subtitle && <p>{subtitle}</p>}
      </div>
      {action}
    </div>
  );
}

export function StatusBadge({
  children,
  tone = "neutral",
}: {
  children: ReactNode;
  tone?: "neutral" | "success" | "warning" | "danger";
}) {
  return <span className={`badge badge--${tone}`}>{children}</span>;
}
