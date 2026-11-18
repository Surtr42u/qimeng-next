import { useState } from "react";
import type { Schema } from "../data/client";
import { useUpdateAuthorProfile } from "../data/author-profiles";
import { ErrorNotice } from "../components/Feedback";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { UnsavedGuard } from "../lib/navigation";
import { profilePatch, rebaseProfileDraft } from "../lib/profile-draft";
import type { ProfileValues } from "../lib/profile-draft";
import { ProfileEvidenceEditor } from "./ProfileEvidenceEditor";
import {
  AuthorAvatar,
  ProfileEvidenceList,
  ProfileValuesView,
} from "./ProfilePresentation";

type Block = Schema<"AuthorProfileBlock">;
type Candidate = Schema<"AuthorProfileCandidate">;
type ProfileRead = () => Promise<{
  data?: Schema<"AuthorProfile">;
  isSuccess: boolean;
  error?: unknown;
}>;

export function ProfileBlockEditor({
  block,
  candidate,
  reload,
  close,
  authorName = "作者",
  avatar,
}: {
  block: Block;
  candidate?: Candidate;
  reload: ProfileRead;
  close: () => void;
  authorName?: string;
  avatar?: Schema<"AuthorProfileAvatar">;
}) {
  const [base, setBase] = useState(block);
  const [draft, setDraft] = useState<ProfileValues>({
    ...block.values,
    ...candidate?.values,
  });
  const [evidence, setEvidence] = useState(
    candidate?.evidence ?? block.evidence,
  );
  const [editChinese, setEditChinese] = useState(false);
  const [translationReviewed, setTranslationReviewed] = useState(
    !!candidate?.values.biographyZh,
  );
  const [discard, setDiscard] = useState(false);
  const [comparison, setComparison] = useState<Block>();
  const [readError, setReadError] = useState<unknown>();
  const mutation = useUpdateAuthorProfile(block.authorId, block.block);
  const changes = profilePatch(base.values, draft);
  const evidenceChanged =
    JSON.stringify(evidence) !== JSON.stringify(base.evidence);
  const dirty =
    Object.keys(changes).length > 0 || evidenceChanged || !!candidate;
  const validEvidence = evidence.every(
    (item) => item.url.trim() && item.fields.length > 0,
  );
  const title = candidate
    ? "候选核对"
    : block.block === "x"
        ? "X资料"
        : block.block === "basic"
          ? "基础资料"
          : "账号来源";
  const update = (patch: ProfileValues) =>
    setDraft((current) => ({ ...current, ...patch }));
  const updateAccount = (patch: Partial<Schema<"AuthorProfileAccount">>) => {
    const accounts = [...(draft.accounts ?? [])];
    accounts[0] = {
      platform: "公开主页",
      identifier: "",
      url: "",
      publishingUrl: "",
      ...accounts[0],
      ...patch,
    };
    update({ accounts });
  };
  const closeEditor = () => (dirty ? setDiscard(true) : close());
  const save = () => {
    const values = { ...changes };
    if (
      (changes.biographyOriginal !== undefined ||
        changes.profileUrl !== undefined) &&
      translationReviewed
    )
      values.biographyZh = draft.biographyZh ?? "";
    mutation.mutate(
      {
        expectedRevision: base.revision,
        values,
        ...(evidenceChanged || candidate ? { evidence } : {}),
        ...(candidate ? { candidateId: candidate.id } : {}),
        translationReviewed,
      },
      { onSuccess: close },
    );
  };
  const loadCurrent = async () => {
    const result = await reload();
    const current = result.data?.blocks.find(
      (item) => item.block === block.block,
    );
    if (result.isSuccess && current) {
      setComparison(current);
      setReadError(undefined);
    } else
      setReadError(result.error ?? new Error("未取得当前资料，草稿保持不变。"));
  };
  const rebase = () => {
    if (!comparison) return;
    setDraft(rebaseProfileDraft(base.values, draft, comparison.values));
    if (!evidenceChanged) setEvidence(comparison.evidence);
    setBase(comparison);
    setComparison(undefined);
    setTranslationReviewed(!!candidate?.values.biographyZh);
    mutation.reset();
  };
  const biography =
    (editChinese ? draft.biographyZh : draft.biographyOriginal) ?? "";
  return (
    <section className="creator-editor" aria-label={`编辑${title}`}>
      <div className="creator-editor-heading">
        <div className="creator-editor-person">
          <AuthorAvatar
            className="creator-profile-avatar"
            name={authorName}
            avatar={avatar}
          />
          <div>
            <p>{authorName}</p>
            <h3>编辑{title}</h3>
          </div>
        </div>
        <button
          type="button"
          className="pill"
          onClick={closeEditor}
          disabled={mutation.isPending}
        >
          返回概览
        </button>
      </div>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          save();
        }}
      >
        <fieldset className="creator-edit-fields" disabled={mutation.isPending}>
          <legend>{title}</legend>
          {block.block === "x" ? (
            <>
              <label className="settings-field">
                <span>X 账号主页</span>
                <input
                  type="url"
                  placeholder="https://x.com/账号名"
                  maxLength={4096}
                  value={draft.profileUrl ?? ""}
                  onChange={(event) => {
                    update({ profileUrl: event.target.value, biographyZh: "" });
                    setTranslationReviewed(false);
                  }}
                />
              </label>
              <div className="creator-edit-language">
                <div
                  className="creator-language"
                  role="group"
                  aria-label="简介编辑语言"
                >
                  <button
                    type="button"
                    aria-pressed={!editChinese}
                    onClick={() => setEditChinese(false)}
                  >
                    原文
                  </button>
                  <button
                    type="button"
                    aria-pressed={editChinese}
                    onClick={() => setEditChinese(true)}
                  >
                    中文
                  </button>
                </div>
                <span>分别保存原文与译文</span>
              </div>
              <label className="settings-field">
                <span>{editChinese ? "中文译文" : "X 简介原文"}</span>
                <textarea
                  rows={6}
                  aria-label={editChinese ? "中文译文" : "X 简介原文"}
                  maxLength={editChinese ? 2000 : 1000}
                  placeholder={
                    editChinese ? "填写或校对中文翻译…" : "粘贴作者的X简介原文…"
                  }
                  value={biography}
                  onChange={(event) => {
                    if (editChinese) {
                      update({ biographyZh: event.target.value });
                      setTranslationReviewed(true);
                    } else {
                      update({
                        biographyOriginal: event.target.value,
                        biographyZh: "",
                      });
                      setTranslationReviewed(false);
                    }
                  }}
                />
                <small>
                  {Array.from(biography).length} / {editChinese ? 2000 : 1000}
                </small>
              </label>
              <p className="creator-profile-note">
                修改原文后需重新校对中文译文，旧译文会清空。语言切换保留当前草稿。
              </p>
            </>
          ) : block.block === "basic" ? (
            <>
              <label className="settings-field">
                <span>公开昵称</span>
                <input
                  maxLength={200}
                  value={draft.publicName ?? ""}
                  onChange={(event) =>
                    update({ publicName: event.target.value })
                  }
                />
              </label>
              <label className="settings-field">
                <span>整理简介</span>
                <textarea
                  rows={5}
                  maxLength={20000}
                  value={draft.summary ?? ""}
                  onChange={(event) => update({ summary: event.target.value })}
                />
              </label>
              <label className="settings-field">
                <span>特殊情况备注</span>
                <textarea
                  rows={3}
                  maxLength={500}
                  value={draft.remark ?? ""}
                  onChange={(event) => update({ remark: event.target.value })}
                  placeholder="退圈、账号删除或仅有存档证据等特殊情况；常规作者留空"
                />
              </label>
              <details className="creator-records">
                <summary>个人整理备注</summary>
                <label className="settings-field">
                  <span>私人备注</span>
                  <textarea
                    rows={5}
                    maxLength={10000}
                    value={draft.note ?? ""}
                    onChange={(event) => update({ note: event.target.value })}
                  />
                </label>
                <p className="creator-profile-note">
                  备注保存在影库中；具有全库读取权限的 AI 接入也能读取。
                </p>
              </details>
            </>
          ) : (
            <>
              <label className="settings-field">
                <span>创作者主页</span>
                <input
                  type="url"
                  placeholder="https://…"
                  maxLength={4096}
                  value={draft.accounts?.[0]?.url ?? ""}
                  onChange={(event) =>
                    updateAccount({ url: event.target.value })
                  }
                />
              </label>
              <label className="settings-field">
                <span>核验链接（每行一个）</span>
                <textarea
                  rows={4}
                  value={evidence.map((item) => item.url).join("\n")}
                  onChange={(event) =>
                    setEvidence(
                      event.target.value
                        .split("\n")
                        .filter(Boolean)
                        .map(
                          (url) =>
                            evidence.find((item) => item.url === url) ?? {
                              url,
                              title: "",
                              fields: ["accounts"],
                              status: "unverified",
                              note: "",
                            },
                        ),
                    )
                  }
                />
              </label>
              <label className="settings-field">
                <span>身份状态</span>
                <select
                  value={draft.identityStatus ?? "empty"}
                  onChange={(event) =>
                    update({
                      identityStatus: event.target.value as NonNullable<
                        ProfileValues["identityStatus"]
                      >,
                    })
                  }
                >
                  <option value="empty">待完善</option>
                  <option value="candidate">待核实</option>
                  <option value="verified">已确认</option>
                </select>
              </label>
              <label className="settings-switch">
                <input type="checkbox" checked={false} disabled readOnly />
                自动检查公开资料
              </label>
              <p className="creator-profile-note">检查变更会进入待审候选。</p>
              {(draft.accounts?.length ?? 0) > 1 && (
                <details className="creator-records">
                  <summary>其他公开账号</summary>
                  <ProfileValuesView block="accounts" values={draft} />
                </details>
              )}
            </>
          )}
          {candidate && (
            <p className="creator-profile-note">
              核对候选中的资料与来源后保存。
              {candidate.baseRevision !== base.revision &&
                "候选所依据的资料已经变化，请对照当前版本。"}
            </p>
          )}
          {block.block !== "accounts" && (
            <ProfileEvidenceEditor
              block={block.block}
              evidence={evidence}
              change={setEvidence}
            />
          )}
        </fieldset>
        <div className="creator-editor-footer">
          <span>
            {mutation.isPending
              ? "正在保存…"
              : dirty
                ? "有未保存的修改"
                : "已与当前资料一致"}
          </span>
          <div className="creator-editor-buttons">
            <button
              type="button"
              className="pill"
              onClick={closeEditor}
              disabled={mutation.isPending}
            >
              取消
            </button>
            <button
              type="submit"
              className="save-btn"
              disabled={mutation.isPending || !dirty || !validEvidence}
            >
              {mutation.isPending ? "保存中…" : "保存资料"}
            </button>
          </div>
        </div>
        <ErrorNotice error={mutation.error} />
        {mutation.isError && (
          <button
            type="button"
            className="pill"
            disabled={mutation.isPending}
            onClick={() => void loadCurrent()}
          >
            读取当前资料并对照
          </button>
        )}
        <ErrorNotice error={readError} />
        {comparison && (
          <div className="notice">
            <h4>服务器当前修订 {comparison.revision}</h4>
            <ProfileValuesView block={block.block} values={comparison.values} />
            <ProfileEvidenceList evidence={comparison.evidence} />
            {block.block === "accounts" && (
              <p>账号列表作为整组保存，请核对完整列表。</p>
            )}
            <button
              type="button"
              className="pill"
              disabled={mutation.isPending}
              onClick={rebase}
            >
              已核对，保留本次改动并采用当前版本
            </button>
          </div>
        )}
      </form>
      <UnsavedGuard dirty={dirty} />
      {discard && (
        <ConfirmDialog
          title="放弃未保存修改？"
          confirmLabel="放弃修改"
          onCancel={() => setDiscard(false)}
          onConfirm={close}
        >
          <p>这组资料仍有草稿。</p>
        </ConfirmDialog>
      )}
    </section>
  );
}
