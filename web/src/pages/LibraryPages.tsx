import { useRef, useState } from "react";
import { Link } from "react-router-dom";
import {
  useLibraries,
  useCollections,
  useCreateCollection,
  useTrash,
  useTrashAction,
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
import { MoreLibraries } from "../components/MoreLibraries";

export { LibrariesPage } from "./LibrariesPage";

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
          <MoreLibraries query={libraries} />
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


export { BackupPage } from "./BackupPage";
