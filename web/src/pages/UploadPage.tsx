import { useRef, useState } from "react";
import { Link } from "react-router-dom";
import { SegmentedControl } from "@ui/GlassNavigation";
import {
  useAuthors,
  useCollections,
  useLibraries,
  useSettings,
} from "../data/manage";
import { useSession } from "../data/session";
import { useUploadQueue } from "../data/upload";
import { isQueueTerminal, QueuedPreparationError } from "../data/upload-queue";
import type { QueueEntry, UploadPhase } from "../data/upload-queue";
import {
  EmptyState,
  ErrorNotice,
  Loading,
  PageHeading,
  StatusBadge,
} from "../components/Feedback";
import { Icon } from "../components/Icon";
import { bytes } from "../lib/format";
import { droppedFiles } from "../lib/drop-files";
import { DirectoryPicker } from "./DirectoryPicker";
import { NamingControls } from "./NamingControls";
import { previewNamingFiles } from "../data/naming";
import type { NamingFilePreview } from "../data/naming";

const phaseNames: Record<UploadPhase, string> = {
  waiting_file: "等待重选原文件",
  queued: "等待上传",
  hashing: "核验文件内容",
  uploading: "正在上传",
  completing: "确认入库结果",
  ingested: "已入库",
  staged: "已接收，等待整理",
  duplicate: "重复内容，未新增",
  failed: "未完成",
  paused: "已暂停",
  cancelled: "已取消",
};
export function UploadPage() {
  const libraries = useLibraries();
  const settings = useSettings();
  const { instance } = useSession();
  const upload = useUploadQueue();
  const [libraryId, setLibraryId] = useState("");
  const [directory, setDirectory] = useState("");
  const [mode, setMode] = useState<"ordinary" | "intelligent">("ordinary");
  const [authorId, setAuthorId] = useState("");
  const [authorName, setAuthorName] = useState("");
  const [acquisition, setAcquisition] = useState("");
  const [authorQ, setAuthorQ] = useState("");
  const [collectionId, setCollectionId] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [reading, setReading] = useState(false);
  const [automaticName, setAutomaticName] = useState(false);
  const [family, setFamily] = useState("");
  const [previewing, setPreviewing] = useState(false);
  const [namePreview, setNamePreview] = useState<NamingFilePreview[]>([]);
  const [retryFiles, setRetryFiles] = useState<readonly File[]>();
  const authors = useAuthors(authorQ);
  const collections = useCollections(libraryId || undefined);
  const selected = libraries.data?.items.find((item) => item.id === libraryId);
  const input = useRef<HTMLInputElement>(null);
  const directoryInput = useRef<HTMLInputElement>(null);
  const ready =
    !!selected?.enabled &&
    !!settings.data &&
    !libraries.isError &&
    !settings.isError &&
    !!upload.queue &&
    upload.initialized &&
    !upload.preparing &&
    !reading &&
    !previewing &&
    (!automaticName || !!family.trim() || !!collectionId);
  const add = async (files: readonly File[]) => {
    if (!ready || !selected || !upload.queue || !settings.data) return;
    setError(null);
    setRetryFiles(undefined);
    try {
      if (files.length > settings.data.maxBatchItems)
        throw new Error(
          `本批最多 ${settings.data.maxBatchItems} 个文件，请分批选取。`,
        );
      const naming = {
        libraryId: selected.id,
        relativeDirectory: directory || undefined,
        family:
          automaticName || collectionId
            ? family.trim() || undefined
            : undefined,
        collectionId: collectionId || undefined,
        note,
      };
      if (mode === "ordinary" && (automaticName || collectionId)) {
        setPreviewing(true);
        const kind = collections.data?.pages
          .flatMap((page) => page.items)
          .find((item) => item.id === collectionId)?.kind;
        setNamePreview(await previewNamingFiles(files, naming, kind));
      } else setNamePreview([]);
      await upload.queue.add(
        files,
        {
          libraryId: selected.id,
          relativeDirectory: directory || undefined,
          mode,
          authorId: authorId || undefined,
          collectionId: collectionId || undefined,
          acquisitionSources: acquisition
            .split("\n")
            .map((value) => value.trim())
            .filter(Boolean),
          note,
          family:
            automaticName || collectionId
              ? family.trim() || undefined
              : undefined,
        },
        selected.name,
      );
    } catch (failure) {
      setError(failure);
      setRetryFiles(failure instanceof QueuedPreparationError ? undefined : files);
    } finally {
      setPreviewing(false);
    }
  };
  return (
    <>
      <Link to="/manage" className="back-link">
        <Icon name="back" size={18} />
        数据管理
      </Link>
      <PageHeading
        title="上传文件"
        subtitle="选入即上传，原件留在你手中。"
        action={
          <Link className="button" to="/manage/tasks">
            任务与报告
          </Link>
        }
      />
      <ErrorNotice
        error={libraries.error ?? settings.error}
        retry={() => {
          void libraries.refetch();
          void settings.refetch();
        }}
      />
      {libraries.isPending || settings.isPending ? (
        <Loading text="正在确认上传目标…" />
      ) : libraries.data?.items.length === 0 ? (
        <EmptyState
          title="先创建一个媒体库"
          action={
            <Link className="button button--primary" to="/manage/libraries">
              管理媒体库
            </Link>
          }
        >
          普通上传不依赖 AI，可以直接入库。
        </EmptyState>
      ) : (
        <div className="upload-workbench">
          <div className="form-stack">
            <section className="panel form-stack">
              <h2>上传位置</h2>
              <label>
                目标媒体库
                <select
                  value={libraryId}
                  onChange={(event) => {
                    setLibraryId(event.target.value);
                    setCollectionId("");
                    setDirectory("");
                    setAuthorId("");
                    setAuthorName("");
                    setAcquisition("");
                    setNamePreview([]);
                    setRetryFiles(undefined);
                    setFamily("");
                  }}
                >
                  <option value="">选择媒体库</option>
                  {libraries.data?.items.map((library) => (
                    <option key={library.id} value={library.id}>
                      {library.name}
                      {library.enabled ? "" : "（已停用）"} ·{" "}
                      {library.kind === "cos" ? "COS" : "常规"}
                    </option>
                  ))}
                </select>
              </label>
              <DirectoryPicker
                libraryId={libraryId}
                value={directory}
                onChange={setDirectory}
              />
            </section>
            <NamingControls
              libraryId={libraryId}
              directory={directory}
              collectionId={collectionId}
              enabled={automaticName}
              family={family}
              setEnabled={setAutomaticName}
              setFamily={setFamily}
              busy={previewing || upload.preparing}
            />
            <details className="panel">
              <summary>作者、集合与备注（可选）</summary>
              <div className="form-stack section-space">
                <label>
                  查找作者
                  <input
                    value={authorQ}
                    onChange={(event) => setAuthorQ(event.target.value)}
                    placeholder="名称或别名"
                  />
                </label>
                <label>
                  指定作者
                  <select
                    value={authorId}
                    onChange={(event) => {
                      setAuthorId(event.target.value);
                      setAuthorName(
                        authors.data?.pages
                          .flatMap((page) => page.items)
                          .find((author) => author.id === event.target.value)
                          ?.name ?? "",
                      );
                      setAcquisition("");
                    }}
                  >
                    <option value="">未指定，保留为其他搁置</option>
                    {authorId &&
                      !authors.data?.pages
                        .flatMap((page) => page.items)
                        .some((author) => author.id === authorId) && (
                        <option value={authorId}>{authorName}</option>
                      )}
                    {authors.data?.pages
                      .flatMap((page) => page.items)
                      .map((author) => (
                        <option value={author.id} key={author.id}>
                          {author.name}
                        </option>
                      ))}
                  </select>
                </label>
                <ErrorNotice
                  error={authors.error}
                  retry={() => void authors.refetch()}
                />
                <label>
                  组图或 AI 短剧
                  <select
                    value={collectionId}
                    onChange={(event) => setCollectionId(event.target.value)}
                  >
                    <option value="">不加入集合</option>
                    {collections.data?.pages
                      .flatMap((page) => page.items)
                      .map((collection) => (
                        <option value={collection.id} key={collection.id}>
                          {collection.name} ·{" "}
                          {collection.kind === "short_drama" ? "短剧" : "组图"}
                        </option>
                      ))}
                  </select>
                </label>
                <ErrorNotice
                  error={collections.error}
                  retry={() => void collections.refetch()}
                />
                <Link to="/manage/collections">管理作品集合</Link>
                <label>
                  本批获取渠道，每行一个
                  <textarea
                    value={acquisition}
                    onChange={(event) => setAcquisition(event.target.value)}
                    maxLength={200000}
                    rows={3}
                  />
                </label>
                <label>
                  本批个人备注
                  <textarea
                    value={note}
                    onChange={(event) => setNote(event.target.value)}
                    maxLength={10000}
                    rows={3}
                  />
                </label>
              </div>
            </details>
          </div>
          <section className="panel upload-add">
            <SegmentedControl
              label="上传方式"
              value={mode}
              options={[
                { value: "ordinary", label: "普通上传" },
                { value: "intelligent", label: "智能整理" },
              ]}
              onChange={setMode}
            />
            <p className="muted">
              {mode === "ordinary"
                ? "按已选目标直接入库，无需 AI 在线。"
                : instance.capabilities.aiConfigured
                  ? "接收后进入本批智能任务，逐项处理并保留报告。"
                  : "当前未配置 AI 执行器；文件可以接收，批次会明确等待执行器，不会伪称已整理。"}
            </p>
            <div
              className="drop-zone"
              onDragOver={(event) => event.preventDefault()}
              onDrop={(event) => {
                event.preventDefault();
                if (!ready || !settings.data) return;
                setReading(true);
                setError(null);
                const data = event.dataTransfer;
                void droppedFiles(data, settings.data.maxBatchItems)
                  .then((files) => {
                    setReading(false);
                    return add(files);
                  })
                  .catch((failure) => setError(failure))
                  .finally(() => setReading(false));
              }}
            >
              <Icon name="upload" size={36} />
              <h2>把喜欢的内容放进来</h2>
              <p>选择文件，或拖入图片、视频与目录</p>
              <div className="actions">
                <button
                  className="button button--primary"
                  disabled={!ready}
                  onClick={() => input.current?.click()}
                >
                  选择文件
                </button>
                <button
                  className="button"
                  disabled={!ready}
                  onClick={() => directoryInput.current?.click()}
                >
                  选择目录
                </button>
              </div>
              <input
                ref={input}
                hidden
                type="file"
                multiple
                accept="image/*,video/*"
                onChange={(event) => {
                  const files = Array.from(event.target.files ?? []);
                  event.target.value = "";
                  void add(files);
                }}
              />
              <input
                ref={directoryInput}
                hidden
                type="file"
                multiple
                {...{ webkitdirectory: "" }}
                onChange={(event) => {
                  const files = Array.from(event.target.files ?? []);
                  event.target.value = "";
                  void add(files);
                }}
              />
            </div>
            <p className="destination-summary">
              {selected
                ? `${selected.name} / ${directory || "库根目录"}`
                : "请先选择目标媒体库"}
              {authorId && " · 已选定作者"}
              {collectionId && " · 已选定作品集合"}
            </p>
            {upload.preparing && <Loading text="正在创建持久批次…" />}
            {previewing && <Loading text="正在预览选定文件的名称…" />}
            {reading && <Loading text="正在读取目录…" />}
            <ErrorNotice error={error} />
            {retryFiles && (
              <button
                className="button"
                disabled={!ready}
                onClick={() => void add(retryFiles)}
              >
                按当前目标重试选定文件
              </button>
            )}
          </section>
        </div>
      )}
      <div className="section-heading">
        <h2>上传队列</h2>
        <button
          className="button button--small"
          onClick={() => void upload.queue?.clearFinished()}
        >
          清理已结束记录
        </button>
      </div>
      {namePreview.length > 0 && (
        <details className="panel section-space">
          <summary>本次选取的命名预览（{namePreview.length} 项）</summary>
          <p className="muted">
            这是入队时的服务端预览，不占用编号。其他设备同时入库时，实际名称以队列的入库结果与任务回读为准。
          </p>
          <div className="profile-raw">
            {namePreview.map((item, index) => (
              <p key={index}>
                {item.sourceName} → {item.relativePath}
              </p>
            ))}
          </div>
        </details>
      )}
      <p className="muted">
        目标与关联以入队时的选择为准。清理记录不会删除媒体。
      </p>
      {upload.persistenceError && (
        <div className="notice notice--warning" role="alert">
          <p>{upload.persistenceError}</p>
          <button
            className="button"
            onClick={() => void upload.queue?.retryStorage()}
          >
            重试本地存储
          </button>
        </div>
      )}
      {!upload.initialized ? (
        <Loading text="正在恢复队列…" />
      ) : !upload.entries.length ? (
        <EmptyState title="队列中还没有文件" />
      ) : (
        <div className="upload-queue">
          {upload.entries.map((entry) => (
            <UploadRow key={entry.id} entry={entry} />
          ))}
        </div>
      )}
    </>
  );
}
export function UploadRow({ entry }: { entry: QueueEntry }) {
  const { queue } = useUploadQueue();
  const reselect = useRef<HTMLInputElement>(null);
  const active = ["hashing", "uploading", "completing", "queued"].includes(
    entry.phase,
  );
  const progress =
    entry.phase === "hashing" ? entry.hashProgress : entry.offset;
  return (
    <article className="queue-card panel">
      <div className="queue-title">
        <Icon name="image" />
        <div>
          <strong>{entry.name}</strong>
          <p>
            {entry.libraryName} /{" "}
            {entry.request.relativeDirectory || "库根目录"} ·{" "}
            {bytes(entry.size)} ·{" "}
            {entry.request.mode === "ordinary" ? "普通上传" : "智能整理"}
          </p>
        </div>
        <StatusBadge
          tone={
            entry.phase === "ingested"
              ? "success"
              : entry.phase === "failed" || entry.phase === "duplicate"
                ? "warning"
                : "neutral"
          }
        >
          {phaseNames[entry.phase]}
        </StatusBadge>
      </div>
      {active && (
        <progress
          max={entry.size}
          value={progress}
          aria-label={`${entry.name} ${phaseNames[entry.phase]}`}
        />
      )}
      {entry.error && (
        <p className="queue-error" role="alert">
          {entry.error}
        </p>
      )}
      {entry.waitingFor && (
        <p className="muted">
          保持命名顺序，等待前项“{entry.waitingFor}
          ”完成。前项失败或暂停时，请先重试或明确取消前项。
        </p>
      )}
      {entry.actualFileName && entry.actualFileName !== entry.name && (
        <p>已保存为：{entry.actualFileName}</p>
      )}
      <div className="actions">
        {entry.assetId && (
          <Link
            className="button button--small"
            to={`/asset/${encodeURIComponent(entry.assetId)}`}
          >
            查看入库内容
          </Link>
        )}
        {entry.request.batchId && (
          <Link
            className="button button--small"
            to={`/manage/tasks/${encodeURIComponent(entry.request.batchId)}`}
          >
            本批报告
          </Link>
        )}
        {active && (
          <button
            className="button button--small"
            onClick={() => queue?.pause(entry.id)}
          >
            暂停
          </button>
        )}
        {!isQueueTerminal(entry.phase) && !active && (
          <>
            <button
              className="button button--small"
              onClick={() =>
                entry.phase === "waiting_file"
                  ? reselect.current?.click()
                  : queue?.retry(entry.id)
              }
            >
              {entry.phase === "waiting_file" ? "重选原文件" : "继续 / 重试"}
            </button>
            <button
              className="button button--small"
              onClick={() => void queue?.cancel(entry.id)}
            >
              取消此项
            </button>
          </>
        )}
        <input
          type="file"
          hidden
          ref={reselect}
          onChange={(event) => {
            const file = event.target.files?.[0];
            event.target.value = "";
            if (file) queue?.retry(entry.id, file);
          }}
        />
      </div>
    </article>
  );
}
