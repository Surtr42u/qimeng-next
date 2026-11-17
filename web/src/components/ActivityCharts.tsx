import { useRef, useState } from "react";
import type { Schema } from "../data/client";
import { duration } from "../lib/format";

export function TrendChart({
  items,
}: {
  items: readonly Schema<"TrendBucket">[];
}) {
  const [selected, setSelected] = useState(0);
  const maximum = Math.max(
    1,
    ...items.map((item) => Math.max(item.views, item.plays)),
  );
  const x = (index: number) =>
    items.length < 2 ? 300 : (index / (items.length - 1)) * 600;
  const y = (value: number) => 155 - (value / maximum) * 130;
  const line = (field: "views" | "plays") =>
    items
      .map(
        (item, index) =>
          `${index === 0 ? "M" : "L"}${x(index)},${y(item[field])}`,
      )
      .join(" ");
  const active = items[Math.min(selected, items.length - 1)];
  return (
    <div className="trend-chart">
      <svg
        viewBox="-6 0 612 172"
        role="img"
        aria-label="所选窗口浏览与播放趋势"
      >
        <path
          d={`${line("views")} L600,160 L0,160Z`}
          fill="var(--qm-accent-soft)"
        />
        <path
          d={line("views")}
          fill="none"
          stroke="var(--qm-accent)"
          strokeWidth="2.5"
        />
        <path
          d={line("plays")}
          fill="none"
          stroke="var(--qm-cyan)"
          strokeWidth="2"
        />
        {active && (
          <circle
            cx={x(Math.min(selected, items.length - 1))}
            cy={y(active.views)}
            r="4"
            fill="var(--qm-accent)"
          />
        )}
      </svg>
      <label className="trend-selector">
        查看日期
        <select
          value={selected}
          onChange={(event) => setSelected(Number(event.target.value))}
        >
          {items.map((item, index) => (
            <option key={item.start} value={index}>
              {item.label}
            </option>
          ))}
        </select>
      </label>
      {active && (
        <p className="chart-readout" aria-live="polite">
          {active.label} · 浏览 {active.views} · 播放 {active.plays} · 停留{" "}
          {duration(active.seconds * 1000)}
        </p>
      )}
      <p className="chart-legend">
        <span>
          <i className="legend-view" />
          浏览
        </span>
        <span>
          <i className="legend-play" />
          播放
        </span>
      </p>
    </div>
  );
}

export function HourChart({
  hours,
}: {
  hours: readonly Schema<"ReviewHour">[];
}) {
  const [selected, setSelected] = useState(0);
  const root = useRef<HTMLDivElement>(null);
  const maximum = Math.max(1, ...hours.map((hour) => hour.seconds));
  const active = hours[selected];
  return (
    <div className="hour-chart">
      <div className="hour-labels" aria-hidden="true">
        <span>00</span>
        <span>06</span>
        <span>12</span>
        <span>18</span>
      </div>
      <div
        ref={root}
        className="hour-bars"
        role="group"
        aria-label="24小时浏览停留"
        onKeyDown={(event) => {
          const next =
            event.key === "ArrowRight"
              ? Math.min(hours.length - 1, selected + 1)
              : event.key === "ArrowLeft"
                ? Math.max(0, selected - 1)
                : event.key === "Home"
                  ? 0
                  : event.key === "End"
                    ? hours.length - 1
                    : -1;
          if (next < 0) return;
          event.preventDefault();
          setSelected(next);
          root.current
            ?.querySelector<HTMLButtonElement>(`[data-hour="${next}"]`)
            ?.focus();
        }}
      >
        {hours.map((hour, index) => (
          <button
            key={hour.hour}
            data-hour={index}
            tabIndex={selected === index ? 0 : -1}
            className={`hour-bar ${selected === index ? "hour-bar--selected" : ""}`}
            aria-label={`${hour.hour}时，停留${hour.seconds}秒，浏览${hour.views}次`}
            onFocus={() => setSelected(index)}
            onMouseEnter={() => setSelected(index)}
            onClick={() => setSelected(index)}
          >
            <span
              className="hour-stack"
              style={{ height: `${(hour.seconds / maximum) * 100}%` }}
            >
              <i
                className="hour-image"
                style={{ flexGrow: hour.imageSeconds }}
              />
              <i
                className="hour-video"
                style={{ flexGrow: hour.videoSeconds }}
              />
              <i
                className="hour-animated"
                style={{ flexGrow: hour.animatedSeconds }}
              />
            </span>
          </button>
        ))}
      </div>
      <p className="chart-readout" aria-live="polite">
        {active
          ? `${String(active.hour).padStart(2, "0")}:00—${String(active.hour + 1).padStart(2, "0")}:00 · 停留 ${duration(active.seconds * 1000)} · 浏览 ${active.views} 次`
          : "没有小时记录"}
      </p>
      <div className="chart-legend">
        <span>
          <i className="hour-image" />
          图片
        </span>
        <span>
          <i className="hour-video" />
          视频
        </span>
        <span>
          <i className="hour-animated" />
          动图
        </span>
      </div>
    </div>
  );
}
