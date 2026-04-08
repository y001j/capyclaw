"use client";

import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from "recharts";
import type { UsageReport } from "@/lib/api";

interface UsageChartProps {
  report: UsageReport;
}

export function UsageChart({ report }: UsageChartProps) {
  const data = (report.models ?? []).map((m) => ({
    model: m.model.length > 20 ? m.model.slice(0, 20) + "..." : m.model,
    "Input Tokens": m.input_tokens,
    "Output Tokens": m.output_tokens,
    "Cost (USD)": Number(m.cost_usd.toFixed(4)),
    Sessions: m.session_count,
  }));

  return (
    <div className="space-y-6">
      {/* Summary cards */}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <div className="rounded-lg border border-border p-4">
          <div className="text-sm text-muted-foreground">Total Sessions</div>
          <div className="mt-1 text-2xl font-bold">
            {report.totals.session_count}
          </div>
        </div>
        <div className="rounded-lg border border-border p-4">
          <div className="text-sm text-muted-foreground">Input Tokens</div>
          <div className="mt-1 text-2xl font-bold">
            {report.totals.input_tokens.toLocaleString()}
          </div>
        </div>
        <div className="rounded-lg border border-border p-4">
          <div className="text-sm text-muted-foreground">Output Tokens</div>
          <div className="mt-1 text-2xl font-bold">
            {report.totals.output_tokens.toLocaleString()}
          </div>
        </div>
        <div className="rounded-lg border border-border p-4">
          <div className="text-sm text-muted-foreground">Total Cost</div>
          <div className="mt-1 text-2xl font-bold">
            ${report.totals.cost_usd.toFixed(2)}
          </div>
        </div>
      </div>

      {/* Token chart */}
      {data.length > 0 && (
        <div className="rounded-lg border border-border p-4">
          <h3 className="mb-4 text-sm font-semibold">Tokens by Model</h3>
          <ResponsiveContainer width="100%" height={300}>
            <BarChart data={data}>
              <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
              <XAxis dataKey="model" className="text-xs" />
              <YAxis className="text-xs" />
              <Tooltip />
              <Legend />
              <Bar dataKey="Input Tokens" fill="hsl(221.2, 83.2%, 53.3%)" />
              <Bar dataKey="Output Tokens" fill="hsl(210, 40%, 60%)" />
            </BarChart>
          </ResponsiveContainer>
        </div>
      )}
    </div>
  );
}
