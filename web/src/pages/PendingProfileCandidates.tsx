import type { Schema } from "../data/client";
import { useAuthorProfileCandidates } from "../data/author-profiles";
import { ErrorNotice } from "../components/Feedback";
import { ProfileLink, ProfileValuesView } from "./ProfilePresentation";

export function PendingProfileCandidates({ authorId, editable, review }: {
  authorId: string;
  editable: boolean;
  review: (candidate: Schema<"AuthorProfileCandidate">) => void;
}) {
  const candidates = useAuthorProfileCandidates(authorId);
  const pending =
    candidates.data?.pages
      .flatMap((page) => page.items)
      .filter((item) => item.state === "pending") ?? [];
  return (
    <>
      <ErrorNotice
        error={candidates.error}
        stale={pending.length > 0}
        retry={() => void candidates.refetch()}
      />
      {pending.map((item) => (
        <section
          key={item.id}
          className="creator-candidate"
          aria-label="待审核资料"
        >
          <h3>待核对的候选资料</h3>
          <p>核对候选资料与来源后保存。</p>
          <ProfileValuesView
            block={item.block}
            values={item.values}
            onlyPresent
          />
          <nav className="creator-platforms">
            {item.evidence.map((evidence, index) => (
              <ProfileLink key={index} value={evidence.url}>
                {evidence.title || evidence.url} ↗
              </ProfileLink>
            ))}
          </nav>
          <button
            type="button"
            className="pill"
            disabled={!editable}
            onClick={() => review(item)}
          >
            填入候选草稿，继续核对
          </button>
        </section>
      ))}
      {candidates.hasNextPage && (
        <button
          className="pill"
          disabled={candidates.isFetchingNextPage}
          onClick={() => void candidates.fetchNextPage()}
        >
          更多候选记录
        </button>
      )}
    </>
  );
}
