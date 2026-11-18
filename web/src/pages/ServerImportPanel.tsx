import { useRef, useState } from "react";
import { useLocalSyncStatus, useTriggerLocalSync } from "../data/local-sync";
import type { Schema } from "../data/client";
import { ErrorNotice } from "../components/Feedback";
import { newId } from "../lib/id";
import { bytes, dateTime } from "../lib/format";

export function ServerImportPanel() {
  const [expanded, setExpanded] = useState(false);
  const query = useLocalSyncStatus(expanded);
  const trigger = useTriggerLocalSync();
  const key = useRef(newId());
  const status = query.data;
  const rows = (items: Schema<"LocalSyncItem">[], label: string) => items.length > 0 && <div className="ls-sec"><h4 className="ls-sec-title">{label}（{items.length}）</h4><ul className="ls-list">{items.map(item => <li key={item.path}><span className="ls-path">{item.path}</span><span>{item.libraryName || ""} · {bytes(item.sizeBytes)}</span>{item.error && <small>{item.error}</small>}</li>)}</ul></div>;
  return <details className="chart-card server-import-panel" onToggle={event => setExpanded(event.currentTarget.open)}>
    <summary><span><strong>服务器目录自动导入</strong><small>高级入口 · 处理服务器投放目录</small></span>{status && <span className="ls-badges"><span className="pill">{status.running ? "扫描中" : status.paused ? "已暂停" : status.enabled ? "已启用" : "未启用"}</span></span>}</summary>
    <div className="server-import-body"><p className="rank-note">用于无人值守投放。按服务器目录名匹配库，接收稳定的媒体与作者 TXT。已成功接收的相同版本会跳过，来源文件保留。</p>
      <ErrorNotice error={query.error} stale={!!status} retry={() => void query.refetch()} />
      {status ? <><div className="ls-info"><span>同步根：<b>{status.root || "未配置"}</b></span><span>轮询周期：每 <b>{status.intervalSeconds}</b> 秒</span><span>上一轮扫描：<b>{status.lastCycleAt ? dateTime(status.lastCycleAt) : "从未运行"}</b></span><span>累计已同步：<b>{status.syncedTotal}</b></span></div>
        {status.lastError && <p className="ls-warn" role="alert">通道异常：{status.lastError}</p>}
        <p className="ls-summary">等待稳定 {status.counts.waitingStable} · 失败 {status.counts.failed} · 忽略 {status.counts.ignored}</p>
        {rows(status.items.filter(item => item.state === "failed"), "失败")}{rows(status.items.filter(item => item.state === "ignored"), "忽略")}{rows(status.recentSynced, "最近成功")}
        {!status.items.length && !status.recentSynced.length && <p className="grid-empty">同步源内暂无待处理文件</p>}
        <div className="settings-actions"><button className="save-btn" type="button" disabled={trigger.isPending || status.running || status.paused || query.isError || !status.enabled} onClick={() => trigger.mutate(key.current, { onSuccess: () => { key.current = newId(); } })}>{trigger.isPending ? "触发中…" : "立即同步"}</button>{!status.root && <span className="ls-hint">尚未配置服务器自动导入目录，日常使用上方手动上传即可。</span>}</div>
        <ErrorNotice error={trigger.error} />
      </> : !query.isError && <p className="grid-empty">通道状态加载中…</p>}
    </div>
  </details>;
}
