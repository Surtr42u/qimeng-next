import { useEffect, useRef, useState, type ReactNode } from "react";
import { Check, ChevronRight, Copy, KeyRound, Plus, RefreshCw, Terminal } from "lucide-react";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ErrorNotice, Loading } from "../components/Feedback";
import { ManagePageHeader } from "../components/LegacyManageHeader";
import { MoreLibraries } from "../components/MoreLibraries";
import {
  useAIDelegations,
  useCreateAIDelegation,
  useResearchProviderContext,
  useRevokeAIDelegation,
} from "../data/ai-access";
import type { Schema } from "../data/client";
import { useAuthors, useLibraries } from "../data/manage";
import { dateTime } from "../lib/format";
import { UnsavedGuard } from "../lib/navigation";
import "../styles/ai-access.css";

type Delegation = Schema<"AIDelegation">;
type Policy = Schema<"AIDelegationPolicy">;
const assetFieldNames: Record<Schema<"AssetMaintenanceField">, string> = {
  authors: "作者关联", taxonomy: "出处与角色", tags: "标签", rename: "文件名",
};
const providerNames: Record<Schema<"ResearchProviderName">, string> = {
  fxtwitter: "FxTwitter（X 公开账号与帖子）", pixiv: "Pixiv", yandere: "yande.re", e621: "e621",
  wayback: "Wayback Machine", steam: "Steam 创意工坊", bing: "Bing 搜索",
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
  label: "我的 AI 助手",
  libraryIds: [],
  authorIds: [],
  allAuthors: false,
  allowedFields: [],
  candidateBlocks: [],
  allowClear: false,
  canIngest: false,
  canResearch: false,
  canLookupImages: false,
  researchProviders: [],
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

const canGrantImageLookup = (policy: Policy) => policy.canResearch && policy.maxResearchCalls > 0 &&
  policy.libraryIds.length > 0 && (policy.canIngest || !!policy.canResearchAssets);

const stateNames: Record<Delegation["state"], string> = {
  active: "授权有效", revoked: "已撤销", expired: "已到期",
};

export function AIAccessPage() {
  const [state, setState] = useState<Delegation["state"] | "all">("active");
  const query = useAIDelegations(state);
  const items = query.data?.pages.flatMap(page => page.items) ?? [];
  const revoke = useRevokeAIDelegation();
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<{ delegation: Delegation; token: string }>();
  const [removing, setRemoving] = useState<Delegation>();
  const addButton = useRef<HTMLButtonElement>(null);
  const revokeButton = useRef<HTMLButtonElement>(null);
  const filter = useRef<HTMLSelectElement>(null);

  function finish() {
    setCreating(false);
    setCreated(undefined);
    requestAnimationFrame(() => addButton.current?.focus());
  }

  return (
    <div className="page manage-workspace ai-access">
      <ManagePageHeader
        title="AI 接入"
        description="所有接入都能读取全库资料。所选媒体库、作者和字段只限定写入范围。"
        backTo="/settings"
        backLabel="返回设置"
        actions={
          <button ref={addButton} type="button" className="save-btn ai-action"
            disabled={creating || !!created} onClick={() => setCreating(true)}>
            <Plus aria-hidden="true" size={16} />添加 AI 接入
          </button>
        }
      />

      <div className="settings-content ai-columns">
        <div className="settings-content ai-main">
          {creating && (
            <DelegationForm
              complete={(delegation, token) => {
                setCreated({ delegation, token });
                setCreating(false);
              }}
              cancel={finish}
            />
          )}
          {created && <OneTimeCredential {...created} close={finish} />}

          <section className="settings-card ai-card" aria-labelledby="ai-records-title">
            <div className="ai-card-head">
              <h3 id="ai-records-title">接入记录</h3>
              <div className="ai-list-tools">
                <label className="settings-field ai-filter">
                  <span>接入状态</span>
                  <select ref={filter} value={state}
                    onChange={event => setState(event.target.value as typeof state)}>
                    <option value="active">当前有效</option>
                    <option value="revoked">已撤销</option>
                    <option value="expired">已到期</option>
                    <option value="all">全部记录</option>
                  </select>
                </label>
                <button type="button" className="pill ai-refresh" title="刷新接入记录" aria-label="刷新接入记录"
                  disabled={query.isFetching} onClick={() => void query.refetch()}>
                  <RefreshCw aria-hidden="true" size={15} />
                </button>
              </div>
            </div>
            <ErrorNotice error={query.error} stale={!!query.data} retry={() => void query.refetch()} />
            {query.isPending ? <Loading text="正在读取接入记录…" /> : items.length > 0 ? (
              <div className="ai-records">
                {items.map(item => (
                  <GrantRow key={item.id} item={item} busy={revoke.isPending} onRevoke={button => {
                    revoke.reset();
                    revokeButton.current = button;
                    setRemoving(item);
                  }} />
                ))}
              </div>
            ) : !query.isError && (
              <div className="manage-empty ai-empty">
                <KeyRound aria-hidden="true" />
                <h4>{state === "active" ? "尚无有效 AI 接入" : state === "all" ? "尚无 AI 接入" : "没有此状态的接入记录"}</h4>
                {state === "active" || state === "all"
                  ? <p>通过「添加 AI 接入」为自己的执行器授权，创建后保存一次性凭据。</p>
                  : <button type="button" className="pill" onClick={() => setState("all")}>查看全部记录</button>}
              </div>
            )}
            {query.hasNextPage && (
              <button type="button" className="pill ai-more" disabled={query.isFetchingNextPage}
                onClick={() => void query.fetchNextPage({ cancelRefetch: false })}>
                {query.isFetchingNextPage ? "正在读取…" : "加载更多接入记录"}
              </button>
            )}
          </section>
        </div>
        <ExecutorGuide />
      </div>

      {removing && (
        <ConfirmDialog
          title={"撤销“" + removing.label + "”？"}
          danger
          busy={revoke.isPending}
          onCancel={() => {
            setRemoving(undefined);
            requestAnimationFrame(() => revokeButton.current?.focus());
          }}
          onConfirm={() => revoke.mutate(removing.id, {
            onSuccess: () => {
              if (created?.delegation.id === removing.id) setCreated(undefined);
              setRemoving(undefined);
              requestAnimationFrame(() => filter.current?.focus());
            },
          })}
        >
          <p>此接入的长期凭据和派生任务凭据都会失效。已入库文件、已确认资料和候选记录保留。</p>
          <ErrorNotice error={revoke.error} />
        </ConfirmDialog>
      )}
    </div>
  );
}

function GrantRow({ item, busy, onRevoke }: {
  item: Delegation; busy: boolean; onRevoke: (button: HTMLButtonElement) => void;
}) {
  const operations = [
    item.canIngest && "接收入库",
    item.canOrganize && "智能整理",
    item.canRecycleSources && "来源回收",
    (item.canResearch || item.canResearchAssets || item.canLookupImages) && "研究",
    item.candidateBlocks.length > 0 && "资料候选",
    item.allowedFields.length > 0 && "作者维护",
    !!item.assetFields?.length && "作品维护",
  ].filter(Boolean);

  return (
    <article className="ai-record">
      <div className="ai-record-heading">
        <h4>{item.label}</h4>
        <span className={"manage-state ai-state ai-state--" + item.state}>{stateNames[item.state]}</span>
      </div>
      <p className="ai-record-scope">{operations.join(" · ") || "仅全库读取"}</p>
      <p className="ai-dates">
        <span>建立于 <time dateTime={item.createdAt}>{dateTime(item.createdAt)}</time></span>
        <span>到期 <time dateTime={item.expiresAt}>{dateTime(item.expiresAt)}</time></span>
      </p>
      <details className="ai-record-details">
        <summary className="ai-disclosure"><ChevronRight aria-hidden="true" />权限详情</summary>
        <PolicySummary policy={item} />
      </details>
      {item.state === "active" && (
        <button type="button" className="pill manage-danger-action ai-revoke" disabled={busy}
          onClick={event => onRevoke(event.currentTarget)}>
          撤销此接入
        </button>
      )}
    </article>
  );
}

function ExecutorGuide() {
  return (
    <aside className="settings-card ai-card ai-guide" aria-labelledby="ai-guide-title">
      <h3 id="ai-guide-title"><Terminal aria-hidden="true" size={17} />连接聊天工具</h3>
      <p>在 Codex、ZCode 等宿主的 MCP 配置中添加本地执行器，传输方式选 stdio。</p>
      <div className="ai-command"><span>启动命令</span><code>qai mcp</code></div>
      <details>
        <summary className="ai-disclosure"><ChevronRight aria-hidden="true" />执行器配置</summary>
        <dl className="ai-guide-fields">
          <div><dt><code>QIMENG_API_BASE</code></dt><dd>影库的服务器地址</dd></div>
          <div><dt><code>QIMENG_API_TOKEN</code></dt><dd>建立接入时仅显示一次的凭据</dd></div>
          <div><dt><code>QIMENG_AI_STATE_DIR</code></dt><dd>执行器保存任务状态的目录</dd></div>
          <div><dt><code>QIMENG_AI_PERMISSIONS</code></dt><dd>允许执行器使用的权限</dd></div>
        </dl>
        <p>本地文件根目录、向模型发送图片的权限也在执行器中单独配置。更换授权范围时，先配置新接入，再撤销旧接入。</p>
      </details>
    </aside>
  );
}

type PolicyEditor = { policy: Policy; change: (patch: Partial<Policy>) => void };

function DelegationForm({ complete, cancel }: {
  complete: (delegation: Delegation, token: string) => void;
  cancel: () => void;
}) {
  const [policy, setPolicy] = useState<Policy>(initialPolicy);
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [discard, setDiscard] = useState(false);
  const [authorNames, setAuthorNames] = useState<Record<string, string>>({});
  const nameField = useRef<HTMLInputElement>(null);
  const libraries = useLibraries();
  const providers = useResearchProviderContext(policy.canResearch);
  const choices = providers.data?.enabled && !providers.isError
    ? providers.data.providers.filter(provider => Object.hasOwn(providerNames, provider.name))
    : [];
  const create = useCreateAIDelegation(complete);

  useEffect(() => { nameField.current?.focus(); }, []);
  function change(patch: Partial<Policy>) {
    setPolicy(current => {
      const next = { ...current, ...patch };
      if (!next.canIngest || !next.libraryIds.length) next.canRecycleSources = false;
      if (!next.canResearch || !next.libraryIds.length) next.canResearchAssets = false;
      if (!canGrantImageLookup(next)) next.canLookupImages = false;
      if (!next.canResearch || !(next.maxResearchCalls > 0)) next.researchProviders = [];
      if (!next.libraryIds.length) next.assetFields = [];
      if (!next.allowedFields.length) next.allowClear = false;
      if (!next.allowedFields.length && !next.candidateBlocks.length) {
        next.allAuthors = false;
        next.authorIds = [];
      }
      return next;
    });
  }

  const hasAuthorWrites = policy.allowedFields.length > 0 || policy.candidateBlocks.length > 0;
  const showLibraries = detailsOpen || policy.canIngest || policy.canResearch || !!policy.assetFields?.length || !!policy.libraryIds.length;
  const needsLibrary = (policy.canIngest || policy.canOrganize || policy.canResearchAssets || !!policy.assetFields?.length) && !policy.libraryIds.length;
  const needsAuthor = hasAuthorWrites && !policy.allAuthors && !policy.authorIds.length;
  const missingProvider = policy.researchProviders?.some(name => !choices.some(choice => choice.name === name));
  const validName = !!policy.label.trim() && new TextEncoder().encode(policy.label.trim()).length <= 80;
  const readOnly = !policy.canIngest && !policy.canResearch && !hasAuthorWrites && !policy.assetFields?.length;
  const validBudget = (value: number | undefined, min: number, max: number) =>
    value !== undefined && Number.isInteger(value) && value >= min && value <= max;
  const validBudgets = validBudget(policy.validDays, 1, 365) && validBudget(policy.maxTasksPerDay, 1, 200) &&
    validBudget(policy.maxCallsPerTask, 10, 10000) && validBudget(policy.taskSeconds, 60, 86400) &&
    validBudget(policy.maxUpdates, 1, 10) && validBudget(policy.maxResearchCalls, 0, 100);
  const blocked = !validBudgets || !validName || needsLibrary || needsAuthor || !!missingProvider || create.isPending;
  const dirty = JSON.stringify(policy) !== JSON.stringify(initialPolicy);

  return (
    <form noValidate className="settings-card ai-card ai-wizard" aria-label="添加 AI 接入" onSubmit={event => {
      event.preventDefault();
      if (blocked) return;
      create.mutate({
        ...policy, label: policy.label.trim(),
        libraryIds: (policy.canIngest || policy.canResearch || policy.assetFields?.length) ? policy.libraryIds : [],
        authorIds: policy.allAuthors ? [] : policy.authorIds,
      });
    }}>
      <fieldset className="ai-form-fields" disabled={create.isPending} aria-label="接入设置">
        <label className="settings-field ai-name">
          <span>接入名称</span>
          <input ref={nameField} required aria-invalid={!validName} value={policy.label}
            onChange={event => change({ label: event.target.value })} placeholder="例如我的 Codex 执行器" />
        </label>
        {!validName && <p className="ai-help" role="alert">请输入接入名称（不超过 80 字节）。</p>}
        <fieldset className="ai-group">
          <legend>执行动作</legend>
          <p className="ai-help">默认仅全库读取。按需勾选要交给执行器的操作。</p>
          <div className="ai-choices">
            <Choice checked={policy.canIngest} onChange={checked => change({
              canIngest: checked, canOrganize: checked && policy.canOrganize,
            })}>接收入库文件</Choice>
            <Choice checked={policy.canOrganize} disabled={!policy.canIngest}
              onChange={checked => change({ canOrganize: checked })}>智能整理所选媒体库</Choice>
            <Choice checked={policy.canResearch} onChange={checked => change({
              canResearch: checked, maxResearchCalls: checked ? 8 : 0,
              candidateBlocks: checked ? policy.candidateBlocks : [],
            })}>研究未知身份与来源</Choice>
          </div>
        </fieldset>
        {policy.canResearch && <fieldset className="ai-group">
          <legend>允许提交的资料候选</legend>
          <p className="ai-help">候选由你核对并保存后才会进入已确认资料。</p>
          <div className="ai-choices">
            {(Object.keys(blockNames) as Policy["candidateBlocks"]).map(block => (
              <Choice key={block} checked={policy.candidateBlocks.includes(block)}
                onChange={() => change({ candidateBlocks: toggle(policy.candidateBlocks, block) })}>{blockNames[block]}</Choice>
            ))}
          </div>
        </fieldset>}
        <details className="ai-direct-fields" onToggle={event => setDetailsOpen(event.currentTarget.open)}>
          <summary className="ai-disclosure"><ChevronRight aria-hidden="true" />详细权限</summary>
          <h4>直接维护基础作者字段</h4>
          <p className="ai-help">所选字段可直接写入已确认资料。</p>
          <div className="ai-choices">
            {(Object.keys(fieldNames) as Policy["allowedFields"]).map(field => (
              <Choice key={field} checked={policy.allowedFields.includes(field)}
                onChange={() => change({ allowedFields: toggle(policy.allowedFields, field) })}>{fieldNames[field]}</Choice>
            ))}
          </div>
          <Choice checked={policy.allowClear} disabled={!policy.allowedFields.length} onChange={checked => change({ allowClear: checked })}>
            允许明确清空已授权基础字段
          </Choice>
        </details>

        {showLibraries && <fieldset className="ai-group">
          <legend>允许写入的媒体库</legend>
          <p className="ai-help">{policy.canIngest || policy.canOrganize
            ? "接收或整理文件至少需要一个目标库。"
            : "只需读取可跳过。选择库后可授权维护其中的已有作品。"}</p>
          <FormReadError error={libraries.error} retry={() => void libraries.refetch()} />
          {libraries.isPending ? <Loading text="正在读取媒体库…" /> : (
            <div className="ai-choices">
              {libraries.data?.items.map(library => (
                <Choice key={library.id} checked={policy.libraryIds.includes(library.id)}
                  disabled={!policy.libraryIds.includes(library.id) && policy.libraryIds.length >= 100}
                  onChange={() => change({ libraryIds: toggle(policy.libraryIds, library.id) })}>
                  {library.name + (library.enabled ? "" : "（已停用）")}
                </Choice>
              ))}
            </div>
          )}
          {!libraries.isError && libraries.data?.items.length === 0 && <p className="ai-help">尚无可选媒体库。</p>}
          <MoreLibraries query={libraries} />
          {policy.libraryIds.length > 0 && <>
            {policy.canIngest && <div className="ai-subgroup">
              <Choice checked={policy.canRecycleSources ?? false} onChange={checked => change({ canRecycleSources: checked })}>
                备份核验后允许回收来源
              </Choice>
              <p className="ai-help">核验独立备份与本批内容后，所选库的来源文件移入回收区。</p>
            </div>}
            {policy.canResearch && <Choice checked={!!policy.canResearchAssets}
              onChange={checked => change({ canResearchAssets: checked })}>研究已入库作品的媒体预览</Choice>}
            <fieldset className="ai-subgroup">
              <legend>允许维护所选库中的作品字段</legend>
              <div className="ai-choices">
                {(Object.keys(assetFieldNames) as Schema<"AssetMaintenanceField">[]).map(field => (
                  <Choice key={field} checked={!!policy.assetFields?.includes(field)}
                    onChange={() => change({ assetFields: toggle(policy.assetFields ?? [], field) })}>{assetFieldNames[field]}</Choice>
                ))}
              </div>
            </fieldset>
          </>}
          {policy.canResearch && <div className="ai-subgroup">
            <Choice checked={!!policy.canLookupImages} disabled={!canGrantImageLookup(policy)}
              onChange={checked => change({ canLookupImages: checked })}>允许单帧识图</Choice>
            <p className="ai-help">允许向已配置的识图服务发送一帧派生预览。需选择媒体库，授权接收文件或已有作品预览，并设置正数研究调用上限。</p>
          </div>}
        </fieldset>}

        {hasAuthorWrites && (
          <AuthorScope policy={policy} change={change} names={authorNames}
            remember={(id, name) => setAuthorNames(current => ({ ...current, [id]: name }))} />
        )}

        {policy.canResearch && (
          <fieldset className="ai-group">
            <legend>允许发送文字查询的公开来源</legend>
            <NumberField label="每任务研究调用上限" value={policy.maxResearchCalls} min={0} max={100} onChange={value => change({ maxResearchCalls: value })} />
            <p className="ai-help">执行器可向勾选的来源发送公开账号、作品编号、网址或检索词。</p>
            <FormReadError error={providers.error} retry={() => void providers.refetch()} />
            {providers.isPending ? <Loading text="正在读取研究来源…" /> : !providers.isError && (
              choices.length > 0 ? <div className="ai-choices">
                {choices.map(({ name }) => (
                  <Choice key={name} disabled={policy.maxResearchCalls <= 0} checked={!!policy.researchProviders?.includes(name)}
                    onChange={() => change({ researchProviders: toggle(policy.researchProviders ?? [], name) })}>{providerNames[name]}</Choice>
                ))}
              </div> : <p className="ai-help">当前服务器未开放公开来源查询。</p>
            )}
            {choices.length > 0 && policy.maxResearchCalls <= 0 && <p className="ai-help">调高研究调用上限即可选择来源。</p>}
            {missingProvider && providers.isSuccess && (
              <button type="button" className="pill" onClick={() => change({
                researchProviders: policy.researchProviders?.filter(name => choices.some(choice => choice.name === name)),
              })}>移除当前不可用的来源</button>
            )}
          </fieldset>
        )}

        <details className="ai-direct-fields">
          <summary className="ai-disclosure"><ChevronRight aria-hidden="true" />有效期与任务预算</summary>
          <div className="settings-grid ai-budget">
            <NumberField label="授权有效天数" value={policy.validDays} min={1} max={365} onChange={value => change({ validDays: value })} />
            <NumberField label="每日最多任务" value={policy.maxTasksPerDay} min={1} max={200} onChange={value => change({ maxTasksPerDay: value })} />
            <NumberField label="每任务调用上限" value={policy.maxCallsPerTask} min={10} max={10000} onChange={value => change({ maxCallsPerTask: value })} />
            <NumberField label="每任务最长秒数" value={policy.taskSeconds} min={60} max={86400} onChange={value => change({ taskSeconds: value })} />
            {hasAuthorWrites && <NumberField label="每任务资料提交上限" value={policy.maxUpdates} min={1} max={10} onChange={value => change({ maxUpdates: value })} />}
          </div>
          <p className="ai-help">预算按影库工具调用计算，接续任务时保留已用额度。模型平台费用由宿主计费。</p>
        </details>
        <section className="ai-review" aria-labelledby="ai-current-policy">
          <h4 id="ai-current-policy">本次授权</h4>
          {readOnly
            ? <p>仅全库读取，不允许写入或向外部研究服务发送资料。有效期 {policy.validDays} 天。</p>
            : <PolicySummary policy={policy} authorNames={authorNames}
                libraryNames={Object.fromEntries((libraries.data?.items ?? []).map(library => [library.id, library.name]))} />}
        </section>
        {!validBudgets && <p className="ai-help" role="alert">请核对有效期与任务预算。</p>}
        <div className="ai-form-actions">
          <button type="submit" className="save-btn" disabled={blocked}>
            {create.isPending ? "正在建立…" : "创建接入凭据"}
          </button>
          <button type="button" className="pill ai-cancel" onClick={() => dirty ? setDiscard(true) : cancel()}>取消</button>
        </div>
      </fieldset>

      <ErrorNotice error={create.error} />
      {create.isError && (
        <p className="ai-help">若提交时连接中断，请在下方接入记录选择「全部记录」并刷新。已建立却未取得凭据的接入可撤销后重建。</p>
      )}
      <UnsavedGuard dirty={dirty} />
      {discard && (
        <ConfirmDialog title="放弃未建立的接入设置？" onCancel={() => setDiscard(false)} onConfirm={cancel}>
          <p>当前范围草稿还没有提交。</p>
        </ConfirmDialog>
      )}
    </form>
  );
}

function Choice({ children, checked, disabled, onChange }: {
  children: ReactNode; checked: boolean; disabled?: boolean; onChange: (checked: boolean) => void;
}) {
  return (
    <label className="ai-choice">
      <input type="checkbox" checked={checked} disabled={disabled} onChange={event => onChange(event.target.checked)} />
      <span>{children}</span>
    </label>
  );
}

function NumberField({ label, value, min, max, onChange }: {
  label: string; value: number | undefined; min: number; max: number; onChange: (value: number) => void;
}) {
  return (
    <label className="settings-field">
      <span>{label}</span>
      <input type="number" min={min} max={max} required value={Number.isFinite(value) ? value : ""} onChange={event => onChange(event.target.valueAsNumber)} />
    </label>
  );
}

// A retry inside the wizard must not submit or advance the form.
function FormReadError({ error, retry }: { error: unknown; retry: () => void }) {
  if (!error) return null;
  return (
    <div className="ai-retry">
      <ErrorNotice error={error} />
      <button type="button" className="pill" onClick={retry}>重新读取</button>
    </div>
  );
}

function AuthorScope({ policy, change, names, remember }: PolicyEditor & {
  names: Record<string, string>; remember: (id: string, name: string) => void;
}) {
  const [search, setSearch] = useState("");
  const authors = useAuthors(search, undefined, !policy.allAuthors);
  const choices = authors.data?.pages.flatMap(page => page.items).filter(author => !author.placeholder) ?? [];
  return (
    <fieldset className="ai-group">
      <legend>允许维护的作者</legend>
      <p className="ai-help">请选至少一位作者，或授权所有作者。候选和直接维护字段共用此范围。</p>
      <Choice checked={policy.allAuthors} onChange={checked => change({ allAuthors: checked })}>
        所有作者（包括今后新增的作者）
      </Choice>
      {!policy.allAuthors && <>
        <label className="settings-field">
          <span>查找可授权作者</span>
          <input value={search} onChange={event => setSearch(event.target.value)} />
        </label>
        {policy.authorIds.length > 0 && (
          <div className="settings-options" aria-label="已选作者">
            {policy.authorIds.map(id => (
              <button type="button" className="pill" key={id} aria-label={"移除" + (names[id] ?? id)}
                onClick={() => change({ authorIds: policy.authorIds.filter(value => value !== id) })}>
                {names[id] ?? id} ×
              </button>
            ))}
          </div>
        )}
        <FormReadError error={authors.error} retry={() => void authors.refetch()} />
        {authors.isPending ? <Loading text="正在查找作者…" /> : (
          <div className="ai-choices">
            {choices.map(author => (
              <Choice key={author.id} checked={policy.authorIds.includes(author.id)}
                disabled={!policy.authorIds.includes(author.id) && policy.authorIds.length >= 100}
                onChange={() => {
                  change({ authorIds: toggle(policy.authorIds, author.id) });
                  remember(author.id, author.name);
                }}>{author.name}</Choice>
            ))}
          </div>
        )}
        {!authors.isPending && !authors.isError && !choices.length && <p className="ai-help">没有匹配的可授权作者。</p>}
        {authors.hasNextPage && (
          <button type="button" className="pill" disabled={authors.isFetchingNextPage}
            onClick={() => void authors.fetchNextPage({ cancelRefetch: false })}>更多作者</button>
        )}
      </>}
    </fieldset>
  );
}

function PolicySummary({ policy, libraryNames, authorNames }: {
  policy: Policy; libraryNames?: Record<string, string>; authorNames?: Record<string, string>;
}) {
  const scopedNames = (ids: string[], names?: Record<string, string>) =>
    ids.map(id => names?.[id] ?? id).join("、") || "未选择";
  const intake = [
    policy.canIngest && "接收文件",
    policy.canOrganize && "智能整理",
    policy.canRecycleSources && "备份核验后可回收来源",
  ].filter(Boolean);
  const fields = (policy.assetFields ?? []).map(field => assetFieldNames[field]);

  return (
    <dl className="ai-facts">
      <div><dt>读取</dt><dd>全库资料</dd></div>
      <div><dt>入库与整理</dt><dd>{intake.join("、") || "未授权"}</dd></div>
      <div><dt>授权媒体库</dt><dd>{scopedNames(policy.libraryIds, libraryNames)}</dd></div>
      <div><dt>已有作品</dt><dd>{policy.canResearchAssets ? "可研究所选库的媒体预览" : "未授权研究媒体预览"} · {fields.join("、") || "未授权修改作品字段"}</dd></div>
      <div><dt>研究</dt><dd>{policy.canResearch ? "允许，每任务最多 " + policy.maxResearchCalls + " 次" : "未授权"}</dd></div>
      {(policy.canResearch || !!policy.researchProviders?.length) && (
        <div><dt>允许发送文字查询的公开来源</dt><dd>{policy.researchProviders?.map(name => providerNames[name]).join("、") || "未授权"}</dd></div>
      )}
      {(policy.canResearch || policy.canLookupImages) && <div><dt>单帧识图</dt><dd>
        {policy.canLookupImages ? "允许将派生的单帧预览发送至已配置的识图服务" : "未授权"}
      </dd></div>}
      <div><dt>作者范围</dt><dd>{policy.allAuthors ? "全部作者（含今后新增）" : scopedNames(policy.authorIds, authorNames)}</dd></div>
      <div><dt>资料候选</dt><dd>{policy.candidateBlocks.map(block => blockNames[block]).join("、") || "未授权"}</dd></div>
      <div><dt>直接维护字段</dt><dd>{policy.allowedFields.map(field => fieldNames[field]).join("、") || "未授权"}{policy.allowClear && " · 允许明确清空已授权字段"}</dd></div>
      <div><dt>授权有效期</dt><dd>{policy.validDays} 天</dd></div>
      <div><dt>任务预算</dt><dd>每日 {policy.maxTasksPerDay} 个 · 每任务 {policy.maxCallsPerTask ?? initialPolicy.maxCallsPerTask} 次调用 / {policy.taskSeconds ?? initialPolicy.taskSeconds} 秒</dd></div>
      <div><dt>资料提交上限</dt><dd>每任务最多 {policy.maxUpdates} 次</dd></div>
    </dl>
  );
}

function OneTimeCredential({ delegation, token, close }: {
  delegation: Delegation; token: string; close: () => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  const field = useRef<HTMLInputElement>(null);
  const [revealed, setRevealed] = useState(false);
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState(false);
  useEffect(() => { heading.current?.focus(); }, []);

  async function copy() {
    try {
      await navigator.clipboard.writeText(token);
      setCopied(true);
      setCopyError(false);
    } catch {
      setCopied(false);
      setRevealed(true);
      setCopyError(true);
      requestAnimationFrame(() => {
        field.current?.focus();
        field.current?.select();
      });
    }
  }

  return (
    <section className="settings-card ai-card ai-credential" aria-label="新接入凭据">
      <h3 ref={heading} tabIndex={-1}>“{delegation.label}”已建立</h3>
      <p className="ai-help">凭据只在这里显示一次，请存入可信执行器的 <code>QIMENG_API_TOKEN</code>。请勿粘贴到聊天内容。</p>
      <label className="settings-field">
        <span>一次性显示的接入凭据</span>
        <input ref={field} readOnly type={revealed ? "text" : "password"} value={token} autoComplete="off" spellCheck={false} />
      </label>
      <div className="ai-credential-actions">
        <button type="button" className="save-btn ai-action" onClick={() => void copy()}>
          {copied ? <Check aria-hidden="true" size={15} /> : <Copy aria-hidden="true" size={15} />}
          {copied ? "已复制" : "复制凭据"}
        </button>
        <button type="button" className="pill" aria-pressed={revealed} onClick={() => {
          setCopyError(false);
          setRevealed(!revealed);
        }}>
          {revealed ? "隐藏" : "显示凭据"}
        </button>
      </div>
      {copyError && <p className="ai-help" role="status">当前浏览器不能自动复制。凭据已选中，请手动复制。</p>}
      <dl className="ai-server"><div><dt>服务器地址</dt><dd><code>{window.location.origin}</code></dd></div></dl>
      <p className="ai-help">刷新或离开后将无法再次读取此凭据。若遗失，可撤销接入后重新建立。</p>
      <button type="button" className="pill" onClick={close}>已保存到执行器，清除本页凭据</button>
      <UnsavedGuard dirty />
    </section>
  );
}
