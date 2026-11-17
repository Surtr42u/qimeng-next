import { useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useStartScan } from '../data/maintenance';
import {
  useLibraries,
  useCreateLibrary,
  useCollections,
  useCreateCollection,
  useTrash,
  useTrashAction,
  useCreateBackup,
  useStorages,
} from "../data/manage";
import type { Schema } from "../data/client";
import {
  EmptyState,
  ErrorNotice,
  Loading,
  PageHeading,
  StatusBadge,
} from "../components/Feedback";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { bytes, dateTime } from "../lib/format";
import { newId } from "../lib/id";
import { UnsavedGuard } from "../lib/navigation";
import { useUpdateLibrary } from "../data/catalog";

export function LibrariesPage() {
  const libraries = useLibraries();
  const storages = useStorages();
  const create = useCreateLibrary();
  const [name, setName] = useState("");
  const [kind, setKind] = useState<"regular" | "cos">("regular");
  const [storageId, setStorageId] = useState('data');
  const [rootRelativePath, setRootRelativePath] = useState('');
  const [open, setOpen] = useState(false);
  const attempt = useRef({ body: "", key: newId() });
  return (
    <>
      <PageHeading
        title="媒体库"
        subtitle="常规与 COS 保持独立分区，媒体统一由服务器保存。"
        action={
          <button className="button" onClick={() => setOpen(!open)}>
            新建媒体库
          </button>
        }
      />
      <ErrorNotice
        error={libraries.error}
        stale={!!libraries.data}
        retry={() => void libraries.refetch()}
      />
      {open && (
        <form
          className="panel form-stack"
          onSubmit={(event) => {
            event.preventDefault();
            const body = { name, kind, storageId, ...(storageId !== 'data' ? { rootRelativePath } : {}) };
            const signature = JSON.stringify(body);
            if (signature !== attempt.current.body)
              attempt.current = { body: signature, key: newId() };
            create.mutate(
              { body, key: attempt.current.key },
              {
                onSuccess: () => {
                  setOpen(false);
                  setName("");
                },
              },
            );
          }}
        >
          <h2>新建媒体库</h2>
          <label>
            名称
            <input
              value={name}
              disabled={create.isPending}
              onChange={(event) => setName(event.target.value)}
              maxLength={120}
              required
            />
          </label>
          <label>
            分区
            <select
              value={kind}
              disabled={create.isPending}
              onChange={(event) =>
                setKind(event.target.value as "regular" | "cos")
              }
            >
              <option value="regular">常规</option>
              <option value="cos">COS</option>
            </select>
          </label>
          <label>存储位置
            <select value={storageId} disabled={create.isPending || !storages.data} onChange={event => { setStorageId(event.target.value); setRootRelativePath(''); }}>
              {storages.data?.items.map(storage => <option key={storage.id} value={storage.id} disabled={!storage.available}>{storage.id === 'data' ? '内置受管存储' : storage.id}{storage.available ? '' : '（不可用）'}</option>)}
            </select>
          </label>
          {storageId !== 'data' && <label>此存储位置中的相对目录<input value={rootRelativePath} onChange={event => setRootRelativePath(event.target.value)} disabled={create.isPending} placeholder="例如 作品/图集；留空使用已配置根目录" /></label>}
          <p className="muted">已有目录的登记与扫描入库分别记录。登记不会删除原件，也不代表其中的文件已经扫描完成。</p>
          <ErrorNotice error={storages.error} retry={() => void storages.refetch()} />
          <ErrorNotice error={create.error} />
          <button
            className="button button--primary"
            disabled={create.isPending || !name.trim() || !storages.data?.items.some(storage => storage.id === storageId && storage.available)}
          >
            创建
          </button>
          <UnsavedGuard dirty={!!name} />
        </form>
      )}
      {libraries.isPending ? (
        <Loading />
      ) : libraries.data?.items.length ? (
        <div className="manage-grid">
          {libraries.data.items.map((library) => (
            <LibraryCard key={library.id} library={library} />
          ))}
        </div>
      ) : (
        !libraries.isError && (
          <EmptyState title="还没有媒体库">创建后即可开始普通上传。</EmptyState>
        )
      )}
    </>
  );
}

function LibraryCard({ library }: { library: Schema<"Library"> }) {
  const navigate = useNavigate();
  const scan = useStartScan();
  const scanAttempt = useRef({ revision: library.revision, key: newId() });
  const update = useUpdateLibrary();
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(library.name);
  const [baseName, setBaseName] = useState(library.name);
  const [revision, setRevision] = useState(library.revision);
  const [confirmation, setConfirmation] = useState<{
    revision: number;
    enabled: boolean;
    key: string;
  }>();
  const attempt = useRef({ name: "", revision: 0, key: newId() });
  return (
    <section className="panel">
      <div className="section-heading">
        <h2>{library.name}</h2>
        <StatusBadge tone={library.enabled ? "neutral" : "warning"}>
          {library.enabled
            ? library.kind === "cos"
              ? "COS"
              : "常规"
            : "已停用"}
        </StatusBadge>
      </div>
      <p className="muted">创建于 {dateTime(library.createdAt)}</p>
      <div className="actions">
        <Link
          className="button button--small"
          to={`/album?libraryId=${encodeURIComponent(library.id)}`}
        >
          浏览内容
        </Link>
        <Link className="button button--small" to="/manage/upload">
          上传文件
        </Link>
        <button
          className="button button--small"
          onClick={() => {
            setName(library.name);
            setBaseName(library.name);
            setRevision(library.revision);
            setEditing(!editing);
          }}
        >
          改名
        </button>
        <button
          className="button button--small"
          onClick={() => {
            update.reset();
            setConfirmation({
              revision: library.revision,
              enabled: !library.enabled,
              key: newId(),
            });
          }}
        >
          {library.enabled ? "停用" : "启用"}
        </button>
      </div>
      {editing && (
        <form
          className="inline-form section-space"
          onSubmit={(event) => {
            event.preventDefault();
            if (
              attempt.current.name !== name ||
              attempt.current.revision !== revision
            )
              attempt.current = { name, revision, key: newId() };
            update.mutate(
              {
                id: library.id,
                body: { expectedRevision: revision, name },
                key: attempt.current.key,
              },
              { onSuccess: () => setEditing(false) },
            );
          }}
        >
          <input
            aria-label="媒体库名称"
            value={name}
            maxLength={120}
            required
            disabled={update.isPending}
            onChange={(event) => setName(event.target.value)}
          />
          <button
            className="button"
            disabled={update.isPending || name === baseName}
          >
            保存
          </button>
        </form>
      )}
      <div className="actions section-space"><button className="button button--small" disabled={!library.enabled || scan.isPending || editing} onClick={() => {
        if (scanAttempt.current.revision !== library.revision) scanAttempt.current = { revision: library.revision, key: newId() };
        scan.mutate({ id: library.id, revision: library.revision, key: scanAttempt.current.key }, { onSuccess: job => navigate(`/manage/scans/${encodeURIComponent(job.id)}`) });
      }}>{scan.isPending ? '正在提交扫描…' : scan.isError ? '重试提交扫描' : '扫描已有文件'}</button></div>
      <ErrorNotice error={scan.error} />
      <ErrorNotice error={update.error} />
      {confirmation && (
        <ConfirmDialog
          title={confirmation.enabled ? "启用此媒体库？" : "停用此媒体库？"}
          busy={update.isPending}
          onCancel={() => setConfirmation(undefined)}
          onConfirm={() =>
            update.mutate(
              {
                id: library.id,
                body: {
                  expectedRevision: confirmation.revision,
                  enabled: confirmation.enabled,
                },
                key: confirmation.key,
              },
              { onSuccess: () => setConfirmation(undefined) },
            )
          }
        >
          <p>{library.name}</p>
          <p>
            停用会阻止新的入库操作，媒体原件和记录保留。既有任务的实际结果可在报告中核对。
          </p>
          <ErrorNotice error={update.error} />
        </ConfirmDialog>
      )}
      <UnsavedGuard dirty={editing && name !== baseName} />
    </section>
  );
}

export function CollectionsPage() {
  const libraries = useLibraries();
  const collections = useCollections();
  const create = useCreateCollection();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [aliases, setAliases] = useState("");
  const [libraryId, setLibraryId] = useState("");
  const [kind, setKind] = useState<"image_set" | "short_drama">("image_set");
  const attempt = useRef({ body: "", key: newId() });
  const items = collections.data?.pages.flatMap((page) => page.items) ?? [];
  return (
    <>
      <PageHeading
        title="组图与 AI 短剧"
        subtitle="集合保留身份与顺序，浏览仍逐张展示。"
        action={
          <button className="button" onClick={() => setOpen(!open)}>
            新建作品集合
          </button>
        }
      />
      {open && (
        <form
          className="panel form-stack"
          onSubmit={(event) => {
            event.preventDefault();
            const body = {
              libraryId,
              name,
              kind,
              aliases: aliases
                .split("\n")
                .map((value) => value.trim())
                .filter(Boolean),
            };
            const signature = JSON.stringify(body);
            if (signature !== attempt.current.body)
              attempt.current = { body: signature, key: newId() };
            create.mutate(
              { body, key: attempt.current.key },
              {
                onSuccess: () => {
                  setOpen(false);
                  setName("");
                  setAliases("");
                },
              },
            );
          }}
        >
          <h2>新建集合</h2>
          <label>
            媒体库
            <select
              required
              value={libraryId}
              onChange={(event) => setLibraryId(event.target.value)}
            >
              <option value="">选择媒体库</option>
              {libraries.data?.items.map((library) => (
                <option key={library.id} value={library.id}>
                  {library.name}
                </option>
              ))}
            </select>
          </label>
          <ErrorNotice
            error={libraries.error}
            retry={() => void libraries.refetch()}
          />
          <label>
            作品名称
            <input
              required
              maxLength={200}
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </label>
          <label>
            类型
            <select
              value={kind}
              onChange={(event) => setKind(event.target.value as typeof kind)}
            >
              <option value="image_set">组图</option>
              <option value="short_drama">AI 短剧</option>
            </select>
          </label>
          <label>
            简称或别名，每行一个
            <textarea
              value={aliases}
              onChange={(event) => setAliases(event.target.value)}
            />
          </label>
          <p className="muted">
            短剧续号由服务器分配；个人备注在上传时填写，不自动编写。
          </p>
          <ErrorNotice error={create.error} />
          <button
            className="button button--primary"
            disabled={!name.trim() || !libraryId || create.isPending}
          >
            创建集合
          </button>
          <UnsavedGuard dirty={!!name || !!aliases} />
        </form>
      )}
      <ErrorNotice
        error={collections.error}
        stale={items.length > 0}
        retry={() => void collections.refetch()}
      />
      {collections.isPending ? (
        <Loading />
      ) : items.length ? (
        <div className="manage-grid">
          {items.map((collection) => (
            <Link
              className="panel collection-card"
              key={collection.id}
              to={`/album?collectionId=${encodeURIComponent(collection.id)}`}
            >
              <StatusBadge>
                {collection.kind === "short_drama" ? "AI 短剧" : "组图"}
              </StatusBadge>
              <h2>{collection.name}</h2>
              <p>{collection.aliases?.join(" · ") || "查看本地内容"}</p>
            </Link>
          ))}
        </div>
      ) : (
        !collections.isError && <EmptyState title="还没有作品集合" />
      )}
      {collections.hasNextPage && (
        <button
          className="button"
          disabled={collections.isFetchingNextPage}
          onClick={() => void collections.fetchNextPage()}
        >
          加载更多集合
        </button>
      )}
    </>
  );
}

export function TrashPage() {
  const query = useTrash();
  const mutation = useTrashAction();
  const [target, setTarget] = useState<{
    asset: Schema<"Asset">;
    permanent: boolean;
    key: string;
  } | null>(null);
  const items = query.data?.pages.flatMap((page) => page.items) ?? [];
  return (
    <>
      <PageHeading
        title="回收站"
        subtitle="恢复会保留原资产身份与关联。永久删除是独立操作。"
      />
      <ErrorNotice
        error={query.error}
        stale={items.length > 0}
        retry={() => void query.refetch()}
      />
      {query.isPending ? (
        <Loading />
      ) : items.length ? (
        <div className="list-panel">
          {items.map((asset) => (
            <div className="list-row" key={asset.id}>
              <div className="row-main">
                <strong>{asset.fileName}</strong>
                <span>
                  {bytes(asset.size)} · {dateTime(asset.updatedAt)}
                </span>
              </div>
              <div className="actions">
                <button
                  className="button button--small"
                  onClick={() => {
                    mutation.reset();
                    setTarget({ asset, permanent: false, key: newId() });
                  }}
                >
                  恢复
                </button>
                <button
                  className="button button--small button--danger"
                  onClick={() => {
                    mutation.reset();
                    setTarget({ asset, permanent: true, key: newId() });
                  }}
                >
                  永久删除
                </button>
              </div>
            </div>
          ))}
        </div>
      ) : (
        !query.isError && <EmptyState title="回收站是空的" />
      )}
      {query.hasNextPage && (
        <button
          className="button"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
        >
          加载更多
        </button>
      )}
      {target && (
        <ConfirmDialog
          title={target.permanent ? "永久删除这个文件？" : "恢复媒体？"}
          danger={target.permanent}
          confirmLabel={target.permanent ? "永久删除" : "恢复"}
          busy={mutation.isPending}
          onCancel={() => setTarget(null)}
          onConfirm={() =>
            mutation.mutate(
              {
                id: target.asset.id,
                revision: target.asset.revision,
                key: target.key,
                permanent: target.permanent,
              },
              { onSuccess: () => setTarget(null) },
            )
          }
        >
          <p>{target.asset.fileName}</p>
          <p>
            {target.permanent
              ? "此操作删除回收站中的原件，无法从本回收站恢复。历史事件仍保留。"
              : "恢复到库内原位置；如果位置已被占用，服务端会返回冲突并保留现场。"}
          </p>
          <ErrorNotice error={mutation.error} />
        </ConfirmDialog>
      )}
    </>
  );
}

export function BackupPage() {
  const mutation = useCreateBackup();
  const key = useRef(newId());
  return (
    <>
      <PageHeading
        title="资料备份与归档"
        subtitle="先保留可核对的快照与版本清单，再确认独立备份。"
      />
      <section className="panel form-stack">
        <h2>创建一致快照</h2>
        <p>
          生成服务器数据库快照与原件版本清单。单独创建清单不代表原件已有独立备份，也不授权删除来源。
        </p>
        <button
          className="button button--primary"
          disabled={mutation.isPending}
          onClick={() => mutation.mutate(key.current)}
        >
          {mutation.isPending
            ? "正在保存快照…"
            : mutation.isError
              ? "重试创建快照"
              : "创建快照与清单"}
        </button>
        <ErrorNotice error={mutation.error} />
        {mutation.data && (
          <div className="notice">
            <strong>快照已创建，等待独立备份</strong>
            <dl className="facts">
              <div>
                <dt>媒体数量</dt>
                <dd>{mutation.data.assetCount}</dd>
              </div>
              <div>
                <dt>时间</dt>
                <dd>{dateTime(mutation.data.createdAt)}</dd>
              </div>
              <div>
                <dt>快照摘要</dt>
                <dd>
                  <code>{mutation.data.snapshotHash}</code>
                </dd>
              </div>
              <div>
                <dt>清单摘要</dt>
                <dd>
                  <code>{mutation.data.manifestHash}</code>
                </dd>
              </div>
            </dl>
          </div>
        )}
      </section>
      <p className="muted">
        浏览器不会据此删除电脑中的源文件。真实迁移与旧备份导入需要通过隔离演练后开放。
      </p>
    </>
  );
}
