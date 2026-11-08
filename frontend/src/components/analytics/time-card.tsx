"use client";

import { STEP_LABELS, STEPS, type Analytics } from "@/lib/analytics";
import { formatHours } from "@/lib/reports";
import { cn } from "@/lib/utils";
import { DrillNumber } from "@/components/analytics/drill";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

// Where the time goes: the median hours delivered projects spent in each step, and how long
// open projects have been sitting in the step they're in now. The longest bar is the
// bottleneck.

export function TimeCard({ d }: { d: Analytics }) {
  const steps = STEPS.filter((s) => s !== "delivered").map((s) => ({ step: s, row: d.step_times.find((r) => r.step === s) }));
  const max = Math.max(1, ...steps.map((s) => s.row?.median_h ?? 0));
  const slowest = steps.reduce<(typeof steps)[number] | null>((a, s) => ((s.row?.median_h ?? 0) > (a?.row?.median_h ?? 0) ? s : a), null);
  const l = d.lateness;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Where the time goes</CardTitle>
        <CardDescription>
          Median time delivered projects spent in each step before delivery
          {slowest?.row ? `. The slowest step is ${STEP_LABELS[slowest.step].toLowerCase()}.` : "."}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        <ul className="space-y-2.5">
          {steps.map(({ step, row }) => (
            <li key={step} className="grid grid-cols-[minmax(8rem,10rem)_1fr] items-center gap-3 text-sm">
              <span className="truncate text-muted-foreground">{STEP_LABELS[step]}</span>
              <div className="flex h-6 items-center gap-2" title={row ? `average ${formatHours(row.avg_h, 1)}, longest ${formatHours(row.max_h, 1)}, ${row.projects} projects` : undefined}>
                <div
                  className={cn("h-3.5 rounded-r-[4px]", slowest?.step === step ? "bg-flow-out" : "bg-flow-in")}
                  style={{ width: `${((row?.median_h ?? 0) / max) * 75}%`, minWidth: row?.median_h ? 4 : 0 }}
                />
                <span className="font-medium whitespace-nowrap tabular-nums">{row ? formatHours(row.median_h, row.projects) : "—"}</span>
                {row && <span className="text-xs whitespace-nowrap text-muted-foreground">avg {formatHours(row.avg_h, row.projects)}</span>}
              </div>
            </li>
          ))}
        </ul>

        <div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
          <div>
            <div className="mb-2 text-xs font-medium text-muted-foreground">Waiting right now</div>
            <ul className="divide-y text-sm">
              {STEPS.map((s) => {
                const r = d.stuck_now.find((x) => x.step === s);
                if (!r) return null;
                return (
                  <li key={s} className="flex items-baseline justify-between gap-3 py-1.5">
                    <span className="text-muted-foreground">{STEP_LABELS[s]}</span>
                    <span className="text-right">
                      <DrillNumber metric={s === "delivered" ? "awaiting_client" : s} title={`${STEP_LABELS[s]} now`} className="font-medium">
                        {r.projects}
                      </DrillNumber>
                      <span className="ml-2 text-xs text-muted-foreground">
                        avg {formatHours(r.avg_h, 1)}, longest {formatHours(r.max_h, 1)}
                      </span>
                    </span>
                  </li>
                );
              })}
              {d.stuck_now.length === 0 && <li className="py-1.5 text-muted-foreground">Nothing open.</li>}
            </ul>
          </div>
          <div>
            <div className="mb-2 text-xs font-medium text-muted-foreground">Late deliveries in the period</div>
            {l.late === 0 ? (
              <p className="text-sm text-muted-foreground">None. Everything with a due date was delivered on time.</p>
            ) : (
              <dl className="grid grid-cols-3 gap-3">
                <div>
                  <dt className="text-xs text-muted-foreground">Late</dt>
                  <dd className="text-2xl font-semibold text-red-600">
                    <DrillNumber metric="late" title="Delivered late">
                      {l.late}
                    </DrillNumber>
                  </dd>
                </div>
                <div>
                  <dt className="text-xs text-muted-foreground">Typically by</dt>
                  <dd className="text-2xl font-semibold tabular-nums">{formatHours(l.median_h, l.late)}</dd>
                </div>
                <div>
                  <dt className="text-xs text-muted-foreground">Worst</dt>
                  <dd className="text-2xl font-semibold tabular-nums">{formatHours(l.max_h, l.late)}</dd>
                </div>
              </dl>
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
