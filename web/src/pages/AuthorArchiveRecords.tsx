import { Link } from "react-router-dom";
import {
  useAuthorProfileArchive,
  useAuthorProfileArchives,
} from "../data/author-profiles";
import {
  EmptyState,
  ErrorNotice,
  Loading,
} from "../components/Feedback";
import { profileBlockTitles } from "../lib/profile-draft";
import {
  AuthorAvatar,
  ProfileEvidenceList,
  ProfileValuesView,
} from "./ProfilePresentation";
import { ProfileCandidates } from "./AdvancedAuthorProfile";

export function AuthorArchiveRecords({ id, archiveId }: { id: string; archiveId: string }) {
  const query = useAuthorProfileArchives(id);
  const detail = useAuthorProfileArchive(id, archiveId);
  const archives = query.data?.pages.flatMap((page) => page.items) ?? [];
  return (
    <>
      <ErrorNotice
        error={query.error}
        stale={archives.length > 0}
        retry={() => void query.refetch()}
      />
      {query.isPending ? (
        <Loading />
      ) : archives.length ? (
        <div className="form-stack section-space">
          {archives.map((archive) => (
            <Link
              key={archive.id}
              className="panel detail-section"
              to={`/authors/${encodeURIComponent(id)}/archives/${encodeURIComponent(archive.id)}`}
            >
              <strong>
                {archive.authorName} ·{" "}
                {archive.reason === "merge"
                  ? "合并保留"
                  : archive.reason === "delete"
                    ? "删除保留"
                    : "迁入原始资料"}
              </strong>
              <span>
                {new Date(archive.createdAt).toLocaleString()} ·{" "}
                {archive.blockCount} 个资料块 · {archive.candidateCount} 条候选
                {archive.conflicts.length
                  ? ` · ${archive.conflicts.length} 项冲突`
                  : ""}
              </span>
            </Link>
          ))}
        </div>
      ) : (
        !query.isError && <EmptyState title="尚无归档资料" />
      )}
      {query.hasNextPage && (
        <button
          className="button"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
        >
          加载更多归档
        </button>
      )}
      {archiveId && (
        <section className="section-space form-stack" aria-label="归档详情">
          <ErrorNotice
            error={detail.error}
            stale={!!detail.data}
            retry={() => void detail.refetch()}
          />
          {detail.isPending ? (
            <Loading />
          ) : (
            detail.data && (
              <>
                <h2>{detail.data.authorName}的保留资料</h2>
                {detail.data.conflicts.length > 0 && (
                  <p className="notice">
                    合并时保留的冲突：{detail.data.conflicts.join("、")}
                    。目标已确认资料没有被覆盖。
                  </p>
                )}
                {detail.data.blocks.map((block) => (
                  <article className="panel" key={block.block}>
                    <h3>
                      {profileBlockTitles[block.block]} · 原修订{" "}
                      {block.revision}
                    </h3>
                    {block.avatar && (
                      <AuthorAvatar
                        name={detail.data!.authorName}
                        avatar={block.avatar}
                        large
                      />
                    )}
                    <ProfileValuesView
                      block={block.block}
                      values={block.values}
                    />
                    <ProfileEvidenceList evidence={block.evidence} />
                  </article>
                ))}
                {detail.data.originalImport && (
                  <details className="panel">
                    <summary>迁入时保留的原始资料</summary>
                    <p className="muted">
                      未转换字段保留在这里，不视为已确认资料。
                    </p>
                    <pre className="profile-raw">
                      {JSON.stringify(detail.data.originalImport, null, 2)}
                    </pre>
                  </details>
                )}
                {detail.data.candidateCount > 0 && (
                  <ProfileCandidates authorId={id} archiveId={archiveId} />
                )}
              </>
            )
          )}
        </section>
      )}
    </>
  );
}
