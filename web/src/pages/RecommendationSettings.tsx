import { useRef, useState } from "react";
import {
  useRecommendationPreferences,
  useSaveRecommendationPreferences,
} from "../data/analytics";
import type { Schema } from "../data/client";
import { ErrorNotice, Loading } from "../components/Feedback";
import { newId } from "../lib/id";

type Weights = Schema<"RecommendationWeights">;
const presets = [
  ["balanced", "均衡推荐"],
  ["familiar", "高记忆流行"],
  ["explore", "深度探索"],
  ["fresh", "新鲜优先"],
] as const;
const same = (left: Weights, right: Weights) =>
  Object.keys(right).every(
    (key) => left[key as keyof Weights] === right[key as keyof Weights],
  );
export function RecommendationSettings() {
  const query = useRecommendationPreferences();
  const mutation = useSaveRecommendationPreferences();
  const preferences = query.data;
  const attempt = useRef({ signature: "", key: newId() });
  const [saved, setSaved] = useState(false);
  const apply = (weights: Weights) => {
    if (!query.data) return;
    const body = { expectedRevision: query.data.revision, weights };
    const signature = JSON.stringify(body);
    if (attempt.current.signature !== signature)
      attempt.current = { signature, key: newId() };
    setSaved(false);
    mutation.mutate(
      { body, operationId: attempt.current.key },
      { onSuccess: () => setSaved(true) },
    );
  };
  return (
    <div className="settings-card">
      <h3>推荐偏好</h3>
      <p>作用于首页推荐流的 9 维权重；点击预设立即保存生效</p>
      <ErrorNotice
        error={query.error}
        stale={!!query.data}
        retry={() => void query.refetch()}
      />
      {query.isPending ? (
        <Loading />
      ) : (
        preferences && (
          <div className="settings-options">
            {presets.map(([key, label]) => {
              const weights = preferences.presets[key];
              return (
                <button
                  key={key}
                  type="button"
                  className={`pill${weights && same(preferences.weights, weights) ? " active" : ""}`}
                  aria-pressed={!!weights && same(preferences.weights, weights)}
                  disabled={mutation.isPending || !weights}
                  onClick={() => {
                    if (weights) apply(weights);
                  }}
                >
                  {label}
                </button>
              );
            })}
          </div>
        )
      )}
      <ErrorNotice
        error={mutation.error}
        retry={() => {
          void query.refetch();
          mutation.reset();
        }}
      />
      {saved && (
        <p className="save-tip" role="status">
          推荐偏好已保存
        </p>
      )}
    </div>
  );
}
