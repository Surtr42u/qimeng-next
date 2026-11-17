import { useRef, useState } from "react";
import {
  useRecommendationPreferences,
  useSaveRecommendationPreferences,
} from "../data/analytics";
import type { Schema } from "../data/client";
import { ErrorNotice, Loading } from "../components/Feedback";
import { UnsavedGuard } from "../lib/navigation";
import { newId } from "../lib/id";

type Weights = Schema<"RecommendationWeights">;
const fields: { key: keyof Weights; label: string }[] = [
  { key: "tagRelevance", label: "标签相关" },
  { key: "tagCollection", label: "标签收藏" },
  { key: "engagement", label: "互动" },
  { key: "recency", label: "近期浏览" },
  { key: "likeScore", label: "点赞" },
  { key: "discovery", label: "发现" },
  { key: "freshness", label: "新入库" },
  { key: "browseDepth", label: "浏览深度" },
  { key: "maxRandom", label: "随机范围" },
];
const same = (left: Weights, right: Weights) =>
  fields.every((field) => left[field.key] === right[field.key]);
const presetNames: Record<string, string> = {
  balanced: "均衡",
  exploration: "探索",
  explore: "探索",
  familiar: "熟悉",
  favorites: "偏爱",
  focus: "专注",
  diverse: "多样",
};
export function RecommendationSettings() {
  const query = useRecommendationPreferences();
  return (
    <section className="panel form-stack recommendation-settings">
      <h2>推荐偏好</h2>
      <ErrorNotice
        error={query.error}
        stale={!!query.data}
        retry={() => void query.refetch()}
      />
      {query.isPending ? (
        <Loading />
      ) : (
        query.data && (
          <PreferenceEditor
            initial={query.data}
            reload={() => query.refetch()}
          />
        )
      )}
    </section>
  );
}
function PreferenceEditor({
  initial,
  reload,
}: {
  initial: Schema<"RecommendationPreferences">;
  reload: () => Promise<{ data?: Schema<"RecommendationPreferences"> }>;
}) {
  const [base, setBase] = useState(initial);
  const [draft, setDraft] = useState(initial.weights);
  const latestDraft = useRef(draft);
  latestDraft.current = draft;
  const [comparison, setComparison] =
    useState<Schema<"RecommendationPreferences">>();
  const [saved, setSaved] = useState(false);
  const [readError, setReadError] = useState<unknown>(null);
  const mutation = useSaveRecommendationPreferences();
  const attempt = useRef({ signature: "", key: newId() });
  const dirty = !same(draft, base.weights);
  const save = () => {
    const weights = { ...draft };
    const signature = JSON.stringify([base.revision, weights]);
    if (signature !== attempt.current.signature)
      attempt.current = { signature, key: newId() };
    mutation.mutate(
      {
        body: { expectedRevision: base.revision, weights },
        operationId: attempt.current.key,
      },
      {
        onSuccess: (response) => {
          setBase(response);
          setSaved(true);
          setComparison(undefined);
          // Only the submitted snapshot is confirmed; edits made while PUT was in flight survive.
          if (same(latestDraft.current, weights)) setDraft(response.weights);
        },
      },
    );
  };
  return (
    <>
      <div className="actions">
        {Object.entries(base.presets).map(([name, weights]) => (
          <button
            key={name}
            type="button"
            className={`button button--small ${same(draft, weights) ? "button--selected" : ""}`}
            aria-pressed={same(draft, weights)}
            onClick={() => {
              setDraft({ ...weights });
              setSaved(false);
            }}
          >
            {presetNames[name] ?? name}
          </button>
        ))}
      </div>
      <p className="muted">
        预设和各项权重来自服务器，客户端不重新计算推荐分数。
      </p>
      <details>
        <summary>自定义权重</summary>
        <div className="preference-fields">
          {fields.map((field) => (
            <label key={field.key}>
              {field.label}
              <input
                type="number"
                min={0}
                max={1}
                step={0.01}
                value={draft[field.key]}
                onChange={(event) => {
                  const value = event.target.valueAsNumber;
                  if (Number.isFinite(value)) {
                    setDraft((current) => ({ ...current, [field.key]: value }));
                    setSaved(false);
                  }
                }}
              />
            </label>
          ))}
        </div>
      </details>
      <ErrorNotice error={mutation.error} />
      <ErrorNotice error={readError} />
      {mutation.isError && (
        <button
          type="button"
          className="button"
          onClick={() => {
            void reload().then((result) => {
              if (result.data) {
                setComparison(result.data);
                setReadError(null);
              } else
                setReadError(
                  new Error("尚未取得服务器当前偏好，草稿保持不变。"),
                );
            }, setReadError);
          }}
        >
          读取服务器当前偏好
        </button>
      )}
      {comparison && (
        <div className="notice">
          <strong>核对当前服务器修订 {comparison.revision}</strong>
          <dl className="facts">
            {fields
              .filter(
                (field) => comparison.weights[field.key] !== draft[field.key],
              )
              .map((field) => (
                <div key={field.key}>
                  <dt>{field.label}</dt>
                  <dd>
                    服务器 {comparison.weights[field.key]} / 草稿{" "}
                    {draft[field.key]}
                  </dd>
                </div>
              ))}
          </dl>
          <button
            className="button button--small"
            onClick={() => {
              setBase(comparison);
              setComparison(undefined);
              mutation.reset();
            }}
          >
            已核对，保留草稿继续编辑
          </button>
        </div>
      )}
      <div className="preference-save">
        <p className="muted" role="status">
          {mutation.isPending
            ? "正在保存当前快照，可以继续编辑…"
            : dirty
              ? "有未保存的修改"
              : saved
                ? "偏好已保存"
                : "当前偏好已载入"}
        </p>
        <div className="actions">
          <button
            className="button button--primary"
            disabled={
              !dirty ||
              mutation.isPending ||
              fields.some(
                (field) => draft[field.key] < 0 || draft[field.key] > 1,
              )
            }
            onClick={save}
          >
            {mutation.isPending ? "保存中…" : "保存偏好"}
          </button>
          <button
            className="button"
            disabled={!dirty || mutation.isPending}
            onClick={() => {
              setDraft(base.weights);
              mutation.reset();
            }}
          >
            还原已保存偏好
          </button>
        </div>
      </div>
      <UnsavedGuard dirty={dirty || mutation.isPending} />
    </>
  );
}
