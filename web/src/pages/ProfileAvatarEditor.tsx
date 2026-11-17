import { useState } from "react";
import type { Schema } from "../data/client";
import { useProfileAvatarMutation } from "../data/author-profiles";
import { newId } from "../lib/id";
import { ErrorNotice } from "../components/Feedback";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { UnsavedGuard } from "../lib/navigation";
import { AuthorAvatar, ProfileLink } from "./ProfilePresentation";

export function ProfileAvatarEditor({
  block,
  name,
  reload,
  close,
}: {
  block: Schema<"AuthorProfileBlock">;
  name: string;
  reload: () => Promise<{
    data?: Schema<"AuthorProfile">;
    isSuccess: boolean;
    error?: unknown;
  }>;
  close: () => void;
}) {
  const [base, setBase] = useState(block);
  const [source, setSource] = useState(block.values.avatarSourceUrl ?? "");
  const [selected, setSelected] = useState<{ file: File; key: string }>();
  const [confirmation, setConfirmation] = useState<"clear" | "discard">();
  const [comparison, setComparison] = useState<Schema<"AuthorProfileBlock">>();
  const [readError, setReadError] = useState<unknown>();
  const mutation = useProfileAvatarMutation(block.authorId);
  const dirty = !!selected || source !== (base.values.avatarSourceUrl ?? "");
  const loadCurrent = async () => {
    const result = await reload();
    const current = result.data?.blocks.find((item) => item.block === "x");
    if (result.isSuccess && current) {
      setComparison(current);
      setReadError(undefined);
    } else setReadError(result.error ?? new Error("未能读取当前头像信息。"));
  };
  return (
    <section className="panel form-stack">
      <div className="section-heading">
        <h3>离线头像</h3>
        <span className="muted">X 资料修订 {base.revision}</span>
      </div>
      <div className="actions">
        <AuthorAvatar name={name} avatar={base.avatar} large />
        <div>
          <p>
            {base.avatar
              ? `本地 PNG · ${base.avatar.width} × ${base.avatar.height}`
              : "尚无离线头像"}
          </p>
          {base.avatar && (
            <p className="muted">
              保存于 {new Date(base.avatar.fetchedAt).toLocaleString()}
            </p>
          )}
        </div>
      </div>
      {base.avatar?.sourceUrl && (
        <p>
          已保存来源：
          <ProfileLink value={base.avatar.sourceUrl} />
        </p>
      )}
      <p className="muted">
        使用同源认证的本地图片。图片上限 4 MiB / 400 万像素，服务端规范化至 512
        像素以内；网络失败保留当前头像。
      </p>
      <fieldset
        className="form-stack profile-editor-fields"
        disabled={mutation.isPending}
      >
        <label>
          HTTPS 图片来源
          <input
            type="url"
            maxLength={4096}
            value={source}
            onChange={(event) => setSource(event.target.value)}
            placeholder="https://…"
          />
        </label>
        <button
          className="button"
          disabled={!source.trim()}
          onClick={() =>
            mutation.mutate(
              {
                kind: "https",
                body: { expectedRevision: base.revision, sourceUrl: source },
              },
              { onSuccess: close },
            )
          }
        >
          获取并保存离线头像
        </button>
        <label>
          或选择本地头像
          <input
            type="file"
            accept="image/png,image/jpeg,image/gif,image/webp"
            onChange={(event) => {
              const file = event.target.files?.[0];
              setSelected(file ? { file, key: newId() } : undefined);
              mutation.reset();
            }}
          />
        </label>
        {selected && (
          <p className="muted">
            {selected.file.name} · {(selected.file.size / 1024).toFixed(0)} KiB
          </p>
        )}
        {selected && selected.file.size > 4 * 1024 * 1024 && (
          <p role="alert">此文件超过 4 MiB，请选择较小头像。</p>
        )}
        <div className="actions">
          <button
            className="button button--primary"
            disabled={!selected || selected.file.size > 4 * 1024 * 1024}
            onClick={() =>
              selected &&
              mutation.mutate(
                { kind: "upload", ...selected, revision: base.revision },
                { onSuccess: close },
              )
            }
          >
            保存本地头像
          </button>
          <button
            className="button button--danger"
            disabled={!base.avatar && !base.values.avatarSourceUrl}
            onClick={() => setConfirmation("clear")}
          >
            清除头像与来源
          </button>
          <button
            className="button"
            onClick={() => (dirty ? setConfirmation("discard") : close())}
          >
            关闭
          </button>
        </div>
      </fieldset>
      {mutation.isPending && <p role="status">正在处理头像…</p>}
      <ErrorNotice error={mutation.error} />
      {mutation.isError && (
        <button className="button" onClick={() => void loadCurrent()}>
          读取当前头像并核对
        </button>
      )}
      <ErrorNotice error={readError} />
      {comparison && (
        <div className="notice">
          <h4>当前资料修订 {comparison.revision}</h4>
          <p>来源：{comparison.values.avatarSourceUrl || "未设置"}</p>
          <AuthorAvatar name={name} avatar={comparison.avatar} />
          <button
            className="button"
            disabled={mutation.isPending}
            onClick={() => {
              setBase(comparison);
              setComparison(undefined);
              mutation.reset();
            }}
          >
            已核对，以当前版本重试所选操作
          </button>
        </div>
      )}
      <UnsavedGuard dirty={dirty} />
      {confirmation && (
        <ConfirmDialog
          title={
            confirmation === "clear"
              ? "清除当前头像？"
              : "放弃未保存的头像选择？"
          }
          danger={confirmation === "clear"}
          busy={mutation.isPending}
          onCancel={() => setConfirmation(undefined)}
          onConfirm={() =>
            confirmation === "clear"
              ? mutation.mutate(
                  { kind: "clear", revision: base.revision },
                  { onSuccess: close },
                )
              : close()
          }
        >
          <p>
            {confirmation === "clear"
              ? "同时清除来源绑定，其他 X 资料保持不变。归档中保留的历史头像不受影响。"
              : "当前选取尚未保存，服务器头像保持原状。"}
          </p>
          <ErrorNotice error={mutation.error} />
        </ConfirmDialog>
      )}
    </section>
  );
}
