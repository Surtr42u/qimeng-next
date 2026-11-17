import { useRef, useState } from "react";
import { useTimeline, useEditTimeline } from "../data/catalog";
import type { Schema } from "../data/client";
import {
  EmptyState,
  ErrorNotice,
  Loading,
  StatusBadge,
} from "../components/Feedback";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { UnsavedGuard } from "../lib/navigation";
import { duration } from "../lib/format";
import { newId } from "../lib/id";

export function TimelinePanel({
  assetId,
  contentVersion,
  currentPosition,
  onSeek,
}: {
  assetId: string;
  contentVersion: number;
  currentPosition: () => number;
  onSeek: (milliseconds: number) => void;
}) {
  const query = useTimeline(assetId);
  const mutation = useEditTimeline(assetId);
  const [editing, setEditing] = useState<Schema<"TimelineMark"> | null>();
  const [label, setLabel] = useState("");
  const [position, setPosition] = useState(0);
  const [color, setColor] = useState(
    getComputedStyle(document.documentElement)
      .getPropertyValue("--qm-accent")
      .trim(),
  );
  const [deleting, setDeleting] = useState<Schema<"TimelineMark">>();
  const [discard, setDiscard] = useState(false);
  const attempt = useRef({ signature: "", key: newId() });
  const items = query.data?.pages.flatMap((page) => page.items) ?? [];
  const dirty =
    editing !== undefined &&
    (label !== (editing?.label ?? "") ||
      position !== (editing?.positionMs ?? 0) / 1000 ||
      color !== (editing?.color ?? ""));
  const begin = (mark: Schema<"TimelineMark"> | null) => {
    setEditing(mark);
    setLabel(mark?.label ?? "");
    setPosition((mark?.positionMs ?? currentPosition()) / 1000);
    if (mark?.color) setColor(mark.color);
    mutation.reset();
  };
  const keyFor = (signature: string) => {
    if (attempt.current.signature !== signature)
      attempt.current = { signature, key: newId() };
    return attempt.current.key;
  };
  return (
    <section className="panel section-space">
      <div className="section-heading">
        <h2>时间轴标记</h2>
        <button
          className="button button--small"
          disabled={editing !== undefined}
          onClick={() => begin(null)}
        >
          标记当前时间
        </button>
      </div>
      <ErrorNotice
        error={query.error}
        stale={items.length > 0}
        retry={() => void query.refetch()}
      />
      {query.isPending ? (
        <Loading />
      ) : items.length ? (
        <div className="timeline-list">
          {items.map((mark) => (
            <div
              className="list-row"
              key={mark.id}
              style={{
                borderLeft: `3px solid ${mark.color || "var(--qm-accent)"}`,
              }}
            >
              <button
                className="button button--small"
                disabled={mark.contentVersion !== contentVersion}
                onClick={() => onSeek(mark.positionMs)}
              >
                {duration(mark.positionMs)}
              </button>
              <div className="row-main">
                <strong>{mark.label}</strong>
                {mark.contentVersion !== contentVersion && (
                  <StatusBadge tone="warning">
                    旧内容版本 {mark.contentVersion}，未自动映射
                  </StatusBadge>
                )}
              </div>
              <div className="actions">
                <button
                  className="button button--small"
                  disabled={editing !== undefined}
                  onClick={() => begin(mark)}
                >
                  编辑
                </button>
                <button
                  className="button button--small button--danger"
                  disabled={editing !== undefined}
                  onClick={() => {
                    mutation.reset();
                    setDeleting(mark);
                  }}
                >
                  删除
                </button>
              </div>
            </div>
          ))}
        </div>
      ) : (
        !query.isError && <p className="muted">还没有时间轴标记。</p>
      )}
      {query.hasNextPage && (
        <button
          className="button"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
        >
          加载更多标记
        </button>
      )}
      {editing !== undefined && (
        <form
          className="form-stack section-space"
          onSubmit={(event) => {
            event.preventDefault();
            const body = {
              positionMs: Math.round(position * 1000),
              label,
              color,
              contentVersion: editing?.contentVersion ?? contentVersion,
            };
            mutation.mutate(
              {
                mark: editing ?? undefined,
                body,
                key: keyFor(
                  JSON.stringify([editing?.id, editing?.revision, body]),
                ),
              },
              { onSuccess: () => setEditing(undefined) },
            );
          }}
        >
          <label>
            标记文字
            <input
              value={label}
              maxLength={200}
              required
              disabled={mutation.isPending}
              onChange={(event) => setLabel(event.target.value)}
            />
          </label>
          <div className="inline-form">
            <label>
              秒
              <input
                type="number"
                min={0}
                step={0.001}
                value={position}
                disabled={mutation.isPending}
                onChange={(event) => setPosition(event.target.valueAsNumber)}
                required
              />
            </label>
            <label>
              颜色
              <input
                type="color"
                value={color}
                disabled={mutation.isPending}
                onChange={(event) => setColor(event.target.value)}
              />
            </label>
          </div>
          <ErrorNotice error={mutation.error} />
          <div className="actions">
            <button
              className="button button--primary"
              disabled={
                mutation.isPending ||
                !label.trim() ||
                !Number.isFinite(position)
              }
            >
              保存标记
            </button>
            <button
              type="button"
              className="button"
              disabled={mutation.isPending}
              onClick={() => (dirty ? setDiscard(true) : setEditing(undefined))}
            >
              取消
            </button>
          </div>
        </form>
      )}
      {deleting && (
        <ConfirmDialog
          title="删除此时间轴标记？"
          danger
          busy={mutation.isPending}
          onCancel={() => setDeleting(undefined)}
          onConfirm={() =>
            mutation.mutate(
              {
                mark: deleting,
                body: deleting,
                key: keyFor(`delete:${deleting.id}:${deleting.revision}`),
                remove: true,
              },
              { onSuccess: () => setDeleting(undefined) },
            )
          }
        >
          <p>
            {duration(deleting.positionMs)} · {deleting.label}
          </p>
          <ErrorNotice error={mutation.error} />
        </ConfirmDialog>
      )}
      {discard && (
        <ConfirmDialog
          title="放弃标记修改？"
          onCancel={() => setDiscard(false)}
          onConfirm={() => {
            setEditing(undefined);
            setDiscard(false);
          }}
        >
          <p>未保存的标记草稿将被放弃。</p>
        </ConfirmDialog>
      )}
      <UnsavedGuard dirty={dirty} />
    </section>
  );
}
