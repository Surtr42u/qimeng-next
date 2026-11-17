import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useBatch, useBatches, useCancelBatch } from "../data/manage";
import {
  EmptyState,
  ErrorNotice,
  Loading,
  PageHeading,
  StatusBadge,
} from "../components/Feedback";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { dateTime, bytes } from "../lib/format";
import { Icon } from "../components/Icon";
import { TaskNavigation } from "./AuthorTaskPages";
import { BatchInputReport } from './BatchInputReport';

const batchLabels: Record<string, string> = {
  receiving: "正在接收",
  waiting_executor: "等待执行器",
  ready: "等待处理",
  partial: "部分完成 / 需要处理",
  completed: "已完成",
  cancelled: "已取消",
};
const uploadLabels: Record<string, string> = {
  receiving: "正在传输",
  staged: "已接收，待整理",
  ingested: "已入库",
  duplicate: "重复，未新增",
  cancelled: "已取消",
  failed: "失败",
};
export function TasksPage() {
  const query = useBatches();
  const [filter, setFilter] = useState("");
  const items = query.data?.pages.flatMap((page) => page.items) ?? [];
  const visible = filter
    ? items.filter((item) => item.state === filter)
    : items;
  return (
    <>
      <PageHeading
        title="任务与报告"
        subtitle="每项都有去向，成功与未完成的部分分别说明。"
        action={
          <button
            className="button"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            刷新
          </button>
        }
      />
      <TaskNavigation />
      <label className="compact-field section-space">
        筛选已加载任务
        <select
          value={filter}
          onChange={(event) => setFilter(event.target.value)}
        >
          <option value="">全部状态</option>
          {Object.entries(batchLabels).map(([state, label]) => (
            <option key={state} value={state}>
              {label}
            </option>
          ))}
        </select>
      </label>
      <ErrorNotice
        error={query.error}
        stale={items.length > 0}
        retry={() => void query.refetch()}
      />
      {query.isPending ? (
        <Loading />
      ) : visible.length ? (
        <div className="list-panel">
          {visible.map((batch) => (
            <Link
              className="list-row"
              key={batch.id}
              to={`/manage/tasks/${encodeURIComponent(batch.id)}`}
            >
              <span className="row-icon">
                <Icon name="task" />
              </span>
              <div className="row-main">
                <strong>{batch.label}</strong>
                <span>
                  {batch.mode === "ordinary" ? "普通上传" : "智能整理"} ·{" "}
                  {dateTime(batch.createdAt)}
                </span>
              </div>
              <StatusBadge
                tone={
                  batch.state === "completed"
                    ? "success"
                    : batch.state === "partial" ||
                        batch.state === "waiting_executor"
                      ? "warning"
                      : "neutral"
                }
              >
                {batchLabels[batch.state] ?? batch.state}
              </StatusBadge>
            </Link>
          ))}
        </div>
      ) : (
        !query.isError && (
          <EmptyState
            title={filter ? "已加载任务中没有此状态" : "还没有任务"}
          />
        )
      )}
      {query.hasNextPage && (
        <div className="load-more">
          <button
            className="button"
            disabled={query.isFetchingNextPage}
            onClick={() => void query.fetchNextPage()}
          >
            加载更多任务
          </button>
        </div>
      )}
    </>
  );
}
export function TaskPage() {
  const { id = "" } = useParams();
  const query = useBatch(id);
  const cancellation = useCancelBatch(id);
  const [confirm, setConfirm] = useState(false);
  if (!query.data)
    return (
      <>
        <PageHeading title="任务报告" />
        {query.isPending ? (
          <Loading />
        ) : (
          <ErrorNotice error={query.error} retry={() => void query.refetch()} />
        )}
      </>
    );
  const { batch, inputs, items, plans } = query.data;
  const ingested = batch.manifestState === 'sealed'
    ? inputs.filter(input => input.state === 'ingested').length
    : items.filter(item => item.upload.state === 'ingested').length;
  const exceptions = new Set([
    ...inputs.filter(input => ['failed', 'duplicate'].includes(input.state)).map(input => input.id),
    ...items.filter(item => item.researchOutcome === 'conflict' || batch.manifestState !== 'sealed' && ['failed', 'duplicate'].includes(item.upload.state)).map(item => item.upload.inputId ?? item.upload.id),
  ]).size;
  return (
    <>
      <Link className="back-link" to="/manage/tasks">
        <Icon name="back" size={18} />
        任务与报告
      </Link>
      <PageHeading
        title={batch.label}
        subtitle={`${batch.mode === "ordinary" ? "普通上传" : "智能整理"} · ${dateTime(batch.createdAt)}`}
        action={
          <StatusBadge
            tone={batch.state === "completed" ? "success" : "neutral"}
          >
            {batchLabels[batch.state]}
          </StatusBadge>
        }
      />
      <ErrorNotice
        error={query.error}
        stale
        retry={() => void query.refetch()}
      />
      <div className="metric-grid">
        <div className="metric panel">
          <span>已入库</span>
          <strong>{ingested}</strong>
        </div>
        <div className="metric panel">
          <span>需要处理</span>
          <strong>{exceptions}</strong>
        </div>
        <div className="metric panel">
          <span>原始输入</span>
          <strong>{batch.manifestState === 'sealed' ? batch.expectedInputs : '未核验'}</strong>
        </div>
      </div>
      <BatchInputReport batch={batch} inputs={inputs} />
      {batch.state === "waiting_executor" && (
        <div className="notice notice--warning">
          <strong>等待可用的外部执行器</strong>
          <p>
            文件已接收的状态与智能处理分开记录。执行器未配置、离线或权限不足时不会声称已完成研究。
          </p>
        </div>
      )}
      <section className="panel">
        <h2>后续处理</h2>
        <dl className="facts">
          <div>
            <dt>独立备份</dt>
            <dd>
              {batch.backupState === "awaiting_external_backup"
                ? "等待独立备份确认"
                : "尚无覆盖本批内容版本的备份凭据"}
            </dd>
          </div>
          <div>
            <dt>来源文件</dt>
            <dd>保留原件</dd>
          </div>
          <div>
            <dt>研究调用</dt>
            <dd>{batch.researchCalls}</dd>
          </div>
          <div>
            <dt>最近更新</dt>
            <dd>{dateTime(batch.updatedAt)}</dd>
          </div>
        </dl>
      </section>
      <div className="section-heading">
        <h2>逐项结果</h2>
        <button
          className="button button--small"
          onClick={() => void query.refetch()}
        >
          读取最新报告
        </button>
      </div>
      {items.length ? (
        <div className="report-items">
          {items.map(({ upload, researchOutcome, candidates }) => (
            <details className="panel" key={upload.id}>
              <summary>
                <span>{upload.fileName}</span>
                <StatusBadge
                  tone={
                    upload.state === "ingested"
                      ? "success"
                      : ["failed", "duplicate"].includes(upload.state)
                        ? "warning"
                        : "neutral"
                  }
                >
                  {uploadLabels[upload.state]}
                </StatusBadge>
              </summary>
              <dl className="facts">
                <div>
                  <dt>原始文件</dt>
                  <dd>
                    {upload.originalFileName || '未记录原名'} · {bytes(upload.size)}
                  </dd>
                </div>
                <div>
                  <dt>已接收</dt>
                  <dd>{bytes(upload.offset)}</dd>
                </div>
                <div>
                  <dt>研究结果</dt>
                  <dd>{researchOutcome || "尚未研究"}</dd>
                </div>
              </dl>
              {upload.errorMessage && (
                <p className="notice notice--error">{upload.errorMessage}</p>
              )}
              {upload.assetId && (
                <Link
                  className="button"
                  to={`/asset/${encodeURIComponent(upload.assetId)}`}
                >
                  查看入库媒体
                </Link>
              )}
              {candidates.length > 0 && (
                <section>
                  <h3>待核对候选与证据</h3>
                  <p className="muted">候选不等于已确认归属。</p>
                  {candidates.map((candidate, index) => (
                    <div
                      className="evidence"
                      key={`${candidate.sourceUrl}:${index}`}
                    >
                      <strong>{candidate.name || "待核对候选"}</strong>
                      <p>{candidate.evidence}</p>
                      <span className="muted break-word">
                        {candidate.sourceUrl}
                      </span>
                    </div>
                  ))}
                </section>
              )}
            </details>
          ))}
        </div>
      ) : (
        <EmptyState title="本批尚无文件记录">
          可以返回上传队列核对接收进度。
        </EmptyState>
      )}
      {!!plans.length && (
        <details className="panel">
          <summary>命名计划记录（{plans.length}）</summary>
          {plans.map((plan) => (
            <p key={plan.id}>
              {plan.state} · <code>{plan.id}</code>
            </p>
          ))}
        </details>
      )}
      {!["completed", "cancelled"].includes(batch.state) && (
        <button
          className="button button--danger"
          onClick={() => setConfirm(true)}
        >
          取消后续处理
        </button>
      )}
      {confirm && (
        <ConfirmDialog
          title="取消本批后续处理？"
          busy={cancellation.isPending}
          onCancel={() => setConfirm(false)}
          onConfirm={() =>
            cancellation.mutate(batch.revision, {
              onSuccess: () => setConfirm(false),
            })
          }
        >
          <p>已入库媒体和已提交结果保留，取消只阻止尚未执行的后续步骤。</p>
          <ErrorNotice error={cancellation.error} />
        </ConfirmDialog>
      )}
    </>
  );
}
