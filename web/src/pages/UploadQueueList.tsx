import { useRef } from "react";
import { Link } from "react-router-dom";
import { useUploadQueue } from "../data/upload";
import { isQueueTerminal } from "../data/upload-queue";
import type { QueueEntry, UploadPhase } from "../data/upload-queue";
import { bytes } from "../lib/format";

const phaseNames: Record<UploadPhase, string> = {
  waiting_file: "需重新选择文件", queued: "排队中", hashing: "核验文件内容",
  uploading: "上传中", completing: "服务器处理中…", ingested: "已完成",
  staged: "已接收，等待整理", duplicate: "重复内容，未新增", failed: "失败",
  paused: "已暂停", cancelled: "已取消",
};

export function UploadRow({ entry }: { entry: QueueEntry }) {
  const { queue } = useUploadQueue();
  const reselect = useRef<HTMLInputElement>(null);
  const active = ["hashing", "uploading", "completing", "queued"].includes(entry.phase);
  const progress = entry.phase === "hashing" ? entry.hashProgress : entry.offset;
  const status = entry.phase === "ingested" ? "done" : entry.phase === "waiting_file" ? "needs-file" : entry.phase === "cancelled" ? "canceled" : entry.phase;
  const statusText = entry.phase === "uploading" ? `上传中 ${Math.floor(entry.offset / entry.size * 100)}%` : phaseNames[entry.phase];
  return (
    <li className={`upload-task upload-task--${status}`}>
      <div className="upload-task-head"><strong>{entry.actualFileName || entry.name}</strong><span className="upload-task-state">{statusText}</span></div>
      {entry.actualFileName && entry.actualFileName !== entry.name && <p className="rank-note">原文件名：{entry.name}</p>}
      <div className="upload-task-meta">
        <span>{bytes(entry.size)}</span>
        <span>{entry.libraryName} / {entry.request.relativeDirectory || "库根目录"}</span>
        {entry.authorName && <span>作者：{entry.authorName}</span>}
        {entry.request.mode === "intelligent" && <span>智能整理</span>}
      </div>
      {(active || entry.phase === "waiting_file" && entry.offset > 0) && <progress className="upload-task-progress" max={entry.size} value={progress} aria-label={`${entry.name} 上传进度`} />}
      {entry.phase === "waiting_file" && <p className="rank-note">{entry.offset > 0 ? "页面刷新中断——重新选择同名文件并核对内容后从断点续传" : "页面刷新中断——重新选择同名文件后恢复上传"}</p>}
      {entry.waitingFor && <p className="rank-note">保持命名顺序，等待前项“{entry.waitingFor}”完成。前项失败或暂停时，请先重试或明确取消前项。</p>}
      {entry.error && <p className="upload-err" role="alert">{entry.error}</p>}
      {entry.archiveNote && <p className={entry.archiveState === "failed" ? "upload-err" : "rank-note"} role="status">{entry.archiveNote}</p>}
      <div className="upload-queue-actions">
        {entry.archiveState === "failed" && <button className="pill" type="button" onClick={() => reselect.current?.click()}>重选原件并重试归档</button>}
        {entry.assetId && <Link className="pill" to={`/asset/${encodeURIComponent(entry.assetId)}`}>查看入库内容</Link>}
        {entry.request.batchId && <Link className="pill" to={`/manage/tasks/${encodeURIComponent(entry.request.batchId)}`}>本批报告</Link>}
        {active && <button className="pill" type="button" onClick={() => queue?.pause(entry.id)}>暂停</button>}
        {!isQueueTerminal(entry.phase) && !active && (
          <>
            <button className="pill" type="button" title={entry.phase === "waiting_file" ? "重新选择同名文件后恢复该条上传" : undefined} onClick={() => entry.phase === "waiting_file" ? reselect.current?.click() : queue?.retry(entry.id)}>{entry.phase === "waiting_file" ? "重选文件" : "继续 / 重试"}</button>
            <button className="pill" type="button" onClick={() => void queue?.cancel(entry.id)}>取消此项</button>
          </>
        )}
        <input ref={reselect} type="file" hidden aria-label={`重新选择 ${entry.name}`} onChange={event => { const file = event.target.files?.[0]; event.target.value = ""; if (file) { if (entry.archiveState === "failed") queue?.startArchive(entry.id, file); else queue?.retry(entry.id, file); } }} />
      </div>
    </li>
  );
}
