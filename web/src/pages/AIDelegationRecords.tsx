import type { Schema } from "../data/client";
import { useAIDelegations } from "../data/ai-access";
import { EmptyState, ErrorNotice, Loading, StatusBadge } from "../components/Feedback";
import { dateTime } from "../lib/format";
import { PolicySummary } from "./AIPolicySummary";

export function AIDelegationRecords({ state, revoking, requestRevoke }: {
  state: Schema<"AIDelegation">["state"] | "all";
  revoking: boolean;
  requestRevoke: (item: Schema<"AIDelegation">) => void;
}) {
  const query = useAIDelegations(state);
  const items = query.data?.pages.flatMap((page) => page.items) ?? [];
  return (
    <>
      <ErrorNotice
        error={query.error}
        stale={!!query.data}
        retry={() => void query.refetch()}
      />
      {query.isPending ? (
        <Loading />
      ) : items.length ? (
        <div className="form-stack section-space">
          {items.map((item) => (
            <article className="panel form-stack" key={item.id}>
              <div className="section-heading">
                <h2>{item.label}</h2>
                <StatusBadge
                  tone={item.state === "active" ? "success" : "neutral"}
                >
                  {item.state === "active"
                    ? "授权有效"
                    : item.state === "revoked"
                      ? "已撤销"
                      : "已到期"}
                </StatusBadge>
              </div>
              <PolicySummary policy={item} />
              <p className="muted">
                建立于 {dateTime(item.createdAt)} · 到期{" "}
                {dateTime(item.expiresAt)}
              </p>
              {item.state === "active" && (
                <button
                  className="button button--danger"
                  disabled={revoking}
                  onClick={() => requestRevoke(item)}
                >
                  撤销此接入
                </button>
              )}
            </article>
          ))}
        </div>
      ) : (
        !query.isError && (
          <EmptyState
            title={
              state === "active" ? "尚无有效 AI 接入" : "没有此状态的接入记录"
            }
          >
            配置范围后把一次性凭据交给自己的可信执行器。日常聊天无需逐次返回 App
            授权。
          </EmptyState>
        )
      )}
      {query.hasNextPage && (
        <button
          className="button"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
        >
          加载更多接入记录
        </button>
      )}
    </>
  );
}
