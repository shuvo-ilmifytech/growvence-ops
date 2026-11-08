"use client";

import Link from "next/link";
import { AlertOctagonIcon, CalendarClockIcon, CalendarDaysIcon, ClockAlertIcon } from "lucide-react";
import { useDeadlines, type DeadlineItem } from "@/lib/deadlines";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

const URGENCY_STYLE: Record<DeadlineItem["urgency"], { label: string; badge: string; row: string }> = {
  overdue: { label: "Overdue", badge: "bg-red-100 text-red-700", row: "border-l-red-500" },
  today: { label: "Due today/tomorrow", badge: "bg-amber-100 text-amber-700", row: "border-l-amber-500" },
  soon: { label: "Due within 3 days", badge: "bg-blue-100 text-blue-700", row: "border-l-blue-500" },
  upcoming: { label: "Upcoming", badge: "bg-muted text-muted-foreground", row: "border-l-muted" },
};

function daysText(days: number) {
  if (days < 0) return `${-days}d overdue`;
  if (days === 0) return "Due today";
  if (days === 1) return "Due tomorrow";
  return `Due in ${days}d`;
}

function DeadlineRow({ item }: { item: DeadlineItem }) {
  const s = URGENCY_STYLE[item.urgency];
  return (
    <Link
      href={`/projects/${item.id}`}
      className={cn("flex items-center gap-4 border-l-4 bg-card px-4 py-3 rounded-r-lg hover:bg-muted/50 transition", s.row)}
    >
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium truncate">
          #{item.number} {item.name}
        </p>
        <p className="text-xs text-muted-foreground mt-0.5">
          {item.assignee_name ?? "Unassigned"}
          {item.team_name ? ` · ${item.team_name}` : ""} · {item.status.replace(/_/g, " ")}
        </p>
      </div>
      <Badge className={cn("shrink-0", s.badge)}>{s.label}</Badge>
      <span className="text-xs font-medium tabular-nums shrink-0 w-24 text-right">{daysText(item.days_until_due)}</span>
    </Link>
  );
}

export default function DeadlinesPage() {
  const { data, isPending, isError } = useDeadlines();

  return (
    <div className="space-y-6">
      <PageHeader
        title="Deadlines"
        description="Projects sorted by urgency. Overdue items notify the team hourly until resolved."
      />

      {isPending ? (
        <div className="grid gap-3 md:grid-cols-2">
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} className="h-24" />
          ))}
        </div>
      ) : isError ? (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ClockAlertIcon />
            </EmptyMedia>
            <EmptyTitle>Couldn't load deadlines</EmptyTitle>
            <EmptyDescription>Try reloading the page.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <>
          <div className="grid gap-3 md:grid-cols-2">
            <Card className={cn(data.overdue_count > 0 && "border-red-200")}>
              <CardHeader className="flex flex-row items-center gap-2 pb-2">
                <AlertOctagonIcon className="size-4 text-red-600" />
                <CardTitle className="text-sm font-medium">Overdue</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-3xl font-bold tabular-nums">{data.overdue_count}</p>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="flex flex-row items-center gap-2 pb-2">
                <CalendarDaysIcon className="size-4 text-blue-600" />
                <CardTitle className="text-sm font-medium">Due this week</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-3xl font-bold tabular-nums">{data.due_week_count}</p>
              </CardContent>
            </Card>
          </div>

          {data.overdue.length === 0 && data.due_soon.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <CalendarClockIcon />
                </EmptyMedia>
                <EmptyTitle>All clear</EmptyTitle>
                <EmptyDescription>No upcoming or overdue deadlines.</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className="space-y-6">
              {data.overdue.length > 0 && (
                <section className="space-y-2">
                  <h2 className="text-sm font-semibold text-red-700 flex items-center gap-2">
                    <AlertOctagonIcon className="size-4" /> Overdue ({data.overdue.length})
                  </h2>
                  {data.overdue.map((it) => (
                    <DeadlineRow key={it.id} item={it} />
                  ))}
                </section>
              )}
              {data.due_soon.length > 0 && (
                <section className="space-y-2">
                  <h2 className="text-sm font-semibold flex items-center gap-2">
                    <CalendarClockIcon className="size-4" /> Upcoming ({data.due_soon.length})
                  </h2>
                  {data.due_soon.map((it) => (
                    <DeadlineRow key={it.id} item={it} />
                  ))}
                </section>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
}
