import { useEffect, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { api, read } from "./client";
import type { paths } from "../api/generated/schema";
import { useRemoteKey } from "./session";
import { bytes, duration } from "../lib/format";

type Status = paths["/system/status"]["get"]["responses"][200]["content"]["application/json"];
type Sample = { at: number; rx: number; tx: number };
type WindowSamples = { boot?: string; base?: Sample; points: Sample[] };
const empty: WindowSamples = { points: [] };
export function networkWindow(previous: WindowSamples, status: Status): WindowSamples {
  const boot = JSON.stringify([status.instanceId, status.startedAt]);
  const sample = { at: Date.parse(status.sampledAt), rx: status.receivedBytes, tx: status.sentBytes };
  if (!Number.isFinite(sample.at) || !Number.isSafeInteger(sample.rx) || !Number.isSafeInteger(sample.tx)) return empty;
  const before = previous.base;
  if (boot === previous.boot && before && sample.at <= before.at) return previous;
  if (boot !== previous.boot || !before || sample.rx < before.rx || sample.tx < before.tx || sample.at - before.at > 120000) return { boot, base: sample, points: [] };
  const seconds = (sample.at - before.at) / 1000;
  const point = { at: sample.at, rx: Math.round((sample.rx - before.rx) / seconds), tx: Math.round((sample.tx - before.tx) / seconds) };
  return { boot, base: sample, points: [...previous.points.filter(item => item.at > sample.at - 120000).slice(-59), point] };
}
const percentage = (used?: number, total?: number) => used !== undefined && total !== undefined && total > 0 ? Math.min(100, 100 * used / total) : null;
function resources(status?: Status, mounts = status?.mounts ?? []) {
  const disks = mounts.map(mount => ({ key: mount.id, label: mount.label, mount: mount.mount,
    percent: mount.available ? percentage(mount.usedBytes, mount.totalBytes) : null,
    caption: mount.available && mount.usedBytes !== undefined && mount.totalBytes !== undefined ? bytes(mount.usedBytes) + " / " + bytes(mount.totalBytes) : "采集不可用" }));
  const usable = mounts.filter(mount => mount.available && mount.usedBytes !== undefined && mount.totalBytes !== undefined && mount.totalBytes > 0);
  const total = usable.reduce((sum, mount) => sum + mount.totalBytes!, 0);
  const used = usable.reduce((sum, mount) => sum + mount.usedBytes!, 0);
  const available = usable.length && usable.every(m => m.availableBytes !== undefined) ? usable.reduce((sum, m) => sum + m.availableBytes!, 0) : undefined;
  const seconds = status?.uptimeSeconds;
  return {
    disks, diskPercent: percentage(used, total), diskFree: available === undefined ? "—" : bytes(available),
    diskCaption: usable.length ? (usable.length < mounts.length ? "已取得读数的目录 " : "") + bytes(used) + " / " + bytes(total) : "暂未取得存储读数",
    cpu: status?.cpuPercent ?? null, memory: percentage(status?.memoryUsedBytes, status?.memoryTotalBytes),
    memoryCaption: status?.memoryUsedBytes !== undefined && status.memoryTotalBytes !== undefined ? bytes(status.memoryUsedBytes) + " / " + bytes(status.memoryTotalBytes) : "暂未取得内存读数",
    uptime: seconds === undefined ? "—" : (seconds >= 86400 ? Math.floor(seconds / 86400) + " 天 " : "") + duration((seconds % 86400) * 1000),
    version: status?.version || "版本未提供",
  };
}
export function useMaintenanceMonitor() {
  const scope = useRemoteKey();
  const query = useQuery({ queryKey: [...scope, "system-status"], gcTime: 0, refetchInterval: 2000, refetchIntervalInBackground: false,
    queryFn: ({ signal }) => read(api.GET("/system/status", { signal })) });
  const [samples, setSamples] = useState<WindowSamples>(empty);
  useEffect(() => { const value = query.data; if (value) setSamples(previous => networkWindow(previous, value)); }, [query.data]);
  const traffic = (["rx", "tx"] as const).map(direction => {
    const points = samples.points.map(p => p[direction]);
    return { direction, label: direction === "rx" ? "接收 / 下行" : "发送 / 上行", waiting: query.isError ? "暂无读数" : query.data ? "采样中…" : "正在读取…",
      value: points.length ? bytes(points.at(-1)!) : null, peak: points.length ? bytes(Math.max(...points)) + "/s" : "—",
      cumulative: direction === "rx" ? query.data?.receivedBytes : query.data?.sentBytes };
  });
  return { ...query, resources: resources(query.data), traffic, points: samples.points,
    stateLabel: query.isError ? (query.data ? "更新失败 · 保留上次读数" : "监控暂不可用") : query.data ? "正在自动更新" : "正在读取状态" };
}
export function useClientErrors() {
  const scope = useRemoteKey();
  return useInfiniteQuery({ queryKey: [...scope, "client-errors"], gcTime: 0, initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => read(api.GET("/system/client-errors", { params: { query: { cursor: pageParam, limit: 50 } }, signal })),
    getNextPageParam: (page, pages) => pages.length < 4 ? page.nextCursor || undefined : undefined });
}

export function useStorageDetails() {
  const monitor = useMaintenanceMonitor();
  const scope = useRemoteKey();
  const first = monitor.data?.nextMountCursor;
  const more = useInfiniteQuery({ queryKey: [...scope, "system-storage", first], enabled: false, gcTime: 0,
    initialPageParam: first, queryFn: ({ pageParam, signal }) => read(api.GET("/system/storage", { params: { query: { cursor: pageParam, limit: 50 } }, signal })),
    getNextPageParam: page => page.nextCursor || undefined });
  const mounts = new Map((monitor.data?.mounts ?? []).map(mount => [mount.id, mount]));
  for (const page of more.data?.pages ?? []) for (const mount of page.items) if (!mounts.has(mount.id)) mounts.set(mount.id, mount);
  return { monitor, resources: resources(monitor.data, [...mounts.values()]), more, incomplete: !!first && (!more.data || more.hasNextPage) };
}
