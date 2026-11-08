import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { REQUEST_STATUS_LABELS, type RequestStatus } from "@/lib/types";

const TONE: Record<RequestStatus, string> = {
  open: "bg-blue-50 text-blue-700 ring-blue-600/20",
  done: "bg-emerald-50 text-emerald-700 ring-emerald-600/20",
  cancelled: "bg-zinc-50 text-zinc-500 ring-zinc-400/20 line-through",
};

const DOT: Record<RequestStatus, string> = {
  open: "bg-blue-500",
  done: "bg-emerald-500",
  cancelled: "bg-zinc-300",
};

export function RequestStatusBadge({ status, className }: { status: RequestStatus; className?: string }) {
  return (
    <Badge variant="outline" className={cn("gap-1.5 border-0 ring-1 ring-inset", TONE[status], className)}>
      <span className={cn("size-1.5 rounded-full", DOT[status])} />
      {REQUEST_STATUS_LABELS[status]}
    </Badge>
  );
}
