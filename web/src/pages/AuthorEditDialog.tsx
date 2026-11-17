import { useState } from "react";
import { Dialog } from "radix-ui";
import { useAuthor, useCreateAuthor, useUpdateAuthor } from "../data/manage";
import { ApiError } from "../lib/errors";
import type { Schema } from "../data/client";
import { ErrorNotice, Loading } from "../components/Feedback";
import { AcquisitionSourceField } from "../components/AcquisitionSourceField";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { UnsavedGuard } from "../lib/navigation";
import { newId } from "../lib/id";

function displayName(author?: Schema<"Author">) {
  return author ? [author.name, ...(author.aliases ?? [])].join(" / ") : "";
}
export function AuthorEditDialog({
  id,
  onDismiss,
  onSaved,
}: {
  id?: string;
  onDismiss: () => void;
  onSaved: (message: string) => void;
}) {
  const query = useAuthor(id ?? "");
  if (id && !query.data)
    return (
      <Dialog.Root
        open
        onOpenChange={(open) => {
          if (!open) onDismiss();
        }}
      >
        <Dialog.Portal>
          <Dialog.Overlay className="detail-dialog-overlay" />
          <Dialog.Content className="detail-dialog author-editor">
            <Dialog.Title className="detail-dialog-title">
              编辑作者资料
            </Dialog.Title>
            <Dialog.Description className="detail-dialog-desc">
              名称、别名与来源记录
            </Dialog.Description>
            {query.isPending ? (
              <Loading text="正在读取作者资料…" />
            ) : (
              <ErrorNotice
                error={query.error}
                retry={() => void query.refetch()}
              />
            )}
            <div className="detail-dialog-foot">
              <button
                className="confirm-btn confirm-btn--cancel"
                onClick={onDismiss}
              >
                关闭
              </button>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    );
  return (
    <AuthorDraft
      author={query.data}
      onDismiss={onDismiss}
      onSaved={onSaved}
      reload={async () => (await query.refetch()).data}
    />
  );
}
function AuthorDraft({
  author,
  onDismiss,
  onSaved,
  reload,
}: {
  author?: Schema<"Author">;
  onDismiss: () => void;
  onSaved: (message: string) => void;
  reload: () => Promise<Schema<"Author"> | undefined>;
}) {
  const [base, setBase] = useState(author);
  const [name, setName] = useState(displayName(author));
  const [sources, setSources] = useState<string[] | null>(null);
  const [tab, setTab] = useState<"name" | "sources">("name");
  const [discard, setDiscard] = useState(false);
  const [reloading, setReloading] = useState(false);
  const [attempt, setAttempt] = useState({ name: "", key: newId() });
  const create = useCreateAuthor();
  const update = useUpdateAuthor(base?.id ?? "");
  const busy = create.isPending || update.isPending || reloading;
  const dirty =
    name !== displayName(base) ||
    (sources !== null &&
      JSON.stringify(sources) !==
        JSON.stringify(base?.acquisitionSources ?? []));
  const error = create.error ?? update.error;
  const conflict = error instanceof ApiError && error.status === 409;
  const dismiss = () => {
    if (!busy) {
      if (dirty) setDiscard(true);
      else onDismiss();
    }
  };
  const saveName = () => {
    const [first = "", ...aliases] = name
      .split(" / ")
      .map((value) => value.trim())
      .filter(Boolean);
    if (!first) return;
    if (base)
      update.mutate(
        { expectedRevision: base.revision, name: first, aliases },
        {
          onSuccess: () => {
            onSaved("作者名称已保存");
            onDismiss();
          },
        },
      );
    else {
      const key = attempt.name === name ? attempt.key : newId();
      setAttempt({ name, key });
      create.mutate(
        { key, body: { name: first, aliases, kind: "regular" } },
        {
          onSuccess: () => {
            onSaved("作者已添加");
            onDismiss();
          },
        },
      );
    }
  };
  const saveSources = () => {
    if (!base || sources === null) return;
    update.mutate(
      { expectedRevision: base.revision, acquisitionSources: sources },
      {
        onSuccess: (saved) => {
          setBase(saved);
          setSources(null);
          onSaved("来源已保存");
        },
      },
    );
  };
  return (
    <>
      <UnsavedGuard dirty={dirty || busy} />
      <Dialog.Root
        open
        onOpenChange={(open) => {
          if (!open) dismiss();
        }}
      >
        <Dialog.Portal>
          <Dialog.Overlay className="detail-dialog-overlay" />
          <Dialog.Content
            className="detail-dialog author-editor"
            onEscapeKeyDown={(event) => {
              event.preventDefault();
              dismiss();
            }}
            onPointerDownOutside={(event) => {
              event.preventDefault();
              dismiss();
            }}
          >
            <Dialog.Title className="detail-dialog-title">
              {base ? "编辑作者资料" : "新建作者"}
            </Dialog.Title>
            <Dialog.Description className="detail-dialog-desc">
              {base
                ? displayName(base)
                : "添加作者后，可继续维护来源和关联文件。"}
            </Dialog.Description>
            {base && (
              <div className="settings-options" aria-label="资料分类">
                <button
                  type="button"
                  className={`pill${tab === "name" ? " active" : ""}`}
                  disabled={dirty || busy}
                  onClick={() => setTab("name")}
                  aria-pressed={tab === "name"}
                >
                  名称与别名
                </button>
                <button
                  type="button"
                  className={`pill${tab === "sources" ? " active" : ""}`}
                  disabled={dirty || busy}
                  onClick={() => setTab("sources")}
                  aria-pressed={tab === "sources"}
                >
                  来源 / 出处
                </button>
              </div>
            )}
            {dirty && (
              <p className="detail-dialog-desc">
                请保存当前修改后切换分类，或关闭并放弃修改。
              </p>
            )}
            {tab === "name" ? (
              <div className="settings-field">
                <label htmlFor="author-name">作者名称</label>
                <input
                  id="author-name"
                  className="tag-input"
                  value={name}
                  disabled={busy}
                  onChange={(event) =>
                    setName(
                      Array.from(event.target.value).slice(0, 200).join(""),
                    )
                  }
                />
                <small>多个别名用「 / 」分隔；改名会保留关注与文件关联。</small>
                <small>{Array.from(name).length} / 200</small>
                <button
                  type="button"
                  className="confirm-btn confirm-btn--primary"
                  disabled={
                    busy ||
                    !name.trim() ||
                    (!!base && name === displayName(base))
                  }
                  onClick={saveName}
                >
                  {busy ? "保存中…" : "保存名称"}
                </button>
              </div>
            ) : (
              <div>
                <AcquisitionSourceField
                  selected={sources ?? base?.acquisitionSources ?? []}
                  onChange={setSources}
                  disabled={busy}
                />
                <button
                  type="button"
                  className="confirm-btn confirm-btn--primary"
                  disabled={busy || sources === null}
                  onClick={saveSources}
                >
                  {busy ? "保存中…" : "保存来源"}
                </button>
              </div>
            )}
            <ErrorNotice error={error} />
            {conflict && (
              <div className="manage-notice manage-notice--warning">
                <span>
                  作者已被其他操作修改。草稿仍保留；读取最新版本后请核对，再保存完整的当前分类。
                </span>
                <button
                  type="button"
                  className="pill"
                  disabled={busy}
                  onClick={async () => {
                    setReloading(true);
                    try {
                      const latest = await reload();
                      if (latest) {
                        setBase(latest);
                        update.reset();
                      }
                    } finally {
                      setReloading(false);
                    }
                  }}
                >
                  读取最新版本，保留草稿
                </button>
              </div>
            )}
            <div className="detail-dialog-foot">
              <button
                type="button"
                className="confirm-btn confirm-btn--cancel"
                disabled={busy}
                onClick={dismiss}
              >
                关闭
              </button>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
      {discard && (
        <ConfirmDialog
          title="放弃未保存的修改？"
          confirmLabel="放弃修改"
          onCancel={() => setDiscard(false)}
          onConfirm={onDismiss}
        >
          <p>本次尚未保存的名称或来源将丢弃。</p>
        </ConfirmDialog>
      )}
    </>
  );
}
