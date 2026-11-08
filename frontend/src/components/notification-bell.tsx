"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import {
  AlertOctagonIcon,
  ArrowRightLeftIcon,
  BellIcon,
  BellRingIcon,
  CheckCheckIcon,
  CheckCircle2Icon,
  ClockAlertIcon,
  FolderInputIcon,
  HelpCircleIcon,
  KanbanIcon,
  MegaphoneIcon,
  PackageCheckIcon,
  MessageSquareIcon,
  PencilLineIcon,
  RotateCcwIcon,
  SendIcon,
  UserPlusIcon,
  XCircleIcon,
  CalendarCheckIcon,
  CalendarClockIcon,
  CalendarXIcon,
  ReceiptTextIcon,
  TimerResetIcon,
  Undo2Icon,
  type LucideIcon,
} from "lucide-react";
import { relativeTime } from "@/lib/format";
import { useMarkRead, useNotifications, useUnreadCount, type AppNotification } from "@/lib/notifications";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

const ICONS: Record<string, LucideIcon> = {
  "project.submitted": FolderInputIcon,
  "project.assigned": UserPlusIcon,
  "project.unassigned": UserPlusIcon,
  "project.moved": ArrowRightLeftIcon,
  "project.needs_info": HelpCircleIcon,
  "project.changes": PencilLineIcon,
  "project.important": BellRingIcon,
  "project.blocked": AlertOctagonIcon,
  "project.ready_for_review": CheckCircle2Icon,
  "project.ready_to_deliver": SendIcon,
  "project.delivered": PackageCheckIcon,
  "project.sent_back": RotateCcwIcon,
  "project.revision": RotateCcwIcon,
  "project.completed": CheckCheckIcon,
  "project.cancelled": XCircleIcon,
  "project.overdue": ClockAlertIcon,
  "project.unassigned_24h": ClockAlertIcon,
  "update.submitted": MegaphoneIcon,
  "update.approved": CheckCircle2Icon,
  "update.changes_requested": RotateCcwIcon,
  "update.sent": SendIcon,
  "update.comments": MessageSquareIcon,
  "update.unsent_24h": ClockAlertIcon,
  "card.created": KanbanIcon,
  "card.assigned": UserPlusIcon,
  "card.review": CheckCircle2Icon,
  "card.done": CheckCheckIcon,
  "card.comments": MessageSquareIcon,
  "hr.leave_requested": CalendarClockIcon,
  "hr.leave_approved": CalendarCheckIcon,
  "hr.leave_rejected": CalendarXIcon,
  "hr.leave_cancelled": CalendarXIcon,
  "hr.correction_requested": TimerResetIcon,
  "hr.correction_reviewed": TimerResetIcon,
  "invoice.raised": ReceiptTextIcon,
  "invoice.approved": CheckCircle2Icon,
  "invoice.rejected": XCircleIcon,
  "invoice.withdrawn": Undo2Icon,
};

export function NotificationBell() {
  const [open, setOpen] = useState(false);
  const [unreadOnly, setUnreadOnly] = useState(false);
  const router = useRouter();
  const unread = useUnreadCount().data ?? 0;
  const list = useNotifications(open);
  const markRead = useMarkRead();

  const items = (list.data?.notifications ?? []).filter((n) => !unreadOnly || !n.read_at);

  function openItem(n: AppNotification) {
    if (!n.read_at) markRead.mutate(n.id);
    setOpen(false);
    if (n.data?.link) router.push(n.data.link);
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon" className="relative" aria-label={unread ? `Notifications, ${unread} unread` : "Notifications"}>
          <BellIcon />
          {unread > 0 && (
            <span className="absolute -top-0.5 -right-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[10px] font-semibold text-primary-foreground tabular-nums">
              {unread > 99 ? "99+" : unread}
            </span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[min(26rem,calc(100vw-2rem))] p-0">
        <div className="flex items-center justify-between border-b px-4 py-3">
          <div className="flex items-center gap-3">
            <span className="text-sm font-semibold">Notifications</span>
            <div className="flex rounded-md bg-muted p-0.5 text-xs">
              {[false, true].map((u) => (
                <button
                  key={String(u)}
                  onClick={() => setUnreadOnly(u)}
                  className={cn("rounded px-2 py-0.5", unreadOnly === u ? "bg-background shadow-sm" : "text-muted-foreground")}
                >
                  {u ? "Unread" : "All"}
                </button>
              ))}
            </div>
          </div>
          <Button variant="ghost" size="sm" disabled={!unread || markRead.isPending} onClick={() => markRead.mutate("all")}>
            <CheckCheckIcon /> Mark all read
          </Button>
        </div>
        <ScrollArea className="h-[min(28rem,70vh)]">
          {list.isPending && (
            <div className="space-y-3 p-4">
              {Array.from({ length: 4 }, (_, i) => (
                <Skeleton key={i} className="h-14 w-full" />
              ))}
            </div>
          )}
          {list.isSuccess && items.length === 0 && (
            <Empty className="py-12">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <BellIcon />
                </EmptyMedia>
                <EmptyTitle className="text-sm">{unreadOnly ? "You're all caught up" : "No notifications yet"}</EmptyTitle>
                <EmptyDescription>New projects, assignments and updates will show up here.</EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
          <ul className="divide-y">
            {items.map((n) => {
              const Icon = ICONS[n.type] ?? BellIcon;
              return (
                <li key={n.id}>
                  <button
                    onClick={() => openItem(n)}
                    className={cn(
                      "flex w-full gap-3 px-4 py-3 text-left transition hover:bg-muted/60",
                      !n.read_at && "bg-accent/40",
                      n.is_important && "border-l-2 border-l-amber-500",
                    )}
                  >
                    <div
                      className={cn(
                        "mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full",
                        n.is_important ? "bg-amber-100 text-amber-700" : "bg-muted text-muted-foreground",
                      )}
                    >
                      <Icon className="size-4" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <p className={cn("text-sm leading-snug", !n.read_at && "font-medium")}>{n.title}</p>
                      {n.body && <p className="mt-0.5 line-clamp-4 text-xs whitespace-pre-line text-muted-foreground">{n.body}</p>}
                      <p className="mt-1 text-[11px] text-muted-foreground">
                        {relativeTime(n.created_at)}
                        {n.data?.count && n.data.count > 1 ? ` · ${n.data.count} changes` : ""}
                      </p>
                    </div>
                    {!n.read_at && <span className="mt-2 size-2 shrink-0 rounded-full bg-primary" aria-label="Unread" />}
                  </button>
                </li>
              );
            })}
          </ul>
        </ScrollArea>
      </PopoverContent>
    </Popover>
  );
}
