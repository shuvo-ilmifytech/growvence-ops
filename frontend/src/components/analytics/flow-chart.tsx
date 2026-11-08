"use client";

import { useState } from "react";
import { cn } from "@/lib/utils";
import type { Analytics, Bucket } from "@/lib/analytics";

// Incoming against delivered per day, week or month, as grouped bars on one count axis.
// Hand-built like the finance charts: recessive grid, thin bars with 4px data ends, and a
// tooltip on hover and keyboard focus. The totals above the chart carry the same numbers.

type Point = Analytics["series"][number];

function ticks(max: number) {
  const raw = Math.max(1, max) / 4;
  const step = [1, 2, 5, 10, 20, 25, 50, 100, 200, 250, 500, 1000].find((s) => s >= raw) ?? 1000;
  const hi = Math.max(step, Math.ceil(max / step) * step);
  const out: number[] = [];
  for (let v = 0; v <= hi; v += step) out.push(v);
  return { hi, out };
}

const day = (s: string) => new Date(`${s}T00:00:00`);

export function bucketLabel(start: string, bucket: Bucket, long = false) {
  const d = day(start);
  if (bucket === "month") return d.toLocaleDateString(undefined, { month: long ? "long" : "short", year: long ? "numeric" : undefined });
  const short = d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
  if (bucket === "week") return long ? `Week of ${short}` : short;
  return long ? d.toLocaleDateString(undefined, { weekday: "short", month: "short", day: "numeric" }) : short;
}

export function FlowChart({ series, bucket }: { series: Point[]; bucket: Bucket }) {
  const [hover, setHover] = useState<string | null>(null);
  const { hi, out } = ticks(Math.max(0, ...series.flatMap((p) => [p.incoming, p.delivered])));
  const pct = (v: number) => (v / hi) * 100;
  // Label about eight buckets so dates never collide, whatever the range.
  const every = Math.max(1, Math.ceil(series.length / 8));
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
        {[
          { label: "Incoming", className: "bg-flow-in" },
          { label: "Delivered", className: "bg-flow-out" },
        ].map((i) => (
          <span key={i.label} className="inline-flex items-center gap-1.5">
            <span className={cn("size-2.5 rounded-[2px]", i.className)} aria-hidden />
            {i.label}
          </span>
        ))}
      </div>
      <div className="flex gap-2">
        <div className="relative h-56 w-8 shrink-0 text-right text-[11px] tabular-nums text-muted-foreground" aria-hidden>
          {out.map((v) => (
            <span key={v} className="absolute right-0 -translate-y-1/2" style={{ top: `${100 - pct(v)}%` }}>
              {v}
            </span>
          ))}
        </div>
        <div className="min-w-0 flex-1">
          <div className="relative h-56">
            {out.map((v) => (
              <div
                key={v}
                className={cn("absolute inset-x-0 border-t", v === 0 ? "border-foreground/30" : "border-dashed border-border/60")}
                style={{ top: `${100 - pct(v)}%` }}
                aria-hidden
              />
            ))}
            <div className="absolute inset-0 flex">
              {series.map((p, i) => {
                const on = hover === p.start;
                return (
                  <div
                    key={p.start}
                    tabIndex={0}
                    aria-label={`${bucketLabel(p.start, bucket, true)}: ${p.incoming} incoming, ${p.delivered} delivered`}
                    className="relative flex-1 rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
                    onPointerEnter={() => setHover(p.start)}
                    onPointerLeave={() => setHover((h) => (h === p.start ? null : h))}
                    onFocus={() => setHover(p.start)}
                    onBlur={() => setHover((h) => (h === p.start ? null : h))}
                  >
                    {on && <div className="absolute inset-0 rounded-sm bg-foreground/[0.04]" aria-hidden />}
                    <div className="absolute inset-x-0 bottom-0 flex h-full items-end justify-center gap-0.5 px-[12%]">
                      {[
                        { v: p.incoming, c: "bg-flow-in" },
                        { v: p.delivered, c: "bg-flow-out" },
                      ].map((b) => (
                        <div
                          key={b.c}
                          className={cn("w-full max-w-4 rounded-t-[4px] transition-[filter]", b.c, on && "brightness-110")}
                          style={{ height: `${pct(b.v)}%`, minHeight: b.v ? 2 : 0 }}
                        />
                      ))}
                    </div>
                    {on && (
                      <div
                        role="tooltip"
                        className={cn(
                          "pointer-events-none absolute top-1 z-10 w-max min-w-36 rounded-md border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-md",
                          i < 2 ? "left-0" : i > series.length - 3 ? "right-0" : "left-1/2 -translate-x-1/2",
                        )}
                      >
                        <div className="mb-1 text-muted-foreground">{bucketLabel(p.start, bucket, true)}</div>
                        {[
                          { label: "Incoming", v: p.incoming, key: "bg-flow-in" },
                          { label: "Delivered", v: p.delivered, key: "bg-flow-out" },
                          { label: "Completed", v: p.completed },
                          { label: "Cancelled", v: p.cancelled },
                        ].map((r) => (
                          <div key={r.label} className="flex items-center justify-between gap-4">
                            <span className="inline-flex items-center gap-1.5 text-muted-foreground">
                              {r.key && <span className={cn("h-0.5 w-3 rounded-full", r.key)} aria-hidden />}
                              {r.label}
                            </span>
                            <span className="font-semibold tabular-nums text-foreground">{r.v}</span>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
          <div className="mt-1.5 flex text-[11px] text-muted-foreground" aria-hidden>
            {series.map((p, i) => (
              <span key={p.start} className="flex-1 overflow-visible whitespace-nowrap text-center">
                {i % every === 0 ? bucketLabel(p.start, bucket) : ""}
              </span>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
