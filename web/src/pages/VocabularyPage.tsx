import { useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { SegmentedControl } from "@ui/GlassNavigation";
import {
  useTagVocabulary,
  useEditTag,
  useTerms,
  useEditTerm,
} from "../data/catalog";
import type { Schema } from "../data/client";
import {
  ErrorNotice,
  EmptyState,
  Loading,
  PageHeading,
} from "../components/Feedback";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { UnsavedGuard } from "../lib/navigation";
import { newId } from "../lib/id";

export function VocabularyPage() {
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") ?? "tags";
  return (
    <>
      <PageHeading
        title="词表与关联"
        subtitle="标签、作品出处和角色使用独立身份与修订。"
      />
      <SegmentedControl
        label="词表类型"
        value={tab}
        options={[
          { value: "tags", label: "标签" },
          { value: "source", label: "作品出处" },
          { value: "character", label: "角色" },
        ]}
        onChange={(value) => setParams({ tab: value }, { replace: true })}
      />
      <div className="section-space">
        {tab === "tags" ? (
          <TagsVocabulary />
        ) : (
          <TermsVocabulary
            key={tab}
            kind={tab === "character" ? "character" : "source"}
          />
        )}
      </div>
    </>
  );
}
function TagsVocabulary() {
  const query = useTagVocabulary();
  const edit = useEditTag();
  const [name, setName] = useState("");
  const [previous, setPrevious] = useState<Schema<"Tag">>();
  const [deleting, setDeleting] = useState<Schema<"Tag">>();
  const attempt = useRef({ signature: "", key: newId() });
  const items = query.data?.pages.flatMap((page) => page.items) ?? [];
  const keyFor = (signature: string) => {
    if (signature !== attempt.current.signature)
      attempt.current = { signature, key: newId() };
    return attempt.current.key;
  };
  return (
    <>
      <form
        className="panel inline-form"
        onSubmit={(event) => {
          event.preventDefault();
          edit.mutate(
            {
              name,
              previous,
              key: keyFor(`${previous?.name}:${previous?.revision}:${name}`),
            },
            {
              onSuccess: () => {
                setName("");
                setPrevious(undefined);
              },
            },
          );
        }}
      >
        <input
          aria-label="标签名称"
          value={name}
          disabled={edit.isPending}
          maxLength={100}
          placeholder={previous ? `修改“${previous.name}”` : "新标签名称"}
          onChange={(event) => setName(event.target.value)}
          required
        />
        <button
          className="button button--primary"
          disabled={!name.trim() || edit.isPending}
        >
          {previous ? "保存改名" : "创建标签"}
        </button>
        {previous && (
          <button
            type="button"
            className="button"
            disabled={edit.isPending}
            onClick={() => {
              setPrevious(undefined);
              setName("");
            }}
          >
            取消
          </button>
        )}
      </form>
      <ErrorNotice error={edit.error} />
      <ErrorNotice
        error={query.error}
        stale={items.length > 0}
        retry={() => void query.refetch()}
      />
      {query.isPending ? (
        <Loading />
      ) : items.length ? (
        <div className="list-panel">
          {items.map((tag) => (
            <div key={tag.name} className="list-row">
              <div className="row-main">
                <strong>{tag.name}</strong>
                <span>修订 {tag.revision}</span>
              </div>
              <div className="actions">
                <button
                  className="button button--small"
                  disabled={edit.isPending}
                  onClick={() => {
                    setPrevious(tag);
                    setName(tag.name);
                    edit.reset();
                  }}
                >
                  改名
                </button>
                <button
                  className="button button--small button--danger"
                  disabled={edit.isPending}
                  onClick={() => {
                    setDeleting(tag);
                    edit.reset();
                  }}
                >
                  删除词条
                </button>
              </div>
            </div>
          ))}
        </div>
      ) : (
        !query.isError && <EmptyState title="还没有标签词条" />
      )}
      {query.hasNextPage && (
        <button
          className="button"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
        >
          加载更多词条
        </button>
      )}
      {deleting && (
        <ConfirmDialog
          title="删除标签及全部关联？"
          danger
          busy={edit.isPending}
          onCancel={() => setDeleting(undefined)}
          onConfirm={() =>
            edit.mutate(
              {
                name: deleting.name,
                previous: deleting,
                remove: true,
                key: keyFor(`delete:${deleting.name}:${deleting.revision}`),
              },
              { onSuccess: () => setDeleting(undefined) },
            )
          }
        >
          <p>“{deleting.name}”将从词表及已关联媒体中移除；媒体原件保留。</p>
          <ErrorNotice error={edit.error} />
        </ConfirmDialog>
      )}
      <UnsavedGuard dirty={!!name && name !== previous?.name} />
    </>
  );
}
function TermsVocabulary({ kind }: { kind: "source" | "character" }) {
  const [q, setQ] = useState("");
  const query = useTerms(kind, q);
  const sources = useTerms("source");
  const edit = useEditTerm(kind);
  const [draft, setDraft] = useState<
    Schema<"Source"> | Schema<"Character"> | null
  >();
  const [name, setName] = useState("");
  const [aliases, setAliases] = useState("");
  const [sourceId, setSourceId] = useState("");
  const attempt = useRef({ signature: "", key: newId() });
  const [discard, setDiscard] = useState(false);
  const items = query.data?.pages.flatMap((page) => page.items) ?? [];
  const dirty =
    draft !== undefined &&
    (name !== (draft?.name ?? "") ||
      aliases !== (draft?.aliases.join("\n") ?? "") ||
      (kind === "character" &&
        sourceId !== (draft && "sourceId" in draft ? draft.sourceId : "")));
  const open = (item: Schema<"Source"> | Schema<"Character"> | null) => {
    setDraft(item);
    setName(item?.name ?? "");
    setAliases(item?.aliases.join("\n") ?? "");
    setSourceId(item && "sourceId" in item ? item.sourceId : "");
    edit.reset();
  };
  return (
    <>
      <div className="section-heading">
        <label className="search-field">
          查找
          <input
            value={q}
            onChange={(event) => setQ(event.target.value)}
            placeholder="名称或别名"
          />
        </label>
        <button
          className="button"
          disabled={dirty || edit.isPending}
          onClick={() => open(null)}
        >
          新建{kind === "source" ? "作品出处" : "角色"}
        </button>
      </div>
      {draft !== undefined && (
        <form
          className="panel form-stack"
          onSubmit={(event) => {
            event.preventDefault();
            const body = {
              name,
              aliases: aliases
                .split("\n")
                .map((value) => value.trim())
                .filter(Boolean),
              ...(kind === "character" ? { sourceId } : {}),
              ...(draft ? { expectedRevision: draft.revision } : {}),
            };
            const signature = JSON.stringify([draft?.id, body]);
            if (signature !== attempt.current.signature)
              attempt.current = { signature, key: newId() };
            edit.mutate(
              { id: draft?.id, body, key: attempt.current.key },
              { onSuccess: () => setDraft(undefined) },
            );
          }}
        >
          <label>
            名称
            <input
              value={name}
              onChange={(event) => setName(event.target.value)}
              disabled={edit.isPending}
              maxLength={200}
              required
            />
          </label>
          <label>
            别名，每行一个
            <textarea
              value={aliases}
              onChange={(event) => setAliases(event.target.value)}
              disabled={edit.isPending}
            />
          </label>
          {kind === "character" && (
            <>
              <label>
                所属原作 / 出处
                <select
                  value={sourceId}
                  onChange={(event) => setSourceId(event.target.value)}
                  required
                  disabled={edit.isPending || !sources.data}
                >
                  <option value="">选择出处</option>
                  {sources.data?.pages
                    .flatMap((page) => page.items)
                    .map((source) => (
                      <option key={source.id} value={source.id}>
                        {source.name}
                      </option>
                    ))}
                </select>
              </label>
              <ErrorNotice
                error={sources.error}
                retry={() => void sources.refetch()}
              />
              {sources.hasNextPage && (
                <button
                  type="button"
                  className="button"
                  disabled={sources.isFetchingNextPage}
                  onClick={() => void sources.fetchNextPage()}
                >
                  加载更多出处
                </button>
              )}
            </>
          )}
          <ErrorNotice error={edit.error} />
          <div className="actions">
            <button
              className="button button--primary"
              disabled={!dirty || edit.isPending}
            >
              保存词条
            </button>
            <button
              type="button"
              className="button"
              disabled={edit.isPending}
              onClick={() => (dirty ? setDiscard(true) : setDraft(undefined))}
            >
              取消
            </button>
          </div>
        </form>
      )}
      <ErrorNotice
        error={query.error}
        stale={items.length > 0}
        retry={() => void query.refetch()}
      />
      {query.isPending ? (
        <Loading />
      ) : items.length ? (
        <div className="list-panel">
          {items.map((item) => (
            <div className="list-row" key={item.id}>
              <div className="row-main">
                <strong>{item.name}</strong>
                <span>{item.aliases.join(" · ") || "尚未填写别名"}</span>
              </div>
              <button
                className="button button--small"
                disabled={dirty || edit.isPending}
                onClick={() => open(item)}
              >
                编辑
              </button>
            </div>
          ))}
        </div>
      ) : (
        !query.isError && <EmptyState title="没有匹配词条" />
      )}
      {query.hasNextPage && (
        <button
          className="button"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
        >
          加载更多词条
        </button>
      )}
      <UnsavedGuard dirty={dirty} />
      {discard && (
        <ConfirmDialog
          title="放弃词条修改？"
          onCancel={() => setDiscard(false)}
          onConfirm={() => {
            setDraft(undefined);
            setDiscard(false);
          }}
        >
          <p>未保存的草稿将被放弃。</p>
        </ConfirmDialog>
      )}
    </>
  );
}
