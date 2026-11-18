import { MoreLibraries } from "../components/MoreLibraries";
import { useRef, useState } from "react";
import { Link } from "react-router-dom";
import {
  useAIDelegations,
  useCreateAIDelegation,
  useRevokeAIDelegation,
} from "../data/ai-access";
import { useAuthors, useLibraries } from "../data/manage";
import type { Schema } from "../data/client";
import {
  EmptyState,
  ErrorNotice,
  Loading,
  PageHeading,
  StatusBadge,
} from "../components/Feedback";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { UnsavedGuard } from "../lib/navigation";
import { dateTime } from "../lib/format";

type Policy = Schema<"AIDelegationPolicy">;
const assetFieldNames: Record<Schema<"AssetMaintenanceField">, string> = {
  authors: "作者关联", taxonomy: "出处与角色", tags: "标签", rename: "文件名",
};
const fieldNames = {
  name: "作者名称",
  biography: "简短介绍",
  homepage: "主主页",
  aliases: "别名",
};
const blockNames = {
  x: "X 公开资料",
  basic: "整理简介与备注",
  accounts: "账号与发布入口",
};
const initialPolicy: Policy = {
  label: "",
  libraryIds: [],
  authorIds: [],
  allAuthors: false,
  allowedFields: [],
  candidateBlocks: [],
  allowClear: false,
  canIngest: false,
  canResearch: false,
  canResearchAssets: false,
  assetFields: [],
  canOrganize: false,
  canRecycleSources: false,
  maxResearchCalls: 0,
  maxUpdates: 1,
  maxTasksPerDay: 10,
  validDays: 30,
  maxCallsPerTask: 100,
  taskSeconds: 3600,
};
const toggle = <T,>(values: T[], value: T) =>
  values.includes(value)
    ? values.filter((item) => item !== value)
    : [...values, value];

export function AIAccessPage() {
  const [state, setState] = useState<Schema<"AIDelegation">["state"] | "all">(
    "active",
  );
  const query = useAIDelegations(state);
  const items = query.data?.pages.flatMap((page) => page.items) ?? [];
  const revoke = useRevokeAIDelegation();
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<{
    delegation: Schema<"AIDelegation">;
    token: string;
  }>();
  const [removing, setRemoving] = useState<Schema<"AIDelegation">>();
  return (
    <>
      <Link className="back-link" to="/settings">
        返回设置
      </Link>
      <PageHeading
        title="AI 接入"
        subtitle="首次设置操作范围，此后可以在熟悉的聊天工具里完成任务。"
        action={
          <button
            className="button button--primary"
            disabled={creating || !!created}
            onClick={() => setCreating(true)}
          >
            添加 AI 接入
          </button>
        }
      />
      <p className="notice">
        所有接入都能读取全库资料。下面选择的库、作者与动作限制的是写入范围，不能作为私人资料的隔离设置。
      </p>
      {creating && (
        <DelegationForm
          complete={(delegation, token) => {
            setCreated({ delegation, token });
            setCreating(false);
          }}
          cancel={() => setCreating(false)}
        />
      )}
      {created && (
        <OneTimeCredential
          delegation={created.delegation}
          token={created.token}
          close={() => setCreated(undefined)}
        />
      )}
      <label className="compact-field section-space">
        接入状态
        <select
          value={state}
          onChange={(event) => setState(event.target.value as typeof state)}
        >
          <option value="active">当前有效</option>
          <option value="revoked">已撤销</option>
          <option value="expired">已到期</option>
          <option value="all">全部记录</option>
        </select>
      </label>
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
                  disabled={revoke.isPending}
                  onClick={() => {
                    revoke.reset();
                    setRemoving(item);
                  }}
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
      <section className="panel form-stack section-space">
        <h2>连接聊天工具</h2>
        <p>
          执行器提供标准 MCP stdio 接口，启动命令为 <code>qai mcp</code>
          。Codex、ZCode、Pi、Hermes 等宿主使用各自的 MCP
          配置入口；具体宿主插件与安装方式以对应发布说明为准。
        </p>
        <p>
          执行器读取 <code>QIMENG_API_BASE</code>、<code>QIMENG_API_TOKEN</code>
          、<code>QIMENG_AI_STATE_DIR</code> 和明确的{" "}
          <code>QIMENG_AI_PERMISSIONS</code>
          。本地文件根目录和允许图片发送给模型须在执行器单独配置。
        </p>
        <p className="muted">
          修改范围时先建立并配置新接入，再撤销旧接入。撤销会同时停止其派生凭据的后续写入，已保存结果仍保留。
        </p>
      </section>
      {removing && (
        <ConfirmDialog
          title={`撤销“${removing.label}”？`}
          danger
          busy={revoke.isPending}
          onCancel={() => setRemoving(undefined)}
          onConfirm={() =>
            revoke.mutate(removing.id, {
              onSuccess: () => {
                if (created?.delegation.id === removing.id)
                  setCreated(undefined);
                setRemoving(undefined);
              },
            })
          }
        >
          <p>
            对应长期凭据与其派生任务凭据都会失效。已入库文件、已确认资料和候选记录保留。
          </p>
          <ErrorNotice error={revoke.error} />
        </ConfirmDialog>
      )}
    </>
  );
}

function DelegationForm({
  complete,
  cancel,
}: {
  complete: (delegation: Schema<"AIDelegation">, token: string) => void;
  cancel: () => void;
}) {
  const [policy, setPolicy] = useState<Policy>(initialPolicy);
  const [step, setStep] = useState(0);
  const [discard, setDiscard] = useState(false);
  const libraries = useLibraries();
  const [authorQ, setAuthorQ] = useState("");
  const authors = useAuthors(authorQ, undefined, step === 1);
  const [selectedNames, setSelectedNames] = useState<Record<string, string>>(
    {},
  );
  const create = useCreateAIDelegation(complete);
  const change = (patch: Partial<Policy>) =>
    setPolicy((current) => {
      const next = { ...current, ...patch };
      if (!next.canIngest || !next.libraryIds.length) next.canRecycleSources = false;
      if (!next.canResearch || !next.libraryIds.length) next.canResearchAssets = false;
      if (!next.libraryIds.length) next.assetFields = [];
      return next;
    });
  const hasAuthorWrites =
    policy.allowedFields.length > 0 || policy.candidateBlocks.length > 0;
  const rangeReady =
    (!(policy.canIngest || policy.canOrganize) ||
      policy.libraryIds.length > 0) &&
    (!hasAuthorWrites || policy.allAuthors || policy.authorIds.length > 0);
  const dirty = JSON.stringify(policy) !== JSON.stringify(initialPolicy);
  return (
    <form
      className="panel form-stack section-space"
      onSubmit={(event) => {
        event.preventDefault();
        if (!policy.label.trim() || (step > 0 && !rangeReady) || create.isPending) return;
        if (step < 2) setStep(step + 1);
        else create.mutate(policy);
      }}
    >
      <h2>
        {["1 · 选择动作", "2 · 选择范围与预算", "3 · 核对并建立接入"][step]}
      </h2>
      <fieldset
        className="profile-editor-fields form-stack"
        disabled={create.isPending}
      >
        {step === 0 ? (
          <>
            <label>
              接入名称
              <input
                required
                maxLength={80}
                value={policy.label}
                onChange={(event) => change({ label: event.target.value })}
                placeholder="例如我的 Codex 执行器"
              />
            </label>
            <p className="muted">
              默认仅全库读取。按需增加以下操作，未选项不会授予。
            </p>
            <label className="check-label">
              <input
                type="checkbox"
                checked={policy.canIngest}
                onChange={(event) =>
                  change({
                    canIngest: event.target.checked,
                    canOrganize: event.target.checked && policy.canOrganize,
                  })
                }
              />
              接收入库文件
            </label>
            <label className="check-label">
              <input
                type="checkbox"
                disabled={!policy.canIngest}
                checked={policy.canOrganize}
                onChange={(event) =>
                  change({ canOrganize: event.target.checked })
                }
              />
              智能整理所选媒体库
            </label>
            <label className="check-label">
              <input
                type="checkbox"
                checked={policy.canResearch}
                onChange={(event) =>
                  change({
                    canResearch: event.target.checked,
                    maxResearchCalls: event.target.checked ? 8 : 0,
                    candidateBlocks: event.target.checked
                      ? policy.candidateBlocks
                      : [],
                  })
                }
              />
              研究未知身份与来源
            </label>
            <fieldset className="profile-account-row">
              <legend>允许提交的资料候选</legend>
              {(Object.keys(blockNames) as Policy["candidateBlocks"]).map(
                (block) => (
                  <label className="check-label" key={block}>
                    <input
                      type="checkbox"
                      disabled={!policy.canResearch}
                      checked={policy.candidateBlocks.includes(block)}
                      onChange={() =>
                        change({
                          candidateBlocks: toggle(
                            policy.candidateBlocks,
                            block,
                          ),
                        })
                      }
                    />
                    {blockNames[block]}
                  </label>
                ),
              )}
              <p className="muted">
                候选需由你核对并保存，才会进入已确认资料。
              </p>
            </fieldset>
            <details>
              <summary>直接维护基础作者字段</summary>
              {(Object.keys(fieldNames) as Policy["allowedFields"]).map(
                (field) => (
                  <label className="check-label" key={field}>
                    <input
                      type="checkbox"
                      checked={policy.allowedFields.includes(field)}
                      onChange={() =>
                        change({
                          allowedFields: toggle(policy.allowedFields, field),
                        })
                      }
                    />
                    {fieldNames[field]}
                  </label>
                ),
              )}
              <label className="check-label">
                <input
                  type="checkbox"
                  checked={policy.allowClear}
                  onChange={(event) =>
                    change({ allowClear: event.target.checked })
                  }
                />
                允许明确清空已授权基础字段
              </label>
            </details>
          </>
        ) : step === 1 ? (
          <>
              <fieldset className="profile-account-row">
                <legend>允许写入的媒体库</legend>
                <MoreLibraries query={libraries} />
                <ErrorNotice
                  error={libraries.error}
                  retry={() => void libraries.refetch()}
                />
                {libraries.isPending ? (
                  <Loading />
                ) : (
                  libraries.data?.items.map((library) => (
                    <label className="check-label" key={library.id}>
                      <input
                        type="checkbox"
                        checked={policy.libraryIds.includes(library.id)}
                        onChange={() =>
                          change({
                            libraryIds: toggle(policy.libraryIds, library.id),
                          })
                        }
                      />
                      {library.name}
                      {library.enabled ? "" : "（已停用）"}
                    </label>
                  ))
                )}
                <label className="check-label">
                  <input
                    type="checkbox"
                    disabled={!policy.canIngest || !policy.libraryIds.length}
                    checked={policy.canRecycleSources}
                    onChange={event => change({ canRecycleSources: event.target.checked })}
                  />
                  备份核验后允许回收来源
                </label>
                <p className="muted">仅限已选媒体库；服务端核验独立备份与本批内容后移入来源回收区，不永久删除。</p>
                <label className="check-label"><input type="checkbox" disabled={!policy.canResearch || !policy.libraryIds.length} checked={!!policy.canResearchAssets} onChange={event => change({ canResearchAssets: event.target.checked })} />研究已入库作品的媒体预览</label>
                <fieldset className="profile-account-row" disabled={!policy.libraryIds.length}><legend>允许维护所选库中的作品字段</legend>{(Object.keys(assetFieldNames) as Schema<"AssetMaintenanceField">[]).map(field => <label className="check-label" key={field}><input type="checkbox" checked={!!policy.assetFields?.includes(field)} onChange={() => change({ assetFields: toggle(policy.assetFields ?? [], field) })} />{assetFieldNames[field]}</label>)}</fieldset>
              </fieldset>
            {hasAuthorWrites && (
              <fieldset className="profile-account-row form-stack">
                <legend>允许维护的作者</legend>
                <label className="check-label">
                  <input
                    type="checkbox"
                    checked={policy.allAuthors}
                    onChange={(event) =>
                      change({ allAuthors: event.target.checked })
                    }
                  />
                  所有作者（包括今后新增的作者）
                </label>
                {!policy.allAuthors && (
                  <>
                    <label>
                      查找可授权作者
                      <input
                        value={authorQ}
                        onChange={(event) => setAuthorQ(event.target.value)}
                      />
                    </label>
                    <div className="actions">
                      {policy.authorIds.map((id) => (
                        <button
                          type="button"
                          className="button button--small"
                          key={id}
                          onClick={() =>
                            change({
                              authorIds: policy.authorIds.filter(
                                (value) => value !== id,
                              ),
                            })
                          }
                        >
                          {selectedNames[id] ?? "已选作者"} ×
                        </button>
                      ))}
                    </div>
                    <ErrorNotice
                      error={authors.error}
                      retry={() => void authors.refetch()}
                    />
                    {authors.data?.pages
                      .flatMap((page) => page.items)
                      .filter((author) => !author.placeholder)
                      .map((author) => (
                        <label className="check-label" key={author.id}>
                          <input
                            type="checkbox"
                            checked={policy.authorIds.includes(author.id)}
                            disabled={
                              !policy.authorIds.includes(author.id) &&
                              policy.authorIds.length >= 100
                            }
                            onChange={() => {
                              change({
                                authorIds: toggle(policy.authorIds, author.id),
                              });
                              setSelectedNames((current) => ({
                                ...current,
                                [author.id]: author.name,
                              }));
                            }}
                          />
                          {author.name}
                        </label>
                      ))}
                    {authors.hasNextPage && (
                      <button
                        type="button"
                        className="button"
                        disabled={authors.isFetchingNextPage}
                        onClick={() => void authors.fetchNextPage()}
                      >
                        更多作者
                      </button>
                    )}
                  </>
                )}
              </fieldset>
            )}
            <div className="settings-grid">
              <label>
                授权有效天数
                <input
                  type="number"
                  min={1}
                  max={365}
                  required
                  value={policy.validDays}
                  onChange={(event) =>
                    change({ validDays: Number(event.target.value) })
                  }
                />
              </label>
              <label>
                每日最多任务
                <input
                  type="number"
                  min={1}
                  max={200}
                  required
                  value={policy.maxTasksPerDay}
                  onChange={(event) =>
                    change({ maxTasksPerDay: Number(event.target.value) })
                  }
                />
              </label>
              <label>
                每任务调用上限
                <input
                  type="number"
                  min={10}
                  max={10000}
                  required
                  value={policy.maxCallsPerTask}
                  onChange={(event) =>
                    change({ maxCallsPerTask: Number(event.target.value) })
                  }
                />
              </label>
              <label>
                每任务最长秒数
                <input
                  type="number"
                  min={60}
                  max={86400}
                  required
                  value={policy.taskSeconds}
                  onChange={(event) =>
                    change({ taskSeconds: Number(event.target.value) })
                  }
                />
              </label>
              {policy.canResearch && (
                <label>
                  每任务研究调用上限
                  <input
                    type="number"
                    min={0}
                    max={100}
                    required
                    value={policy.maxResearchCalls}
                    onChange={(event) =>
                      change({ maxResearchCalls: Number(event.target.value) })
                    }
                  />
                </label>
              )}
              {hasAuthorWrites && (
                <label>
                  每任务资料提交上限
                  <input
                    type="number"
                    min={1}
                    max={10}
                    required
                    value={policy.maxUpdates}
                    onChange={(event) =>
                      change({ maxUpdates: Number(event.target.value) })
                    }
                  />
                </label>
              )}
            </div>
            <p className="muted">
              这些是影库工具的执行预算，不是模型平台费用额度。换宿主接续任务不会清零已经使用的预算。
            </p>
          </>
        ) : (
          <>
            <PolicySummary policy={policy} />
            <p>
              接入名称：<strong>{policy.label}</strong>
              。创建后仅这次返回凭据；随后交给你控制的执行器。
            </p>
          </>
        )}
        <div className="actions">
          {step > 0 && (
            <button
              type="button"
              className="button"
              onClick={() => setStep(step - 1)}
            >
              上一步
            </button>
          )}
          <button
            className="button button--primary"
            disabled={
              !policy.label.trim() ||
              (step === 1 && !rangeReady) ||
              create.isPending
            }
          >
            {step === 2
              ? create.isPending
                ? "正在建立…"
                : "建立此范围的接入"
              : "下一步"}
          </button>
          <button
            type="button"
            className="button"
            onClick={() => (dirty ? setDiscard(true) : cancel())}
          >
            取消
          </button>
        </div>
      </fieldset>
      <ErrorNotice error={create.error} />
      {create.isError && (
        <p className="notice">
          若提交时网络中断，先刷新下面的授权列表核对是否已建立。未取得凭据的授权可撤销后重新建立；不会自动重复创建。
        </p>
      )}
      <UnsavedGuard dirty={dirty} />
      {discard && (
        <ConfirmDialog
          title="放弃未建立的接入设置？"
          onCancel={() => setDiscard(false)}
          onConfirm={cancel}
        >
          <p>当前范围草稿还没有提交。</p>
        </ConfirmDialog>
      )}
    </form>
  );
}

function PolicySummary({ policy }: { policy: Policy }) {
  return (
    <dl className="facts">
      <div>
        <dt>读取</dt>
        <dd>全库资料</dd>
      </div>
      <div>
        <dt>入库与整理</dt>
        <dd>
          {policy.canIngest
            ? `接收文件，${policy.libraryIds.length} 个库`
            : "未授权接收"}
          {policy.canOrganize ? " · 允许智能整理" : ""}
          {policy.canRecycleSources ? " · 备份核验后可回收来源" : ""}
        </dd>
      </div>
      <div><dt>已有作品</dt><dd>{policy.canResearchAssets ? "可研究所选库的媒体预览" : "未授权研究媒体预览"} · {(policy.assetFields ?? []).map(field => assetFieldNames[field]).join("、") || "未授权修改作品字段"}{(policy.canResearchAssets || !!policy.assetFields?.length) && " · " + policy.libraryIds.length + " 个库"}</dd></div>
      <div>
        <dt>研究</dt>
        <dd>
          {policy.canResearch
            ? `允许，每任务最多 ${policy.maxResearchCalls} 次`
            : "未授权"}
        </dd>
      </div>
      <div>
        <dt>作者范围</dt>
        <dd>
          {policy.allAuthors
            ? "全部作者"
            : `${policy.authorIds.length} 位指定作者`}
        </dd>
      </div>
      <div>
        <dt>资料候选</dt>
        <dd>
          {policy.candidateBlocks
            .map((block) => blockNames[block])
            .join("、") || "未授权"}
        </dd>
      </div>
      <div>
        <dt>直接维护字段</dt>
        <dd>
          {policy.allowedFields.map((field) => fieldNames[field]).join("、") ||
            "未授权"}
          {policy.allowClear ? " · 允许明确清空" : ""}
        </dd>
      </div>
      <div>
        <dt>任务预算</dt>
        <dd>
          每日 {policy.maxTasksPerDay} 个 · 每任务 {policy.maxCallsPerTask}{" "}
          次工具调用 / {policy.taskSeconds} 秒
        </dd>
      </div>
      <div>
        <dt>资料提交</dt>
        <dd>每任务最多 {policy.maxUpdates} 次</dd>
      </div>
    </dl>
  );
}

function OneTimeCredential({
  delegation,
  token,
  close,
}: {
  delegation: Schema<"AIDelegation">;
  token: string;
  close: () => void;
}) {
  const field = useRef<HTMLInputElement>(null);
  const [revealed, setRevealed] = useState(false);
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState("");
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(token);
      setCopied(true);
      setCopyError("");
    } catch {
      setRevealed(true);
      field.current?.focus();
      field.current?.select();
      setCopyError("当前浏览器不能自动复制。凭据已选中，请手动复制。");
    }
  };
  return (
    <section className="panel form-stack section-space" aria-label="新接入凭据">
      <h2>“{delegation.label}”已建立</h2>
      <p>
        凭据只在本次创建响应中提供。请放入可信执行器的{" "}
        <code>QIMENG_API_TOKEN</code> 配置，不粘贴到模型聊天内容。
      </p>
      <label>
        一次性显示的接入凭据
        <input
          ref={field}
          readOnly
          type={revealed ? "text" : "password"}
          value={token}
          autoComplete="off"
          spellCheck={false}
        />
      </label>
      <div className="actions">
        <button className="button button--primary" onClick={() => void copy()}>
          {copied ? "已复制" : "复制凭据"}
        </button>
        <button className="button" onClick={() => setRevealed(!revealed)}>
          {revealed ? "隐藏" : "显示凭据"}
        </button>
      </div>
      {copyError && <p role="status">{copyError}</p>}
      <p>
        服务器地址：<code>{window.location.origin}</code>
      </p>
      <p className="muted">
        页面不将凭据写入浏览器持久存储。刷新或离开后无法再次从列表读取；若丢失，撤销此接入并重新建立。
      </p>
      <button className="button" onClick={close}>
        已保存到执行器，清除本页凭据
      </button>
      <UnsavedGuard dirty />
    </section>
  );
}
