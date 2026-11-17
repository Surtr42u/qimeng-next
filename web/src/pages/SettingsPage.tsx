import { useRef, useState } from "react";
import { Link } from "react-router-dom";
import { useSettings } from "../data/manage";
import { useSaveSettings } from "../data/settings";
import { useLogout } from "../data/session";
import type { Schema } from "../data/client";
import { ErrorNotice, Loading } from "../components/Feedback";
import { Switch } from "../components/LegacySwitch";
import { RecommendationSettings } from "./RecommendationSettings";
import { UnsavedGuard } from "../lib/navigation";
import { useThumbnailPreference } from "../lib/thumbnail-preference";
import { newId } from "../lib/id";

export function SettingsPage() {
  const query = useSettings();
  return (
    <div className="page" id="page-settings">
      <div className="page-head">
        <h1 data-page-heading tabIndex={-1}>
          设置
        </h1>
        <p>调整浏览体验与媒体库行为</p>
      </div>
      <ErrorNotice
        error={query.error}
        stale={!!query.data}
        retry={() => void query.refetch()}
      />
      {query.isPending ? (
        <Loading />
      ) : (
        query.data && (
          <SettingsForm
            initial={query.data}
            reload={async () => (await query.refetch()).data}
          />
        )
      )}
    </div>
  );
}
type Draft = { workers: string; thumbEdge: string; autoAccept: boolean };
const draftOf = (value: Schema<"Settings">): Draft => ({
  workers: String(value.scan.workers),
  thumbEdge: String(value.scan.thumbEdge),
  autoAccept: value.upload.autoAccept,
});
const sameDraft = (a: Draft, b: Draft) =>
  a.workers === b.workers &&
  a.thumbEdge === b.thumbEdge &&
  a.autoAccept === b.autoAccept;
function SettingsForm({
  initial,
  reload,
}: {
  initial: Schema<"Settings">;
  reload: () => Promise<Schema<"Settings"> | undefined>;
}) {
  const [base, setBase] = useState(initial);
  const [draft, setDraft] = useState(() => draftOf(initial));
  const liveDraft = useRef(draft);
  liveDraft.current = draft;
  const [saved, setSaved] = useState(false);
  const [comparison, setComparison] = useState<Schema<"Settings">>();
  const [readError, setReadError] = useState<unknown>();
  const mutation = useSaveSettings();
  const logout = useLogout();
  const attempt = useRef({ signature: "", key: newId() });
  const dirty = !sameDraft(draft, draftOf(base));
  const workers = Number(draft.workers),
    edge = Number(draft.thumbEdge);
  const invalid =
    !draft.workers ||
    !draft.thumbEdge ||
    !Number.isInteger(workers) ||
    workers < 0 ||
    workers > 32 ||
    !Number.isInteger(edge) ||
    edge < 200 ||
    edge > 1600;
  const edit = (value: Partial<Draft>) => {
    setDraft((current) => ({ ...current, ...value }));
    setSaved(false);
  };
  const save = (snapshot = draft) => {
    const body = {
      expectedRevision: base.revision,
      scan: {
        workers: Number(snapshot.workers),
        thumbEdge: Number(snapshot.thumbEdge),
      },
      upload: { autoAccept: snapshot.autoAccept },
    };
    const signature = JSON.stringify(body);
    if (attempt.current.signature !== signature)
      attempt.current = { signature, key: newId() };
    mutation.mutate(
      { body, key: attempt.current.key },
      {
        onSuccess: (value) => {
          setBase(value);
          setSaved(true);
          setComparison(undefined);
          if (sameDraft(liveDraft.current, snapshot)) setDraft(draftOf(value));
        },
      },
    );
  };
  const automatic = (value: boolean) => {
    const snapshot = {
      ...draft,
      workers: value
        ? "0"
        : String(base.scan.workers || base.effectiveScanWorkers),
    };
    setDraft(snapshot);
    liveDraft.current = snapshot;
    setSaved(false);
    save(snapshot);
  };
  return (
    <>
      <UnsavedGuard dirty={dirty || mutation.isPending} />
      <div className="settings-content">
        <div className="settings-card" id="settings-library">
          <h3>扫描</h3>
          <p>媒体库扫描并发 · 保存后用于后续扫描</p>
          <div className="settings-grid">
            <div className="settings-field">
              <span>扫描并发数</span>
              <span className="settings-switch">
                <Switch
                  checked={draft.workers === "0"}
                  disabled={mutation.isPending || invalid}
                  onCheckedChange={automatic}
                  aria-label="自动调整扫描并发"
                />
                自动调整
              </span>
              {draft.workers !== "0" && (
                <input
                  id="scan-workers"
                  aria-label="扫描并发数"
                  type="number"
                  min={1}
                  max={32}
                  step={1}
                  value={draft.workers}
                  onChange={(event) => edit({ workers: event.target.value })}
                />
              )}
              <small>
                切换模式立即保存；手动数量修改后点“保存设置”。自动按CPU与可用内存分配。
              </small>
              {mutation.isPending && (
                <small role="status">正在保存，请稍候…</small>
              )}
            </div>
            <label className="settings-field">
              <span>缩略图长边</span>
              <input
                type="number"
                min={200}
                max={1600}
                step={1}
                value={draft.thumbEdge}
                onChange={(event) => edit({ thumbEdge: event.target.value })}
              />
              <small>200–1600 px · 预留字段，暂不生效</small>
            </label>
          </div>
        </div>
        <div className="settings-card">
          <h3>上传</h3>
          <p>客户端上传行为约束 · 保存即生效</p>
          <p>不设单文件大小上限；保留类型、路径、可用空间与暂存总预算检查</p>
          <label className="settings-switch">
            <Switch
              checked={draft.autoAccept}
              onCheckedChange={(autoAccept) => edit({ autoAccept })}
            />
            <span>自动接收上传</span>
          </label>
        </div>
        <div className="settings-card">
          <h3>来源与镜像</h3>
          <p>维护常用获取渠道，或将作者总表单向导出到服务器本地 TXT。</p>
          <div className="settings-options">
            <Link className="pill" to="/authors">
              编辑作者资料
            </Link>
            <Link className="pill" to="/manage/acquisition">
              维护来源与镜像
            </Link>
            <Link className="pill" to="/settings/ai">
              AI 接入
            </Link>
          </div>
        </div>
        <div className="settings-preferences" id="settings-preferences">
          <div className="settings-card">
            <h3>界面</h3>
            <p>外观与主题</p>
            <div className="settings-muted">
              <span>主题模式</span>
              <span>默认深色（侧栏月亮按钮切换并记忆）</span>
            </div>
            <p>缩略图清晰度（全站图片，点击即生效并记忆）</p>
            <ThumbnailOptions />
          </div>
          <div className="settings-preference-column">
            <RecommendationSettings />
            <div
              className="settings-actions"
              role="group"
              aria-label="设置操作"
            >
              <span className="save-tip" role="status">
                {mutation.isPending
                  ? "正在保存…"
                  : dirty
                    ? "有未保存的修改"
                    : saved
                      ? "设置已保存"
                      : "修改后保存"}
              </span>
              <button
                className="save-btn"
                type="button"
                disabled={mutation.isPending || invalid}
                onClick={() => save()}
              >
                {mutation.isPending ? "保存中…" : "保存设置"}
              </button>
              <button
                className="save-btn"
                type="button"
                disabled={logout.isPending || mutation.isPending}
                onClick={() => logout.mutate()}
              >
                {logout.isPending ? "登出中…" : "退出登录"}
              </button>
            </div>
          </div>
        </div>
        <ErrorNotice error={mutation.error ?? logout.error} />
        {mutation.isError && (
          <button
            className="pill"
            onClick={async () => {
              const value = await reload();
              if (value) {
                setComparison(value);
                setReadError(undefined);
              } else
                setReadError(new Error("未取得服务器当前设置，草稿仍保留。"));
            }}
          >
            读取当前设置并对照
          </button>
        )}
        <ErrorNotice error={readError} />
        {comparison && (
          <div className="manage-notice manage-notice--warning">
            <p>
              当前服务器：扫描并发{" "}
              {comparison.scan.workers === 0 ? "自动" : comparison.scan.workers}
              ，缩略图长边 {comparison.scan.thumbEdge}，自动接收上传
              {comparison.upload.autoAccept ? "开启" : "关闭"}。
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
      </div>
    </>
  );
}
function ThumbnailOptions() {
  const [value, setValue] = useThumbnailPreference();
  const [error, setError] = useState(false);
  return (
    <>
      <div className="settings-options">
        {[
          { value: "auto", label: "跟随服务端" },
          { value: "sm", label: "省流量" },
          { value: "lg", label: "高清" },
        ].map((item) => (
          <button
            className={`pill${item.value === value ? " active" : ""}`}
            type="button"
            key={item.value}
            aria-pressed={item.value === value}
            onClick={() => setError(!setValue(item.value as typeof value))}
          >
            {item.label}
          </button>
        ))}
      </div>
      {error && <p role="alert">清晰度已应用，浏览器无法保存偏好。</p>}
    </>
  );
}
