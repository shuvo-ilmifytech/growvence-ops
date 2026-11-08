import { AlertTriangleIcon, ArrowDownIcon, ArrowUpIcon, ChevronsUpIcon, ClockAlertIcon, MinusIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { PRIORITY_LABELS, STATUS_LABELS, STORE_ACCESS_LABELS, type HistoryStatus, type Priority, type StoreAccess } from "@/lib/types";

const STATUS_TONE: Record<HistoryStatus, string> = {
  new: "bg-sky-50 text-sky-700 ring-sky-600/20",
  in_progress: "bg-blue-50 text-blue-700 ring-blue-600/20",
  blocked: "bg-red-50 text-red-700 ring-red-600/20",
  ready_for_review: "bg-violet-50 text-violet-700 ring-violet-600/20",
  ready_to_deliver: "bg-amber-50 text-amber-800 ring-amber-600/25",
  delivered: "bg-teal-50 text-teal-700 ring-teal-600/20",
  completed: "bg-emerald-50 text-emerald-700 ring-emerald-600/20",
  cancelled: "bg-zinc-50 text-zinc-500 ring-zinc-400/20 line-through",
  draft: "bg-zinc-100 text-zinc-700 ring-zinc-500/15",
  submitted: "bg-sky-50 text-sky-700 ring-sky-600/20",
  needs_info: "bg-amber-50 text-amber-800 ring-amber-600/25",
  assigned: "bg-indigo-50 text-indigo-700 ring-indigo-600/20",
};

const STATUS_DOT: Record<HistoryStatus, string> = {
  new: "bg-sky-500",
  in_progress: "bg-blue-500",
  blocked: "bg-red-500",
  ready_for_review: "bg-violet-500",
  ready_to_deliver: "bg-amber-500",
  delivered: "bg-teal-500",
  completed: "bg-emerald-500",
  cancelled: "bg-zinc-300",
  draft: "bg-zinc-400",
  submitted: "bg-sky-500",
  needs_info: "bg-amber-500",
  assigned: "bg-indigo-500",
};

export function StatusBadge({ status, className }: { status: HistoryStatus; className?: string }) {
  return (
    <Badge variant="outline" className={cn("gap-1.5 border-0 ring-1 ring-inset", STATUS_TONE[status], className)}>
      <span className={cn("size-1.5 rounded-full", STATUS_DOT[status])} />
      {STATUS_LABELS[status]}
    </Badge>
  );
}

export function statusDot(status: HistoryStatus) {
  return STATUS_DOT[status];
}

const PRIORITY_ICON = { low: ArrowDownIcon, medium: MinusIcon, high: ArrowUpIcon, urgent: ChevronsUpIcon };
const PRIORITY_TONE: Record<Priority, string> = {
  low: "text-zinc-500",
  medium: "text-sky-600",
  high: "text-orange-600",
  urgent: "text-red-600",
};

export function PriorityLabel({ priority, className }: { priority: Priority; className?: string }) {
  const Icon = PRIORITY_ICON[priority];
  return (
    <span className={cn("inline-flex items-center gap-1 text-sm font-medium", PRIORITY_TONE[priority], className)}>
      <Icon className="size-3.5" />
      {PRIORITY_LABELS[priority]}
    </span>
  );
}

export function OverdueBadge() {
  return (
    <Badge variant="outline" className="gap-1 border-0 bg-red-600 text-white">
      <ClockAlertIcon className="size-3" /> Overdue
    </Badge>
  );
}

export function RevisionBadge({ count }: { count: number }) {
  if (!count) return null;
  return (
    <Badge variant="outline" className="gap-1">
      <AlertTriangleIcon className="size-3 text-amber-600" />
      {count} revision{count > 1 ? "s" : ""}
    </Badge>
  );
}

const ACCESS_TONE: Record<StoreAccess, string> = {
  not_requested: "bg-red-50 text-red-700 ring-red-600/20 dark:bg-red-500/10 dark:text-red-400 dark:ring-red-500/30",
  requested: "bg-amber-50 text-amber-800 ring-amber-600/20 dark:bg-amber-500/10 dark:text-amber-400 dark:ring-amber-500/30",
  given: "bg-emerald-50 text-emerald-700 ring-emerald-600/20 dark:bg-emerald-500/10 dark:text-emerald-400 dark:ring-emerald-500/30",
};
const ACCESS_DOT: Record<StoreAccess, string> = { not_requested: "bg-red-500", requested: "bg-amber-500", given: "bg-emerald-500" };

/** Where access to the client's store stands. Not requested is red: it's the one to chase. */
export function StoreAccessBadge({ access, short = false, className }: { access: StoreAccess; short?: boolean; className?: string }) {
  const label = short ? { not_requested: "No access", requested: "Access requested", given: "Access" }[access] : STORE_ACCESS_LABELS[access];
  return (
    <Badge variant="outline" className={cn("gap-1.5 border-0 ring-1 ring-inset", ACCESS_TONE[access], className)}>
      <span className={cn("size-1.5 rounded-full", ACCESS_DOT[access])} />
      {label}
    </Badge>
  );
}
