"use client";

import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react";
import { clock, DAY_LABELS, hours, monthLabel, shiftMonth, weekday, type PersonSheet, type Sheet } from "@/lib/hr";
import { DAY_TONE, DayLegend } from "@/components/hr/hr-badges";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

const HEAD = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

export function MonthNav({ month, onChange }: { month: string; onChange: (m: string) => void }) {
  return (
    <div className="flex items-center gap-1">
      <Button variant="outline" size="icon-sm" aria-label="Previous month" onClick={() => onChange(shiftMonth(month, -1))}>
        <ChevronLeftIcon />
      </Button>
      <span className="min-w-28 text-center text-sm font-medium">{monthLabel(month)}</span>
      <Button variant="outline" size="icon-sm" aria-label="Next month" onClick={() => onChange(shiftMonth(month, 1))}>
        <ChevronRightIcon />
      </Button>
    </div>
  );
}

/** Month totals as small tiles; wraps to two columns on narrow screens. */
export function SummaryLine({ person }: { person: PersonSheet }) {
  const s = person.summary;
  const items: [string, string | number, string][] = [
    ["Present", (s.present ?? 0) + (s.late ?? 0), "text-emerald-700 dark:text-emerald-400"],
    ["Late", s.late ?? 0, "text-amber-700 dark:text-amber-400"],
    ["Absent", s.absent ?? 0, "text-red-700 dark:text-red-400"],
    ["Leave", (s.leave ?? 0) + (s.half_day_leave ?? 0) * 0.5, "text-sky-700 dark:text-sky-400"],
    ["Worked", hours(person.worked_minutes), ""],
  ];
  return (
    <dl className="grid grid-cols-2 gap-2 min-[480px]:grid-cols-5">
      {items.map(([label, n, tone], i) => (
        <div key={label} className={cn("rounded-lg border px-3 py-2", i === items.length - 1 && "col-span-2 min-[480px]:col-span-1")}>
          <dt className="text-xs text-muted-foreground">{label}</dt>
          <dd className={cn("text-lg font-semibold tabular-nums", tone)}>{n}</dd>
        </div>
      ))}
    </dl>
  );
}

/** A classic month grid for one person. */
export function MonthCalendar({ sheet, person }: { sheet: Sheet; person: PersonSheet }) {
  const tz = sheet.schedule.timezone;
  const lead = person.days.length ? weekday(person.days[0]!.date) : 0;
  return (
    <div className="space-y-3">
      <div className="grid grid-cols-7 gap-0.5 text-center text-xs font-medium text-muted-foreground sm:gap-1">
        {HEAD.map((h) => (
          <div key={h} className="truncate">
            <span className="sm:hidden">{h[0]}</span>
            <span className="hidden sm:inline">{h}</span>
          </div>
        ))}
      </div>
      <div className="grid grid-cols-7 gap-0.5 sm:gap-1">
        {Array.from({ length: lead }, (_, i) => (
          <div key={`pad${i}`} />
        ))}
        {person.days.map((d) => {
          const isToday = d.date === sheet.today;
          const detail = [
            DAY_LABELS[d.status],
            d.leave_type && `${d.leave_type} leave`,
            d.holiday,
            d.check_in && `In ${clock(d.check_in, tz)}`,
            d.check_out && `Out ${clock(d.check_out, tz)}${d.left_early ? " (early)" : ""}`,
            d.worked_minutes > 0 && hours(d.worked_minutes),
            d.source === "correction" && "Corrected",
          ]
            .filter(Boolean)
            .join(" · ");
          return (
            <Tooltip key={d.date}>
              <TooltipTrigger asChild>
                <div
                  tabIndex={0}
                  className={cn(
                    "flex h-10 min-w-0 flex-col justify-between rounded-md p-1 text-left text-xs sm:h-16 sm:p-1.5",
                    DAY_TONE[d.status],
                    isToday && "ring-2 ring-primary",
                  )}
                >
                  <span className="font-semibold tabular-nums">{Number(d.date.slice(8))}</span>
                  <span className="hidden truncate sm:block">
                    {d.check_in ? clock(d.check_in, tz) : d.status === "upcoming" || d.status === "weekend" || d.status === "untracked" ? "" : DAY_LABELS[d.status]}
                  </span>
                </div>
              </TooltipTrigger>
              <TooltipContent>{detail}</TooltipContent>
            </Tooltip>
          );
        })}
      </div>
      <DayLegend />
    </div>
  );
}

export { monthLabel };
