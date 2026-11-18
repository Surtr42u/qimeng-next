import { useEffect } from "react";
import { Link } from "react-router-dom";
import { MaintenanceOverview, ResourceBar } from "../components/MaintenanceOverview";
import { ManagePageHeader } from "../components/LegacyManageHeader";
import { MaintenanceJobCenter } from "./MaintenanceJobCenter";
import { useClientErrors, useMaintenanceMonitor, useStorageDetails } from "../data/system-monitor";
import { dateTime } from "../lib/format";

export function MaintenancePage() {
  const monitor = useMaintenanceMonitor();
  const trash = monitor.data?.trashCount;
  const errors = monitor.data?.clientErrorCount;
  return <div className="page" id="page-maintenance">
    <div className="page-head monitor-page-head">
      <div><h2>服务与维护</h2><p>运行概况、资源使用与常用维护</p></div>
      <span className="monitor-update" role="status">{monitor.stateLabel} · 每 2 秒</span>
    </div>
    {monitor.isError && <div className="monitor-query-error" role="alert">
      <span>{monitor.data ? "监控更新失败，以下保留上次成功读数。" : "暂时无法读取监控状态，维护工具仍可使用。"}</span>
      <button className="pill" disabled={monitor.isFetching} onClick={() => void monitor.refetch()}>重试监控</button>
    </div>}
    <MaintenanceOverview monitor={monitor}
      trashLabel={trash !== undefined ? trash + " 条待处理" : monitor.isError ? "条目数暂不可用" : "正在读取…"}
      logsLabel={errors !== undefined ? "保留 " + errors + " 条上报" : monitor.isError ? "更新失败，查看详情" : "正在读取…"} />
    <MaintenanceJobCenter />
  </div>;
}

export function StorageDetailsPage() {
  const { monitor, resources, more, incomplete } = useStorageDetails();
  return <div className="page" id="page-storage-details">
    <ManagePageHeader title="存储明细" description="按监控目录展示容量，同卷目录可能重复计入。" backTo="/maintenance" backLabel="服务与维护" />
    {monitor.isError && <div className="monitor-query-error" role="alert"><span>存储状态读取失败{monitor.data ? "，保留上次成功读数" : ""}</span><button className="pill" disabled={monitor.isFetching} onClick={() => void monitor.refetch()}>重试存储</button></div>}
    {!monitor.data ? <p className="grid-empty">{monitor.isError ? "暂无可用存储读数" : "正在读取存储状态…"}</p> : <>
      <section className="chart-card" aria-label="监控目录容量合计"><h3>{incomplete ? "已读取目录容量合计" : "监控目录容量合计"}</h3><ResourceBar value={resources.diskPercent} label="合计存储使用率" /><p>{resources.diskCaption} · 约 {resources.diskFree} 可用</p></section>
      {resources.disks.map(disk => <section className="chart-card monitor-volume" key={disk.key} aria-label={disk.label}>
        <div className="monitor-row"><h3>{disk.label}</h3><span>{disk.percent == null ? "—" : disk.percent.toFixed(1) + "% 已用"}</span></div>
        <p className="monitor-mount">{disk.mount}</p><ResourceBar value={disk.percent} label={disk.label + "使用率"} /><p className="monitor-muted">{disk.caption}</p>
      </section>)}
      {resources.disks.length === 0 && <p className="grid-empty">服务端未提供监控目录读数</p>}
      {more.isError && <div className="monitor-query-error" role="alert">后续监控目录读取失败，已读取目录仍可查看。</div>}
      {incomplete && <button className="pill" disabled={more.isFetching} onClick={() => void more.fetchNextPage()}>{more.isFetching ? "读取中…" : more.isError ? "重试后续目录" : "更多监控目录"}</button>}
    </>}
  </div>;
}

const screens = { home: "首页", album: "相册", search: "搜索", detail: "作品详情", upload: "上传", author: "作者", settings: "设置", maintenance: "服务与维护", backup: "备份与恢复", other: "其他" };
export function ClientErrorsPage() {
  const logs = useClientErrors();
  const entries = logs.data?.pages.flatMap(page => page.items) ?? [];
  useEffect(() => {
    if (logs.hasNextPage && !logs.isFetching && !logs.isError) void logs.fetchNextPage();
  }, [logs.hasNextPage, logs.isFetching, logs.isError, logs.fetchNextPage]);
  return <div className="page" id="page-client-errors">
    <div className="page-head"><div><h2>客户端异常</h2><p>用户端崩溃 / 错误日志 · 保留最新 200 条 · 新在上</p></div><Link className="pill" to="/maintenance">返回服务与维护</Link></div>
    {logs.isError && <div className="monitor-query-error" role="alert"><span>客户端异常记录读取失败{logs.data ? "，以下保留上次成功记录" : ""}</span><button className="pill" disabled={logs.isFetching} onClick={() => void (logs.isFetchNextPageError ? logs.fetchNextPage() : logs.refetch())}>重试日志</button></div>}
    {!logs.data ? <p className="grid-empty">{logs.isError ? "暂无可用的客户端异常记录" : "客户端异常记录加载中…"}</p>
      : !entries.length ? <p className="grid-empty">暂无客户端异常上报</p> : <section className="chart-card" aria-label="全部客户端异常">
        <p>{logs.hasNextPage ? "已读取 " : "全部 "}{entries.length} 条</p>
        <div className="monitor-log-scroll"><table className="log-table">
          <thead><tr><th>时间</th><th>级别</th><th>消息</th><th>页面</th></tr></thead>
          <tbody>{entries.map(entry => <tr key={entry.id}><td>{dateTime(entry.occurredAt)}</td><td>{entry.level === "error" ? "错误" : "警告"}</td><td>{entry.message}{entry.httpStatus !== undefined && " · HTTP " + entry.httpStatus}</td><td>{screens[entry.screen]}</td></tr>)}</tbody>
        </table></div>
      </section>}
  </div>;
}
