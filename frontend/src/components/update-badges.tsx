import { InboxIcon, PackageCheckIcon, ReceiptTextIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { DESTINATION_LABELS, UPDATE_STATUS_LABELS, type Destination, type UpdateStatus } from "@/lib/types";

const TONE: Record<UpdateStatus, string> = {
  draft: "bg-zinc-100 text-zinc-700 ring-zinc-500/15",
  pending_approval: "bg-amber-50 text-amber-800 ring-amber-600/25",
  changes_requested: "bg-red-50 text-red-700 ring-red-600/20",
  approved: "bg-sky-50 text-sky-700 ring-sky-600/20",
  sent: "bg-emerald-50 text-emerald-700 ring-emerald-600/20",
};

export function UpdateStatusBadge({ status, className }: { status: UpdateStatus; className?: string }) {
  return (
    <Badge variant="outline" className={cn("border-0 ring-1 ring-inset", TONE[status], className)}>
      {UPDATE_STATUS_LABELS[status]}
    </Badge>
  );
}

const DEST_ICON = { inbox: InboxIcon, order_page: ReceiptTextIcon, delivery: PackageCheckIcon };

export function DestinationLabel({ destination, className }: { destination: Destination; className?: string }) {
  const Icon = DEST_ICON[destination];
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-sm", className)}>
      <Icon className="size-3.5 text-muted-foreground" />
      {DESTINATION_LABELS[destination]}
    </span>
  );
}
