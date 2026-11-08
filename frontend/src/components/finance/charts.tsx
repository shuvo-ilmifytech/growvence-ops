"use client";

import { useState } from "react";
import { cn } from "@/lib/utils";
import { formatBdt, formatBdtShort, formatMonth, type FinanceMonth, type FinanceSummary } from "@/lib/finance";

// Hand-built charts (no chart library in the app). One axis per chart, a recessive grid,
// thin bars with 4px rounded data ends, and a tooltip on hover and keyboard focus. Every
// value is also in the monthly table, so nothing depends on hovering.

/** About four round gridline steps covering [min, max] (min ≤ 0 ≤ max). */
function ticks(min: number, max: number) {
  const span = max - min || 1;
  const raw = span / 4;
  const mag = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => s >= raw)!;
  const lo = Math.floor(min / step) * step;
  const hi = Math.ceil(max / step) * step || step;
  const out: number[] = [];
  for (let v = lo; v <= hi + step / 2; v += step) out.push(Math.round(v));
  return { lo, hi, out };
}

function Legend({ items }: { items: { label: string; className: string }[] }) {
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
      {items.map((i) => (
        <span key={i.label} className="inline-flex items-center gap-1.5">
          <span className={cn("size-2.5 rounded-[2px]", i.className)} aria-hidden />
          {i.label}
        </span>
      ))}
    </div>
  );
}

type Row = { label: string; value: number; key?: string };

function Tooltip({ title, rows, align }: { title: string; rows: Row[]; align: "left" | "right" | "center" }) {
  return (
    <div
      role="tooltip"
      className={cn(
        "pointer-events-none absolute top-1 z-10 w-max min-w-40 rounded-md border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-md",
        align === "left" && "left-0",
        align === "right" && "right-0",
        align === "center" && "left-1/2 -translate-x-1/2",
      )}
    >
      <div className="mb-1 text-muted-foreground">{title}</div>
      {rows.map((r) => (
        <div key={r.label} className="flex items-center justify-between gap-4">
          <span className="inline-flex items-center gap-1.5 text-muted-foreground">
            {r.key && <span className={cn("h-0.5 w-3 rounded-full", r.key)} aria-hidden />}
            {r.label}
          </span>
          <span className="font-semibold tabular-nums text-foreground">{formatBdt(r.value)}</span>
        </div>
      ))}
    </div>
  );
}

function Plot({
  months,
  current,
  lo,
  hi,
  gridValues,
  children,
  tooltip,
  label,
}: {
  months: FinanceMonth[];
  current: string;
  lo: number;
  hi: number;
  gridValues: number[];
  children: (m: FinanceMonth, hovered: boolean) => React.ReactNode;
  tooltip: (m: FinanceMonth) => { title: string; rows: Row[] };
  label: (m: FinanceMonth) => string;
}) {
  const [hover, setHover] = useState<string | null>(null);
  const pct = (v: number) => ((v - lo) / (hi - lo)) * 100;
  return (
    <div className="flex gap-2">
      <div className="relative h-56 w-14 shrink-0 text-right text-[11px] tabular-nums text-muted-foreground" aria-hidden>
        {gridValues.map((v) => (
          <span key={v} className="absolute right-0 -translate-y-1/2" style={{ top: `${100 - pct(v)}%` }}>
            {formatBdtShort(v)}
          </span>
        ))}
      </div>
      <div className="min-w-0 flex-1">
        <div className="relative h-56">
          {gridValues.map((v) => (
            <div
              key={v}
              className={cn("absolute inset-x-0 border-t", v === 0 ? "border-foreground/30" : "border-border/60 border-dashed")}
              style={{ top: `${100 - pct(v)}%` }}
              aria-hidden
            />
          ))}
          <div className="absolute inset-0 flex">
            {months.map((m, i) => {
              const t = tooltip(m);
              return (
                <div
                  key={m.month}
                  tabIndex={0}
                  aria-label={label(m)}
                  className="relative flex-1 rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
                  onPointerEnter={() => setHover(m.month)}
                  onPointerLeave={() => setHover((h) => (h === m.month ? null : h))}
                  onFocus={() => setHover(m.month)}
                  onBlur={() => setHover((h) => (h === m.month ? null : h))}
                >
                  {hover === m.month && <div className="absolute inset-0 rounded-sm bg-foreground/[0.04]" aria-hidden />}
                  {children(m, hover === m.month)}
                  {hover === m.month && (
                    <Tooltip
                      title={t.title + (m.month === current ? " (so far)" : "")}
                      rows={t.rows}
                      align={i < 2 ? "left" : i > months.length - 3 ? "right" : "center"}
                    />
                  )}
                </div>
              );
            })}
          </div>
        </div>
        <div className="mt-1.5 flex text-[11px] text-muted-foreground" aria-hidden>
          {months.map((m, i) => (
            <span
              key={m.month}
              className={cn("flex-1 text-center", months.length > 8 && i % 2 === 1 && "max-sm:invisible", m.month === current && "font-medium text-foreground")}
            >
              {formatMonth(m.month, true)}
              {m.month.endsWith("-01") && <span className="block text-[10px]">{m.month.slice(0, 4)}</span>}
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}

/** Income against expense per month, as grouped bars on one BDT axis. */
export function IncomeExpenseChart({ summary }: { summary: FinanceSummary }) {
  const months = summary.months;
  const max = Math.max(0, ...months.flatMap((m) => [Number(m.total_income), Number(m.total_expense)]));
  const { lo, hi, out } = ticks(0, max);
  const h = (v: number) => `${(Math.max(0, v) / (hi - lo)) * 100}%`;
  return (
    <div className="space-y-3">
      <Legend items={[{ label: "Income", className: "bg-fin-income" }, { label: "Expense", className: "bg-fin-expense" }]} />
      <Plot
        months={months}
        current={summary.current_month}
        lo={lo}
        hi={hi}
        gridValues={out}
        label={(m) => `${formatMonth(m.month)}: income ${formatBdt(m.total_income)}, expense ${formatBdt(m.total_expense)}`}
        tooltip={(m) => ({
          title: formatMonth(m.month),
          rows: [
            { label: "Income", value: Number(m.total_income), key: "bg-fin-income" },
            { label: "Expense", value: Number(m.total_expense), key: "bg-fin-expense" },
            { label: "Profit", value: Number(m.profit) },
          ],
        })}
      >
        {(m, hovered) => (
          <div className="absolute inset-x-0 bottom-0 flex h-full items-end justify-center gap-0.5 px-[12%]">
            <div className={cn("w-full max-w-5 rounded-t-[4px] bg-fin-income transition-[filter]", hovered && "brightness-110")} style={{ height: h(Number(m.total_income)) }} />
            <div className={cn("w-full max-w-5 rounded-t-[4px] bg-fin-expense transition-[filter]", hovered && "brightness-110")} style={{ height: h(Number(m.total_expense)) }} />
          </div>
        )}
      </Plot>
    </div>
  );
}

/** Profit per month: bars up from zero for a profit, down for a loss. */
export function ProfitChart({ summary }: { summary: FinanceSummary }) {
  const months = summary.months;
  const values = months.map((m) => Number(m.profit));
  const { lo, hi, out } = ticks(Math.min(0, ...values), Math.max(0, ...values));
  const zero = ((0 - lo) / (hi - lo)) * 100;
  return (
    <div className="space-y-3">
      <Legend items={[{ label: "Profit", className: "bg-fin-income" }, { label: "Loss", className: "bg-fin-loss" }]} />
      <Plot
        months={months}
        current={summary.current_month}
        lo={lo}
        hi={hi}
        gridValues={out}
        label={(m) => `${formatMonth(m.month)}: ${Number(m.profit) < 0 ? "loss" : "profit"} ${formatBdt(m.profit)}`}
        tooltip={(m) => ({ title: formatMonth(m.month), rows: [{ label: Number(m.profit) < 0 ? "Loss" : "Profit", value: Number(m.profit) }] })}
      >
        {(m, hovered) => {
          const v = Number(m.profit);
          const size = `${(Math.abs(v) / (hi - lo)) * 100}%`;
          return (
            <div className="absolute inset-x-0 flex justify-center px-[22%]" style={v >= 0 ? { bottom: `${zero}%`, height: size } : { top: `${100 - zero}%`, height: size }}>
              <div
                className={cn(
                  "h-full w-full max-w-7 transition-[filter]",
                  v >= 0 ? "rounded-t-[4px] bg-fin-income" : "rounded-b-[4px] bg-fin-loss",
                  hovered && "brightness-110",
                )}
              />
            </div>
          );
        }}
      </Plot>
    </div>
  );
}

/** Ledger totals per category, one side at a time, largest first. */
export function CategoryBars({ rows, kind }: { rows: FinanceSummary["categories"]; kind: "income" | "expense" }) {
  const list = rows.filter((r) => r.kind === kind);
  const max = Math.max(1, ...list.map((r) => Number(r.total)));
  if (list.length === 0) return <p className="text-sm text-muted-foreground">Nothing recorded in this period.</p>;
  return (
    <ul className="space-y-3">
      {list.map((r) => (
        <li key={r.id} className="grid grid-cols-[minmax(0,1fr)_auto] items-baseline gap-x-3 gap-y-1">
          <span className="truncate text-sm">{r.name}</span>
          <span className="text-sm font-medium tabular-nums">{formatBdt(r.total)}</span>
          <div className="col-span-2 h-2 rounded-full bg-muted">
            <div
              className={cn("h-2 rounded-full", kind === "income" ? "bg-fin-income" : "bg-fin-expense")}
              style={{ width: `${(Number(r.total) / max) * 100}%` }}
              title={`${r.entries} ${r.entries === 1 ? "entry" : "entries"}`}
            />
          </div>
        </li>
      ))}
    </ul>
  );
}
