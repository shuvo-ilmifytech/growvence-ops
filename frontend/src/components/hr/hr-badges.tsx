import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { DAY_LABELS, LEAVE_STATUS_LABELS, type DayStatus, type LeaveStatus } from "@/lib/hr";

const LEAVE_TONE: Record<LeaveStatus, string> = {
  pending: "bg-amber-50 text-amber-800 ring-amber-600/25",
  awaiting_hr: "bg-orange-50 text-orange-800 ring-orange-600/25",
  approved: "bg-emerald-50 text-emerald-700 ring-emerald-600/20",
  rejected: "bg-red-50 text-red-700 ring-red-600/20",
  cancelled: "bg-zinc-50 text-zinc-500 ring-zinc-400/20 line-through",
};

export function LeaveStatusBadge({ status }: { status: LeaveStatus }) {
  return (
    <Badge variant="outline" className={cn("border-0 ring-1 ring-inset", LEAVE_TONE[status])}>
      {LEAVE_STATUS_LABELS[status]}
    </Badge>
  );
}

/** Background + text for a day cell; shared by the personal calendar and the team sheet. */
export const DAY_TONE: Record<DayStatus, string> = {
  present: "bg-emerald-100 text-emerald-800 dark:bg-emerald-500/20 dark:text-emerald-300",
  late: "bg-amber-100 text-amber-800 dark:bg-amber-500/20 dark:text-amber-300",
  absent: "bg-red-100 text-red-700 dark:bg-red-500/20 dark:text-red-300",
  leave: "bg-sky-100 text-sky-800 dark:bg-sky-500/20 dark:text-sky-300",
  half_day_leave: "bg-sky-50 text-sky-700 dark:bg-sky-500/10 dark:text-sky-300",
  holiday: "bg-violet-100 text-violet-800 dark:bg-violet-500/20 dark:text-violet-300",
  weekend: "bg-muted text-muted-foreground",
  upcoming: "bg-background text-muted-foreground",
  untracked: "bg-background text-muted-foreground/60",
};

/** One-letter code for dense grids. */
export const DAY_CODE: Record<DayStatus, string> = {
  present: "P",
  late: "L",
  absent: "A",
  leave: "LV",
  half_day_leave: "½",
  holiday: "H",
  weekend: "",
  upcoming: "",
  untracked: "",
};

export const LEGEND: DayStatus[] = ["present", "late", "absent", "leave", "half_day_leave", "holiday", "weekend"];

export function DayLegend() {
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1.5 text-xs text-muted-foreground">
      {LEGEND.map((s) => (
        <span key={s} className="inline-flex items-center gap-1.5">
          <span className={cn("inline-flex h-4 min-w-4 items-center justify-center rounded px-0.5 text-[10px] font-semibold", DAY_TONE[s])}>
            {DAY_CODE[s]}
          </span>
          {DAY_LABELS[s]}
        </span>
      ))}
    </div>
  );
}
