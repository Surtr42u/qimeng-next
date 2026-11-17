import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import type { Schema } from "../data/client";
import {
  useAuthorProfileCandidates,
  useDismissProfileCandidate,
} from "../data/author-profiles";
import {
  EmptyState,
  ErrorNotice,
  Loading,
  StatusBadge,
} from "../components/Feedback";
import { profileBlockTitles } from "../lib/profile-draft";
import { ProfileBlockEditor } from "./ProfileBlockEditor";
import { ProfileAvatarEditor } from "./ProfileAvatarEditor";
import { ProfileEvidenceList, ProfileValuesView } from "./ProfilePresentation";

type BlockName = Schema<"AuthorProfileBlock">["block"];
type Candidate = Schema<"AuthorProfileCandidate">;

export function AdvancedAuthorProfile({
  author,
  profile,
  reload,
  editingChanged,
}: {
  author: Schema<"Author">;
  profile: Schema<"AuthorProfile">;
  reload: () => Promise<{
    data?: Schema<"AuthorProfile">;
    isSuccess: boolean;
    error?: unknown;
  }>;
  editingChanged: (editing: boolean) => void;
}) {
  const [edit, setEdit] = useState<BlockName | "avatar">();
  const [candidate, setCandidate] = useState<Candidate>();
  const [showCandidates, setShowCandidates] = useState(false);
  const [saved, setSaved] = useState(false);
  useEffect(() => {
    editingChanged(!!edit);
    return () => editingChanged(false);
  }, [edit, editingChanged]);
  const close = () => {
    setEdit(undefined);
    setCandidate(undefined);
    setSaved(true);
  };
  return (
    <section className="section-space form-stack" aria-label="作者高级资料">
      <div className="section-heading">
        <h2>公开资料与整理档案</h2>
        <Link
          className="text-link"
          to={`/authors/${encodeURIComponent(author.id)}/archives`}
        >
          历史归档
        </Link>
      </div>
      <p className="muted">
        X 资料、整理简介和账号分别保存。AI 候选须填入草稿并确认后才会生效。
      </p>
      {saved && (
        <p role="status" className="muted">
          已返回资料详情。已确认内容以服务器回读为准。
        </p>
      )}
      {profile.blocks.map((block) =>
        edit === block.block ? (
          <ProfileBlockEditor
            key={block.block}
            block={block}
            candidate={candidate}
            reload={reload}
            close={close}
          />
        ) : (
          <details
            className="panel profile-block"
            key={block.block}
            open={block.block === "basic" || undefined}
          >
            <summary>
              <strong>{profileBlockTitles[block.block]}</strong>
              <span className="muted">
                {block.revision ? `修订 ${block.revision}` : "尚未填写"}
              </span>
            </summary>
            <ProfileValuesView block={block.block} values={block.values} />
            <details>
              <summary>来源与核实证据（{block.evidence.length}）</summary>
              <ProfileEvidenceList evidence={block.evidence} />
            </details>
            {block.updatedAt && (
              <p className="muted">
                {block.source === "merged"
                  ? "由作者合并保留"
                  : block.source === "imported"
                    ? "迁入资料"
                    : "人工保存"}{" "}
                · {new Date(block.updatedAt).toLocaleString()}
              </p>
            )}
            {profile.editable && (
              <div className="actions section-space">
                <button
                  className="button button--small"
                  disabled={!!edit}
                  onClick={() => {
                    setSaved(false);
                    setEdit(block.block);
                  }}
                >
                  编辑{profileBlockTitles[block.block]}
                </button>
                {block.block === "x" && (
                  <button
                    className="button button--small"
                    disabled={!!edit}
                    onClick={() => {
                      setSaved(false);
                      setEdit("avatar");
                    }}
                  >
                    离线头像
                  </button>
                )}
              </div>
            )}
          </details>
        ),
      )}
      {edit === "avatar" && (
        <ProfileAvatarEditor
          block={profile.blocks.find((block) => block.block === "x")!}
          name={author.name}
          reload={reload}
          close={close}
        />
      )}
      <button
        className="button"
        aria-expanded={showCandidates}
        onClick={() => setShowCandidates(!showCandidates)}
      >
        资料候选与处理记录
      </button>
      {showCandidates && (
        <ProfileCandidates
          authorId={author.id}
          blocks={profile.blocks}
          editable={profile.editable && !edit}
          apply={(item) => {
            setCandidate(item);
            setEdit(item.block);
            setSaved(false);
          }}
        />
      )}
    </section>
  );
}

export function ProfileCandidates({
  authorId,
  archiveId,
  blocks = [],
  editable = false,
  apply,
}: {
  authorId: string;
  archiveId?: string;
  blocks?: Schema<"AuthorProfileBlock">[];
  editable?: boolean;
  apply?: (candidate: Candidate) => void;
}) {
  const query = useAuthorProfileCandidates(authorId, archiveId);
  const dismiss = useDismissProfileCandidate(authorId);
  const candidates = query.data?.pages.flatMap((page) => page.items) ?? [];
  return (
    <div className="form-stack">
      <ErrorNotice
        error={query.error}
        stale={candidates.length > 0}
        retry={() => void query.refetch()}
      />
      {query.isPending ? (
        <Loading />
      ) : candidates.length ? (
        candidates.map((candidate) => (
          <article className="panel profile-candidate" key={candidate.id}>
            <div className="section-heading">
              <h3>{profileBlockTitles[candidate.block]}候选</h3>
              <StatusBadge
                tone={candidate.state === "applied" ? "success" : "neutral"}
              >
                {candidate.state === "pending"
                  ? "待核对"
                  : candidate.state === "applied"
                    ? `已保存为修订 ${candidate.appliedRevision}`
                    : "已忽略"}
              </StatusBadge>
            </div>
            <p className="muted">
              {new Date(candidate.createdAt).toLocaleString()} · 基于修订{" "}
              {candidate.baseRevision}
              {candidate.authorTaskId ? " · 资料维护任务" : " · 管理员提交"}
            </p>
            <div className="profile-comparison">
              {!archiveId && (
                <section>
                  <h4>当前已确认资料</h4>
                  <ProfileValuesView
                    block={candidate.block}
                    values={
                      blocks.find((block) => block.block === candidate.block)
                        ?.values ?? {}
                    }
                  />
                </section>
              )}
              <section>
                <h4>候选建议</h4>
                <ProfileValuesView
                  block={candidate.block}
                  values={candidate.values}
                  onlyPresent
                />
              </section>
            </div>
            <ProfileEvidenceList evidence={candidate.evidence} />
            {candidate.state === "pending" && !archiveId && (
              <div className="actions">
                <button
                  className="button button--primary"
                  disabled={!editable || dismiss.isPending}
                  onClick={() => apply?.(candidate)}
                >
                  填入草稿并核对
                </button>
                <button
                  className="button"
                  disabled={!editable || dismiss.isPending}
                  onClick={() => dismiss.mutate(candidate.id)}
                >
                  忽略此候选
                </button>
              </div>
            )}
          </article>
        ))
      ) : (
        !query.isError && <EmptyState title="没有资料候选记录" />
      )}
      <ErrorNotice error={dismiss.error} />
      {query.hasNextPage && (
        <button
          className="button"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
        >
          加载更多候选记录
        </button>
      )}
    </div>
  );
}
