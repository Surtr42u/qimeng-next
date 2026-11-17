export function bytes(value: number) {
  if (value < 1024) return `${value} B`;
  const power = Math.min(Math.floor(Math.log(value) / Math.log(1024)), 4);
  return `${(value / 1024 ** power).toLocaleString("zh-CN", { maximumFractionDigits: 1 })} ${["B", "KiB", "MiB", "GiB", "TiB"][power]}`;
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
    ? `${Math.floor(seconds / 3600)}:${String(minutes % 60).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}`
    : `${minutes}:${String(seconds % 60).padStart(2, "0")}`;
}
export function readableDuration(seconds: number) {
  const whole = Math.floor(seconds);
  if (whole < 60) return `${whole}秒`;
  if (whole < 3600) return `${Math.floor(whole / 60)}分 ${whole % 60}秒`;
  return `${Math.floor(whole / 3600)}小时 ${Math.floor(whole / 60) % 60}分`;
}
