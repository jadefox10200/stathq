import React from "react";
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  LabelList,
} from "recharts";

/*
StandardChart - basic Recharts LineChart with default styling and labels

Props:
- data: [{ xKey: value, yKey: number, ... }, ...] (sorted ascending)
- xKey: string (default "week")
- yKey: string (default "value")
- height: number (px) optional, default 360
- color: string optional, default "#8884d8" (Recharts default blue)
- valueFormatter: (number) => string optional for label formatting (default truncates decimals)

Behavior:
- Uses Recharts' default LineChart with basic features: grid, axes, tooltip, legend, and labels on points.
- Labels are shown by default above each point.
*/
export default function StandardChart({
  data = [],
  xKey = "week",
  yKey = "value",
  height = 360,
  color = "#8884d8",
  valueFormatter = (v) => {
    if (v === null || v === undefined) return "";
    if (Number.isInteger(v)) return String(v);
    return Number(v).toFixed(2).replace(/\.00$/, "");
  },
}) {
  if (!Array.isArray(data) || data.length === 0) {
    return <div>No data to display</div>;
  }

  // Custom label renderer for points
  const renderCustomizedLabel = (props) => {
    const { x, y, value } = props;
    const txt = valueFormatter(Number(value));
    if (x === undefined || y === undefined) return null;
    return (
      <text x={x} y={y - 8} fill="#000000" fontSize="11" textAnchor="middle">
        {txt}
      </text>
    );
  };

  return (
    <div style={{ width: "100%", height }}>
      <ResponsiveContainer>
        <LineChart data={data}>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey={xKey} />
          <YAxis />
          <Tooltip formatter={(v) => valueFormatter(Number(v))} />
          <Legend />
          <Line
            type="linear"
            dataKey={yKey}
            stroke={color}
            strokeWidth={2}
            activeDot={{ r: 6 }}
          >
            <LabelList
              content={renderCustomizedLabel}
              dataKey={yKey}
              interval={0} // Show label on every point
            />
          </Line>
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
