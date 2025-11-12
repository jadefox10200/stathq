import React from "react";
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  LabelList,
} from "recharts";

/*
ChartLine - single-line with colored point markers + colored labels

Props:
- data: [{ week: "YYYY-MM-DD", value: number, ... }, ...] (sorted ascending)
- xKey: string (default "week")
- yKey: string (default "value")
- height: number (px) optional, default 360
- strokeWidth: number optional, default 2
- valueFormatter: (number) => string optional for label formatting
- labelInterval: number optional; show a label every N points (default 1 = every point)
- pointRadius: number optional; radius for point marker (default 3)

Behavior:
- Draws a single Line for the whole dataset (prevents duplicated axes/ticks).
- Each dot is colored: black if next point > this point, red if next point <= this point.
- Labels for each point are rendered above the point (black).
- Line segments are colored: black if the next point > current point (value went up), red otherwise.
- Added margins to prevent labels from being cut off (especially on the right and top).
*/
export default function ChartLine({
  data = [],
  xKey = "week",
  yKey = "value",
  height = 360,
  strokeWidth = 2,
  valueFormatter = (v) => {
    if (v === null || v === undefined) return "";
    if (Number.isInteger(v)) return String(v);
    return Number(v).toFixed(2).replace(/\.00$/, "");
  },
  labelInterval = 1,
  pointRadius = 3,
  reversed = false,
}) {
  if (!Array.isArray(data) || data.length === 0) return null;

  // Precompute colors per point based on slope to next point.
  // Black if next > current, red otherwise.
  const pointColors = data.map((d, i) => {
    if (i < data.length - 1) {
      return Number(data[i + 1][yKey]) > Number(d[yKey])
        ? "#000000"
        : "#d9534f";
    }
    // last point: based on previous slope
    if (data.length >= 2) {
      return Number(d[yKey]) > Number(data[data.length - 2][yKey])
        ? "#000000"
        : "#d9534f";
    }
    return "#000000";
  });

  // Precompute colors per segment based on whether the next point > current point.
  const segmentColors = data.slice(0, -1).map((d, i) => {
    if (reversed) {
      return Number(data[i + 1][yKey]) < Number(d[yKey])
        ? "#000000"
        : "#d9534f";
    }
    return Number(data[i + 1][yKey]) > Number(d[yKey]) ? "#000000" : "#d9534f";
  });

  // Custom dot renderer: use pointColors by index (recharts provides payload and index)
  const CustomDot = ({ cx, cy, payload, index }) => {
    if (cx === undefined || cy === undefined) return null;
    const color = pointColors[index] || "#000000";
    return (
      <g>
        <circle
          cx={cx}
          cy={cy}
          r={pointRadius}
          fill={color}
          stroke="#fff"
          strokeWidth={1}
        />
      </g>
    );
  };

  // Custom label renderer: labels are black
  const renderCustomizedLabel = (props) => {
    const { x, y, value, index } = props;
    const txt = valueFormatter(Number(value));
    if (x === undefined || y === undefined) return null;
    return (
      <text x={x} y={y - 8} fill="#000000" fontSize="11" textAnchor="middle">
        {txt}
      </text>
    );
  };

  // If single-point, render a small chart with the single dot and label
  if (data.length === 1) {
    const only = data;
    return (
      <div style={{ width: "100%", height }}>
        <ResponsiveContainer>
          <LineChart
            data={only}
            margin={{ top: 20, right: 30, left: 20, bottom: 5 }}
          >
            <CartesianGrid strokeDasharray="3 3" />
            <XAxis
              dataKey={xKey}
              type="category"
              allowDuplicatedCategory={false}
            />
            <YAxis reversed={reversed} />
            {/* <Tooltip formatter={(v) => valueFormatter(Number(v))} /> */}
            <Line
              type="linear"
              dataKey={yKey}
              stroke="#000000"
              strokeWidth={strokeWidth}
              dot={<CustomDot />}
              isAnimationActive={false}
            >
              <LabelList
                dataKey={yKey}
                content={renderCustomizedLabel}
                interval={0}
              />
            </Line>
          </LineChart>
        </ResponsiveContainer>
      </div>
    );
  }

  return (
    <div style={{ width: "100%", height }}>
      <ResponsiveContainer>
        <LineChart
          data={data}
          margin={{ top: 20, right: 30, left: 20, bottom: 5 }}
        >
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis
            dataKey={xKey}
            type="category"
            allowDuplicatedCategory={false}
            interval="preserveStartEnd"
          />
          <YAxis reversed={reversed} />
          {/* <Tooltip formatter={(v) => valueFormatter(Number(v))} /> */}
          {/* Render each segment as a separate Line */}
          {data.slice(0, -1).map((_, i) => (
            <Line
              key={i}
              data={[data[i], data[i + 1]]}
              dataKey={yKey}
              stroke={segmentColors[i]}
              strokeWidth={strokeWidth}
              dot={false}
              isAnimationActive={false}
            />
          ))}
          {/* Invisible Line for dots and labels */}
          <Line
            data={data}
            dataKey={yKey}
            stroke="none"
            strokeWidth={strokeWidth}
            dot={<CustomDot />}
            isAnimationActive={false}
          >
            <LabelList
              content={renderCustomizedLabel}
              dataKey={yKey}
              interval={labelInterval <= 0 ? 0 : labelInterval - 1}
            />
          </Line>
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
