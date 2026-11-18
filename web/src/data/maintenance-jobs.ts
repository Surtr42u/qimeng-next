import { useQuery } from "@tanstack/react-query";
import { api, read, type Schema } from "./client";
import { useRemoteKey } from "./session";
import { getBackup } from "./backups";
import { bytes, dateTime } from "../lib/format";

export type Job = Schema<"MaintenanceJob">;
export const stateNames: Record<Job["state"], string> = { waiting: "等待中", running: "运行中", completed: "已完成", partial: "部分完成", failed: "失败", cancelled: "已取消" };
export const kindNames: Record<Job["kind"], string> = { scan: "目录扫描", batch: "入库与整理", backup: "备份导出", protection: "批次保护", author: "作者资料维护", asset: "作品维护" };

export function useMaintenanceJobs(state?: Job["state"], cursor?: string) {
  const scope = useRemoteKey();
  return useQuery({ queryKey: [...scope, "maintenance-jobs", state, cursor], gcTime: 0,
    queryFn: ({ signal }) => read(api.GET("/maintenance/jobs", { params: { query: { state, cursor, limit: 50 } }, signal })) });
}
type JobDetail = {
  fields: [string, string | number][]; steps?: [string, string, string?][]; artifacts?: [string, string, string?][];
  progress?: { label: string; done: number; total: number }; revision?: number; canCancel?: boolean; link?: string;
};
const inputState: Record<Schema<"BatchInput">["state"], string> = { pending: "待接收", receiving: "接收中", staged: "待整理", ingested: "已入库", duplicate: "重复", cancelled: "已取消", failed: "失败" };
async function getJobDetail(job: Job, signal: AbortSignal): Promise<JobDetail> {
  const params = { path: { id: job.id } };
  const id = encodeURIComponent(job.id);
  switch (job.kind) {
    case "scan": {
      const scan = await read(api.GET("/scans/{id}", { params, signal }));
      return { fields: [["已核对", scan.processed], ["新增", scan.added], ["移动", scan.moved], ["内容变化", scan.changed], ["跳过", scan.skipped], ["问题", scan.issueCount], ["更新", dateTime(scan.updatedAt)]],
        canCancel: scan.state === "queued" || scan.state === "running", steps: scan.issues.map(issue => [issue.relativePath, issue.code]), link: "/manage/scans/" + id };
    }
    case "batch": {
      const { batch, inputs, plans } = await read(api.GET("/batches/{id}", { params, signal }));
      return { fields: [["批次", batch.label || batch.id], ["原始输入", batch.expectedInputs], ["更新", dateTime(batch.updatedAt)]],
        steps: inputs.map(input => [input.originalFileName || input.id, inputState[input.state], input.errorCode]),
        artifacts: plans.map(plan => ["整理计划", plan.id, plan.fingerprint]), revision: batch.revision, canCancel: batch.state !== "completed" && batch.state !== "cancelled", link: "/manage/tasks/" + id };
    }
    case "backup": {
      const backup = await getBackup(job.id, signal);
      const completed = backup.archiveHash && backup.exportState === "completed";
      return { fields: [["快照资产", backup.assetCount], ["归档", backup.exportState ?? "未导出"], ["导出时间", dateTime(backup.exportedAt)], ...(backup.exportError ? [["错误", backup.exportError] as [string, string]] : [])],
        progress: backup.exportedFiles !== undefined && backup.totalFiles !== undefined ? { label: "已导出文件", done: backup.exportedFiles, total: backup.totalFiles } : undefined,
        artifacts: completed ? [[".qnext", backup.archiveBytes === undefined ? "—" : bytes(backup.archiveBytes), backup.archiveHash]] : [], link: "/manage/backup" };
    }
    case "protection": {
      if (!job.batchId) throw new Error("此保护记录缺少对应的批次标识。");
      const proof = await read(api.GET("/batches/{id}/protections/{protectionId}", { params: { path: { id: job.batchId, protectionId: job.id } }, signal }));
      return { fields: [["备份目标", proof.targetId], ["故障域", proof.failureDomain], ["校验时间", dateTime(proof.verifiedAt)], ...(proof.errorCode ? [["错误", proof.errorCode] as [string, string]] : [])],
        steps: proof.items.map(item => [item.inputId, item.state === "protected" ? "已保护" : item.state === "retained" ? "保留来源" : "待处理", item.reason]),
        artifacts: [["数据库检查点", proof.checkpointId, proof.snapshotHash], ...(proof.proofHash ? [["保护凭据", proof.id, proof.proofHash] as [string, string, string]] : [])], link: "/manage/tasks/" + encodeURIComponent(job.batchId) };
    }
    case "author": {
      const { task, author } = await read(api.GET("/author-tasks/{id}", { params, signal }));
      return { fields: [["作者", author.name], ["生效截止", dateTime(task.expiresAt)], ["已用授权次数", task.usedUpdates + " / " + task.maxUpdates]], canCancel: task.state === "authorized", link: "/manage/author-tasks/" + id };
    }
    case "asset": {
      const task = await read(api.GET("/asset-tasks/{id}", { params, signal }));
      return { fields: [["作品版本", task.contentVersion], ["生效截止", dateTime(task.expiresAt)], ["已用授权次数", task.usedUpdates + " / " + task.maxUpdates]],
        artifacts: [["原件", task.assetId, task.hash]], link: "/asset/" + encodeURIComponent(task.assetId) };
    }
  }
}
export function useMaintenanceJobDetail(job: Job) {
  const scope = useRemoteKey();
  return useQuery({ queryKey: [...scope, "maintenance-job-detail", job.kind, job.id, job.rawState, job.updatedAt], gcTime: 0,
    queryFn: ({ signal }) => getJobDetail(job, signal) });
}
