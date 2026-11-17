import {
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import type { Schema } from "../data/client";

const tipStyle = {
  contentStyle: {
    background: "var(--pop-chip-hover-bg)",
    border: "none",
    borderRadius: 6,
    fontSize: 11,
    color: "var(--text-main)",
    padding: "4px 9px",
  },
  labelStyle: { color: "var(--text-sub)", marginRight: 2 },
  itemStyle: { padding: 0 },
  cursor: {
    stroke: "var(--text-sub)",
    strokeDasharray: "4 4",
    strokeOpacity: 0.4,
  },
} as const;

// Existing TrendChart presentation; generated TrendBucket field mapping is the only adapter.
export function LegacyTrendChart({
  items,
}: {
  items: Schema<"TrendBucket">[];
}) {
  const data = items.map((item) => ({
    label: item.label,
    浏览: item.views,
    播放: item.plays,
  }));
  return (
    <>
      <div className="trend-legend">
        <span>
          <i style={{ background: "var(--qm-primary)" }} />
          浏览
        </span>
        <span>
          <i style={{ background: "var(--trend-line-sub)" }} />
          播放
        </span>
      </div>
      <ResponsiveContainer width="100%" height={210}>
        <LineChart
          data={data}
          margin={{ top: 6, right: 8, left: 8, bottom: 0 }}
          accessibilityLayer
        >
          <XAxis
            dataKey="label"
            tick={{ fontSize: 10, fill: "var(--text-sub)" }}
            tickLine={false}
            axisLine={false}
            interval="preserveStartEnd"
            minTickGap={40}
          />
          <YAxis hide />
          <Tooltip {...tipStyle} />
          <Line
            type="monotone"
            dataKey="浏览"
            name="浏览"
            stroke="var(--qm-primary)"
            strokeWidth={2}
            dot={false}
            activeDot={{ r: 4, fill: "var(--qm-primary)" }}
            isAnimationActive={false}
          />
          <Line
            type="monotone"
            dataKey="播放"
            name="播放"
            stroke="var(--trend-line-sub)"
            strokeWidth={2}
            strokeDasharray="4 4"
            dot={false}
            activeDot={{ r: 4, fill: "var(--trend-line-sub)" }}
            isAnimationActive={false}
          />
        </LineChart>
      </ResponsiveContainer>
    </>
  );
}
