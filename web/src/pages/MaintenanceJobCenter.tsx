import { useRef, useState } from "react";
import { Link } from "react-router-dom";
import { ListChecks } from "lucide-react";
import { useCancelAuthorTask, useCancelScan } from "../data/maintenance";
import { useCancelBatch } from "../data/manage";
import { kindNames, stateNames, useMaintenanceJobDetail, useMaintenanceJobs, type Job } from "../data/maintenance-jobs";
import { ErrorNotice, Loading } from "../components/Feedback";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ResourceBar } from "../components/MaintenanceOverview";
import { Select, SelectContent, SelectItem, SelectTrigger } from "../components/LegacySelect";
import { newId } from "../lib/id";
import { dateTime } from "../lib/format";

const jobKey = (job: Job) => job.kind + "/" + job.id;
function jobPercent(job: Job) {
  return job.completedSteps !== undefined && job.totalSteps !== undefined && job.totalSteps > 0 ? Math.min(100, Math.floor(100 * job.completedSteps / job.totalSteps)) : null;
}

export function MaintenanceJobCenter() {
  const [state, setState] = useState<Job["state"]>();
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]);
  const [selected, setSelected] = useState("");
  const list = useMaintenanceJobs(state, cursors.at(-1));
  const job = list.data?.items.find(row => jobKey(row) === selected);
  const changePage = (next: (string | undefined)[]) => { setCursors(next); setSelected(""); };
  return <div className="maintenance-job-center" aria-label="维护任务中心">
    <section className="chart-card monitor-span-2" aria-label="维护任务列表">
      <div className="rank-head"><h3><ListChecks aria-hidden="true" /> 维护任务中心</h3><button className="pill" disabled={list.isFetching} onClick={() => void list.refetch()}>{list.isFetching ? "刷新中…" : "刷新"}</button></div>
      <p className="rank-note">查看任务列表与详情，或取消仍可停止的任务。</p>
      <div className="maintenance-tabs" role="group" aria-label="任务状态筛选">
        <Select value={state ?? "all"} onValueChange={value => { setState(value in stateNames ? value as Job["state"] : undefined); changePage([undefined]); }}>
          <SelectTrigger aria-label="按状态筛选任务" /><SelectContent><SelectItem value="all">全部状态</SelectItem>
            {Object.entries(stateNames).map(([value, name]) => <SelectItem key={value} value={value}>{name}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>
      {list.isError && <div className="monitor-query-error" role="alert"><span>{list.data ? "任务列表刷新失败，以下保留上次成功结果。" : "任务列表读取失败，请检查网络或服务端后重试。"}</span><button className="pill" disabled={list.isFetching} onClick={() => void list.refetch()}>重试读取</button></div>}
      {!list.data ? (list.isError ? <p className="grid-empty">暂无可用的任务列表。</p> : <Loading text="任务列表加载中…" />)
        : !list.data.items.length ? <p className="grid-empty">当前筛选下没有维护任务</p> : <div style={{ overflowX: "auto" }}>
          <table className="log-table"><thead><tr>{["创建时间", "类型", "状态", "进度", "步骤", "更新时间", "操作"].map(name => <th scope="col" key={name}>{name}</th>)}</tr></thead>
            <tbody>{list.data.items.map(item => { const key = jobKey(item); const percent = jobPercent(item); return <tr key={key}>
              <td>{dateTime(item.createdAt)}</td><td>{kindNames[item.kind]}</td><td>{stateNames[item.state]}</td><td>{percent === null ? "—" : percent + "%"}</td><td>{item.completedSteps ?? "—"}{item.totalSteps !== undefined && " / " + item.totalSteps}</td><td>{dateTime(item.updatedAt)}</td>
              <td><button className={"pill" + (key === selected ? " active" : "")} aria-pressed={key === selected} onClick={() => setSelected(key)}>查看</button></td>
            </tr>; })}</tbody></table>
        </div>}
      {(cursors.length > 1 || list.data?.nextCursor) && <div className="maintenance-tabs" role="group" aria-label="任务列表分页">
        <button className="pill" disabled={cursors.length === 1 || list.isFetching} onClick={() => changePage(cursors.slice(0, -1))}>上一页</button><span className="rank-note">第 {cursors.length} 页</span>
        <button className="pill" disabled={!list.data?.nextCursor || list.isFetching} onClick={() => { if (list.data?.nextCursor) changePage([...cursors, list.data.nextCursor]); }}>下一页</button>
      </div>}
    </section>
    {job ? <JobDetail key={jobKey(job)} job={job} refreshList={() => void list.refetch()} />
      : <section className="chart-card monitor-span-2" aria-label="维护任务详情"><div className="rank-head"><h3>任务详情</h3></div><p className="grid-empty">选择左侧任务查看类型、状态、进度、步骤与工件。</p></section>}
  </div>;
}

function JobDetail({ job, refreshList }: { job: Job; refreshList: () => void }) {
  const detail = useMaintenanceJobDetail(job);
  const scan = useCancelScan(job.id), batch = useCancelBatch(job.id), author = useCancelAuthorTask(job.id);
  const [confirm, setConfirm] = useState(false);
  const key = useRef("");
  const data = detail.data;
  const mutating = scan.isPending || batch.isPending || author.isPending;
  const cancelError = scan.error || batch.error || author.error;
  const canCancel = job.cancellable && data?.canCancel && !detail.isError && !detail.isFetching;
  const refresh = () => { refreshList(); void detail.refetch(); };
  const cancel = () => {
    if (!canCancel || mutating) return;
    const handlers = { onSuccess: () => { setConfirm(false); refresh(); }, onError: refresh };
    if (job.kind === "scan") scan.mutate(key.current, handlers);
    if (job.kind === "author") author.mutate(undefined, handlers);
    if (job.kind === "batch" && data?.revision !== undefined) batch.mutate(data.revision, handlers);
  };
  const percent = jobPercent(job);
  return <section className="chart-card monitor-span-2" aria-label="维护任务详情">
    <div className="rank-head"><h3>任务详情</h3><button className="pill" disabled={detail.isFetching} onClick={refresh}>{detail.isFetching ? "刷新中…" : "刷新详情"}</button></div>
    <ErrorNotice error={detail.error} stale={!!data} retry={refresh} />
    {detail.isPending ? <Loading text="任务详情加载中…" /> : data && <>
      <div className="monitor-server-meta">
        {([["类型", kindNames[job.kind]], ["状态", stateNames[job.state]], ["创建", dateTime(job.createdAt)], ["任务", job.label], ...data.fields] as [string, string | number][]).map(([name, value]) => <div key={name}><span>{name}</span><strong>{value}</strong></div>)}
      </div>
      {data.progress ? <><div className="monitor-row"><span>{data.progress.label}</span><strong>{data.progress.done} / {data.progress.total}</strong></div><ResourceBar value={data.progress.total > 0 ? 100 * data.progress.done / data.progress.total : null} label="维护任务进度" /></> : percent !== null && <><div className="monitor-row"><span>已完成步骤占比</span><strong>{percent}%</strong></div><ResourceBar value={percent} label="维护任务进度" /></>}
      <p className="rank-note">任务 ID <code>{job.id}</code></p>
      <div className="upload-queue-actions" style={{ marginTop: 12 }}>
        {job.cancellable && data.canCancel && <button className="pill" disabled={!canCancel || mutating} onClick={() => { scan.reset(); batch.reset(); author.reset(); key.current = newId(); setConfirm(true); }}>取消任务</button>}
        {data.link && <Link className="pill" to={data.link}>打开完整记录</Link>}
      </div>
      <DetailRows title="最近步骤" columns={["步骤", "状态", "明细"]} rows={data.steps} />
      <DetailRows title="最近工件" columns={["类别", "引用", "SHA-256"]} rows={data.artifacts} />
    </>}
    {confirm && <ConfirmDialog danger title="取消维护任务？" confirmLabel="确认取消任务" busy={mutating} onCancel={() => setConfirm(false)} onConfirm={cancel}>
      <p>{job.label || kindNames[job.kind]}（{job.id}）。已写入的内容及记录保留。</p><ErrorNotice error={cancelError} />
      {!canCancel && !detail.isFetching && <p>任务状态已变化，请返回核对。</p>}
    </ConfirmDialog>}
  </section>;
}
function DetailRows({ title, columns, rows }: { title: string; columns: string[]; rows?: [string, string, string?][] }) {
  return <><h4 style={{ margin: "18px 0 0" }}>{title}</h4>{rows?.length ? <div style={{ overflowX: "auto" }}><table className="log-table">
    <thead><tr>{columns.map(column => <th scope="col" key={column}>{column}</th>)}</tr></thead>
    <tbody>{rows.map(([name, value, note], i) => <tr key={i}><td>{name}</td><td>{value}</td><td title={note}>{note || "—"}</td></tr>)}</tbody></table></div> : <p className="grid-empty">暂无{title.slice(2)}记录。</p>}</>;
}
