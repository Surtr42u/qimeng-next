import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useAsset } from "../data/media";
import type { Asset } from "../data/media";
import { useLibraries, useSettings } from "../data/manage";
import { useUploadQueue } from "../data/upload";
import { QueuedPreparationError } from '../data/upload-queue';
import { PageHeading, Loading, ErrorNotice } from "../components/Feedback";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { UploadRow } from "./UploadQueueList";
import { bytes } from "../lib/format";
import { UnsavedGuard } from "../lib/navigation";

export function ReplacePage() {
  const { id = "" } = useParams();
  const asset = useAsset(id);
  const libraries = useLibraries();
  const upload = useUploadQueue();
  const settings = useSettings();
  const [file, setFile] = useState<File>();
  const [confirmation, setConfirmation] = useState<{
    file: File;
    asset: Asset;
  }>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const tasks = upload.entries.filter(
    (entry) => entry.request.replaceAssetId === id,
  );
  const submit = async () => {
    if (!confirmation || !upload.queue || busy) return;
    setBusy(true);
    setError(null);
    try {
      const target = confirmation.asset;
      await upload.queue.add(
        [confirmation.file],
        {
          libraryId: target.libraryId,
          mode: "ordinary",
          replaceAssetId: target.id,
          expectedContentVersion: target.contentVersion,
        },
        libraries.data?.items.find((library) => library.id === target.libraryId)
          ?.name ?? "原媒体库",
      );
      setFile(undefined);
      setConfirmation(undefined);
    } catch (failure) {
      setError(failure);
      if (failure instanceof QueuedPreparationError) { setFile(undefined); setConfirmation(undefined); }
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <Link className="back-link" to={`/asset/${encodeURIComponent(id)}`}>
        返回媒体详情
      </Link>
      <PageHeading
        title="替换原件"
        subtitle="只替换明确选择的目标，保留资产身份、作者、标签、收藏与历史。"
      />
      <ErrorNotice
        error={asset.error ?? libraries.error ?? settings.error}
        retry={() => {
          void asset.refetch();
          void libraries.refetch();
          void settings.refetch();
        }}
      />
      {asset.isPending ? (
        <Loading />
      ) : (
        asset.data && (
          <section className="panel form-stack">
            <h2>{asset.data.fileName}</h2>
            <p className="muted">
              当前内容版本 {asset.data.contentVersion} ·{" "}
              {bytes(asset.data.size)}
            </p>
            <p>
              替换在服务器核验新内容后提交。旧版本时间轴保留来源，不自动套用到新视频。替换失败不等于旧媒体已丢失。
            </p>
            <label>
              选择用于替换的图片或视频
              <input
                type="file"
                accept="image/*,video/*"
                disabled={busy}
                onChange={(event) => setFile(event.target.files?.[0])}
              />
            </label>
            {file && (
              <p>
                {file.name} · {bytes(file.size)}
              </p>
            )}
            <button
              className="button button--primary"
              disabled={!file || !upload.initialized || !upload.queue || busy}
              onClick={() => {
                if (file && asset.data)
                  setConfirmation({ file, asset: asset.data });
              }}
            >
              核对并替换
            </button>
          </section>
        )
      )}
      <ErrorNotice error={error} />
      {upload.persistenceError && (
        <p role="alert" className="notice notice--warning">
          {upload.persistenceError}
        </p>
      )}
      {tasks.length > 0 && (
        <ul className="section-space upload-task-list">
          {tasks.map((entry) => (
            <UploadRow key={entry.id} entry={entry} />
          ))}
        </ul>
      )}
      <UnsavedGuard dirty={!!file && !busy} />
      {confirmation && (
        <ConfirmDialog
          title="替换这份原件？"
          confirmLabel="上传并替换"
          busy={busy}
          onCancel={() => setConfirmation(undefined)}
          onConfirm={() => void submit()}
        >
          <p>
            目标：{confirmation.asset.fileName}，内容版本{" "}
            {confirmation.asset.contentVersion}
          </p>
          <p>
            新文件：{confirmation.file.name}，{bytes(confirmation.file.size)}
          </p>
          <p>
            目标在此后变化会被服务器拒绝并提示冲突。本次授权不删除上传来源文件。
          </p>
          <ErrorNotice error={error} />
        </ConfirmDialog>
      )}
    </>
  );
}
