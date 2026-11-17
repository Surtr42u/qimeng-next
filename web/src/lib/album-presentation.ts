import type { Asset } from "../data/media";
import type { Schema } from "../data/client";

// Use the same fixed offset sent with the query, including across daylight-saving changes.
function localDate(value: string, offset: number) {
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp)
    ? new Date(timestamp + offset * 60_000)
    : undefined;
}
export function albumGroups(
  assets: Asset[],
  counts: Schema<"DateCountBucket">[] | undefined,
  offset: number,
  now = Date.now(),
  unknownDateCount?: number,
) {
  const totals = new Map(counts?.map((value) => [value.date, value.fileCount]));
  const days = new Map<string, Asset[]>();
  for (const asset of assets) {
    const value = asset.fileDate;
    const date = value ? localDate(value, offset) : undefined;
    const key = date?.toISOString().slice(0, 10) ?? "";
    const group = days.get(key);
    if (group) group.push(asset);
    else days.set(key, [asset]);
  }
  const today = new Date(now + offset * 60_000).toISOString().slice(0, 10);
  const currentDay = Date.parse(`${today}T00:00:00Z`);
  return [...days.entries()]
    .sort(([left], [right]) => right.localeCompare(left))
    .map(([key, items]) => {
      const distance = key
        ? (currentDay - Date.parse(`${key}T00:00:00Z`)) / 86_400_000
        : NaN;
      const label =
        distance === 0
          ? "今天"
          : distance === 1
            ? "昨天"
            : distance > 1 && distance < 7
              ? `周${"日一二三四五六"[new Date(`${key}T00:00:00Z`).getUTCDay()]}`
              : key;
      return { key, label, items, total: key ? totals.get(key) : unknownDateCount };
    });
}
