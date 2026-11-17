export function bytes(value: number) {
  // Original UI size labels use binary thresholds and the displayed KB/MB/GB suffixes.
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)} GB`;
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(0)} KB`;
  return `${value} B`;
}
export function count(value: number | undefined) {
  if (value === undefined) return "—";
  if (value >= 10000) {
    const tenThousands = value / 10000;
    return `${tenThousands >= 100 ? Math.round(tenThousands) : tenThousands.toFixed(1).replace(/\.0$/, "")}万`;
  }
  return value.toLocaleString("zh-CN");
}
export function dateTime(value: string | null | undefined) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.valueOf())
    ? "时间不可识别"
    : new Intl.DateTimeFormat("zh-CN", {
        dateStyle: "medium",
        timeStyle: "short",
      }).format(date);
}
export function duration(milliseconds: number) {
  const seconds = Math.floor(milliseconds / 1000);
  const minutes = Math.floor(seconds / 60);
  return seconds >= 3600
    ? `${Math.floor(seconds / 3600)}:${String(minutes % 60).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}`
    : `${minutes}:${String(seconds % 60).padStart(2, "0")}`;
}
export function readableDuration(seconds: number) {
  const whole = Math.floor(seconds);
  if (whole < 60) return `${whole}秒`;
  if (whole < 3600) return `${Math.floor(whole / 60)}分 ${whole % 60}秒`;
  return `${Math.floor(whole / 3600)}小时 ${Math.floor(whole / 60) % 60}分`;
}
