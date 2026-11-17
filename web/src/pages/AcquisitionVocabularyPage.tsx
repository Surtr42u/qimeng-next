import { useRef, useState } from "react";
import {
  useAcquisitionVocabulary,
  useUpdateAcquisitionVocabulary,
} from "../data/acquisition";
import type { Schema } from "../data/client";
import { ManagePageHeader } from "../components/LegacyManageHeader";
import { ErrorNotice, Loading } from "../components/Feedback";
import { UnsavedGuard } from "../lib/navigation";
import { newId } from "../lib/id";

export function AcquisitionVocabularyPage() {
  const query = useAcquisitionVocabulary();
  return (
    <div
      className="page manage-workspace"
      id="page-maintenance-files-vocabulary"
    >
      <ManagePageHeader
        title="来源词表与镜像"
        description="来源词表用于获取渠道建议；镜像用于导出作者清单，两者分别保存"
      />
      <ErrorNotice
        error={query.error}
        stale={!!query.data}
        retry={() => void query.refetch()}
      />
      {query.isPending ? (
        <Loading />
      ) : (
        query.data && (
          <VocabularyEditor
            initial={query.data}
            reload={async () => (await query.refetch()).data}
          />
        )
      )}
      <div className="settings-card">
        <h3>作者总表镜像</h3>
        <p>当前服务版本尚未提供作者清单镜像配置与导出状态，未执行任何导出。</p>
      </div>
    </div>
  );
}
function VocabularyEditor({
  initial,
  reload,
}: {
  initial: Schema<"AcquisitionVocabulary">;
  reload: () => Promise<Schema<"AcquisitionVocabulary"> | undefined>;
}) {
  const [base, setBase] = useState(initial);
  const [draft, setDraft] = useState<string[] | null>(null);
  const [input, setInput] = useState("");
  const [comparison, setComparison] =
    useState<Schema<"AcquisitionVocabulary">>();
  const [readError, setReadError] = useState<unknown>();
  const [saved, setSaved] = useState(false);
  const mutation = useUpdateAcquisitionVocabulary();
  const attempt = useRef({ signature: "", key: newId() });
  const current = draft ?? base.sources;
  const add = () => {
    const value = input.trim();
    if (value && !current.includes(value)) setDraft([...current, value]);
    setInput("");
    setSaved(false);
  };
  const save = () => {
    const body = { expectedRevision: base.revision, sources: current };
    const signature = JSON.stringify(body);
    if (attempt.current.signature !== signature)
      attempt.current = { signature, key: newId() };
    mutation.mutate(
      { ...body, key: attempt.current.key },
      {
        onSuccess: (value) => {
          setBase(value);
          setDraft(null);
          setSaved(true);
          setComparison(undefined);
        },
      },
    );
  };
  return (
    <div className="settings-card">
      <h3>通用来源词表</h3>
      <p>
        资产编辑页来源区与上传挂靠来源区的建议词（获取渠道/平台名，如「forum-c」）·
        全员共享，服务端统一保存
      </p>
      <div className="source-chips">
        {current.map((value) => (
          <button
            className="pill active"
            key={value}
            type="button"
            disabled={mutation.isPending}
            title="点击移除该建议词"
            onClick={() => {
              setDraft(current.filter((item) => item !== value));
              setSaved(false);
            }}
          >
            {value}
          </button>
        ))}
        {!current.length && (
          <span className="source-empty-hint">
            暂无建议词，在下方输入框回车添加
          </span>
        )}
      </div>
      <div className="tag-newrow">
        <input
          className="tag-input"
          type="text"
          placeholder="新来源词，回车加入词表草稿"
          autoComplete="off"
          value={input}
          disabled={mutation.isPending}
          onChange={(event) => setInput(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.nativeEvent.isComposing) {
              event.preventDefault();
              add();
            }
          }}
        />
      </div>
      <small>
        点击词表条目移除 · 回车添加（自动去空白，重复词不加）· 保存为整体替换
      </small>
      <div className="settings-actions" style={{ marginTop: 12 }}>
        <button
          className="save-btn"
          type="button"
          disabled={mutation.isPending || draft === null}
          onClick={save}
        >
          {mutation.isPending ? "保存中…" : "保存来源词表"}
        </button>
        <button
          className="pill"
          disabled={mutation.isPending || draft === null}
          onClick={() => {
            setDraft(null);
            setInput("");
            mutation.reset();
          }}
        >
          放弃修改
        </button>
        {saved && (
          <span className="save-tip" role="status">
            通用来源词表已保存
          </span>
        )}
      </div>
      <ErrorNotice error={mutation.error} />
      {mutation.isError && (
        <button
          className="pill"
          onClick={async () => {
            const value = await reload();
            if (value) {
              setComparison(value);
              setReadError(undefined);
            } else
              setReadError(new Error("未取得当前来源词表，草稿保持不变。"));
          }}
        >
          读取当前词表并对照
        </button>
      )}
      <ErrorNotice error={readError} />
      {comparison && (
        <div className="manage-notice manage-notice--warning">
          <p>
            服务器当前词表：{comparison.sources.join(" · ") || "空词表"}
            。保存会用完整草稿替换这一版本。
          </p>
          <button
            className="pill"
            onClick={() => {
              setBase(comparison);
              setComparison(undefined);
              mutation.reset();
            }}
          >
            已核对，保留草稿继续
          </button>
        </div>
      )}
      <UnsavedGuard dirty={draft !== null || mutation.isPending} />
    </div>
  );
}
