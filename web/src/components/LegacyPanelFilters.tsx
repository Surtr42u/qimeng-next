import { useState } from "react";
import type { ReactNode } from "react";
import type { AssetFilters } from "../data/media";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "./LegacySelect";

// Labels and DOM/classes retained from the existing PanelFilters; ranges are server enums.
const sorts = [
  ["default", "默认"],
  ["views", "观看次数"],
  ["size", "文件大小"],
] as const;
const orders = [
  ["desc", "降序"],
  ["asc", "升序"],
] as const;
const plays = [
  ["all", "全部"],
  ["none", "未播放"],
  ["low", "1-5"],
  ["mid", "5-20"],
  ["high", ">20"],
] as const;
const sizes = [
  ["all", "全部"],
  ["lt1m", "<1MB"],
  ["m1to10", "1-10MB"],
  ["m10to50", "10-50MB"],
  ["gt50m", ">50MB"],
] as const;
const times = [
  ["all", "全部"],
  ["today", "今天"],
  ["week", "本周"],
  ["month", "本月"],
  ["three_months", "近三月"],
  ["year", "本年"],
  ["year_range", "按年份区间"],
] as const;
const tagModes = [
  ["any", "模糊"],
  ["all", "精确"],
] as const;
const years = Array.from({ length: 11 }, (_, index) =>
  String(new Date().getFullYear() - index),
);
function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="f-row">
      <span className="f-label">{label}</span>
      <div className="f-opts">{children}</div>
    </div>
  );
}
function Options<T extends string>({
  options,
  selected,
  onChange,
}: {
  options: readonly (readonly [T, string])[];
  selected: string;
  onChange: (value: T) => void;
}) {
  return (
    <>
      {options.map(([value, label]) => (
        <button
          type="button"
          key={value}
          className={`pill${selected === value ? " active" : ""}`}
          aria-pressed={selected === value}
          onClick={() => onChange(value)}
        >
          {label}
        </button>
      ))}
    </>
  );
}
export function panelActive(filters: AssetFilters) {
  return (
    (filters.sort && filters.sort !== "default") ||
    filters.order === "asc" ||
    (filters.playRange && filters.playRange !== "all") ||
    (filters.sizeRange && filters.sizeRange !== "all") ||
    (filters.timeRange && filters.timeRange !== "all") ||
    !!filters.tags?.length
  );
}
export function LegacyPanelFilters({
  hidden,
  filters,
  tagPool,
  onChange,
  tagActions,
}: {
  hidden: boolean;
  filters: AssetFilters;
  tagPool: string[];
  onChange: (patch: Partial<AssetFilters>) => void;
  tagActions?: ReactNode;
}) {
  const [inputOpen, setInputOpen] = useState(false);
  const selected = filters.tags ?? [];
  const pool = [...new Set([...tagPool, ...selected])].sort((a, b) =>
    a.localeCompare(b, "zh-CN"),
  );
  const toggle = (name: string) =>
    onChange({
      tags: selected.includes(name)
        ? selected.filter((value) => value !== name)
        : [...selected, name],
    });
  const add = (raw: string) => {
    const value = raw.trim();
    if (value && !selected.includes(value))
      onChange({ tags: [...selected, value] });
    setInputOpen(false);
  };
  return (
    <div className="search-filters" hidden={hidden}>
      <div>
        <Row label="排序">
          <Options
            options={sorts}
            selected={filters.sort ?? "default"}
            onChange={(sort) => onChange({ sort })}
          />
        </Row>
        <Row label="顺位">
          <Options
            options={orders}
            selected={filters.order ?? "desc"}
            onChange={(order) => onChange({ order })}
          />
        </Row>
        <Row label="播放次数">
          <Options
            options={plays}
            selected={filters.playRange ?? "all"}
            onChange={(playRange) => onChange({ playRange })}
          />
        </Row>
        <Row label="文件大小">
          <Options
            options={sizes}
            selected={filters.sizeRange ?? "all"}
            onChange={(sizeRange) => onChange({ sizeRange })}
          />
        </Row>
        <Row label="时间范围">
          <Options
            options={times}
            selected={filters.timeRange ?? "all"}
            onChange={(timeRange) =>
              onChange({
                timeRange,
                fromYear:
                  timeRange === "year_range"
                    ? (filters.fromYear ?? Number(years.at(-1)))
                    : undefined,
                toYear:
                  timeRange === "year_range"
                    ? (filters.toYear ?? Number(years[0]))
                    : undefined,
              })
            }
          />
          {filters.timeRange === "year_range" && (
            <span className="f-years">
              <Select
                value={String(filters.fromYear ?? years.at(-1))}
                onValueChange={(value) => onChange({ fromYear: Number(value) })}
              >
                <SelectTrigger aria-label="起始年份" />
                <SelectContent>
                  {years.map((year) => (
                    <SelectItem key={year} value={year}>
                      {year} 年
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <span className="f-years-sep">至</span>
              <Select
                value={String(filters.toYear ?? years[0])}
                onValueChange={(value) => onChange({ toYear: Number(value) })}
              >
                <SelectTrigger aria-label="结束年份" />
                <SelectContent>
                  {years.map((year) => (
                    <SelectItem key={year} value={year}>
                      {year} 年
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </span>
          )}
        </Row>
        <Row label="标签模式">
          <Options
            options={tagModes}
            selected={filters.tagMode ?? "any"}
            onChange={(tagMode) => onChange({ tagMode })}
          />
        </Row>
        <Row label="标签">
          {pool.map((name) => (
            <button
              type="button"
              key={name}
              className={`pill pill-tag${selected.includes(name) ? " active" : ""}`}
              aria-pressed={selected.includes(name)}
              onClick={() => toggle(name)}
            >
              {name}
              <span
                className="tag-x"
                title="移除该标签筛选"
                onClick={(event) => {
                  event.stopPropagation();
                  onChange({
                    tags: selected.filter((value) => value !== name),
                  });
                }}
              >
                ×
              </span>
            </button>
          ))}
          {inputOpen ? (
            <input
              className="tag-input"
              type="text"
              placeholder="新标签，回车添加"
              maxLength={12}
              autoFocus
              onKeyDown={(event) => {
                if (event.nativeEvent.isComposing) return;
                if (event.key === "Enter") {
                  event.preventDefault();
                  add(event.currentTarget.value);
                }
                if (event.key === "Escape") {
                  event.currentTarget.value = "";
                  setInputOpen(false);
                }
              }}
              onBlur={(event) => add(event.currentTarget.value)}
            />
          ) : (
            <button
              type="button"
              className="pill pill-add"
              onClick={() => setInputOpen(true)}
            >
              + 添加
            </button>
          )}
          {tagActions}
        </Row>
      </div>
    </div>
  );
}
