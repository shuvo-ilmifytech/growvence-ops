"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { BellRingIcon, CalendarClockIcon, CircleDollarSignIcon } from "lucide-react";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { RemindDialog } from "@/components/projects/project-payments";
import { money } from "@/lib/payments";
import {
  milestoneAmount,
  milestonePaid,
  useDueMilestones,
  type Milestone,
  type MilestoneStatus,
} from "@/lib/milestones";
import { useMe } from "@/lib/session";
import { cn } from "@/lib/utils";

const STATUS_TONE: Record<MilestoneStatus, string> = {
  scheduled: "bg-muted text-muted-foreground",
  invoiced: "bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-300",
  partially_paid: "bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300",
  paid: "bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300",
  overdue: "bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-300",
};

const STATUS_LABEL: Record<MilestoneStatus, string> = {
  scheduled: "Scheduled",
  invoiced: "Invoiced",
  partially_paid: "Partially paid",
  paid: "Paid",
  overdue: "Overdue",
};

function fmtDate(d: string) {
  return new Date(d.length === 10 ? d + "T00:00:00" : d).toLocaleDateString();
}

function MilestoneRow({
  m,
  canManage,
  onRemind,
}: {
  m: Milestone;
  canManage: boolean;
  onRemind: (m: Milestone) => void;
}) {
  const remaining = milestoneAmount(m) - milestonePaid(m);
  return (
    <li className="flex flex-wrap items-center gap-3 px-4 py-3">
      <div className="min-w-0 flex-1">
        <p className="font-medium">
          {m.label}
          <span className="ml-2 text-sm font-normal text-muted-foreground">
            {m.project_name ?? ""}
          </span>
        </p>
        <p className="text-xs text-muted-foreground">
          {money(milestoneAmount(m), m.currency)} · due {fmtDate(m.due_date)}
          {remaining > 0.005 && remaining < milestoneAmount(m)
            ? ` · ${money(remaining, m.currency)} remaining`
            : ""}
          {m.invoice_number ? ` · ${m.invoice_number}` : ""}
        </p>
      </div>
      <Badge className={cn(STATUS_TONE[m.status], "capitalize")}>{STATUS_LABEL[m.status]}</Badge>
      <Link href={`/projects/${m.project_id}/payments`}>
        <Button variant="ghost" size="sm">
          Schedule
        </Button>
      </Link>
      {canManage && m.status !== "paid" && (
        <Button variant="ghost" size="icon" onClick={() => onRemind(m)} aria-label="Remind client">
          <BellRingIcon className="size-4" />
        </Button>
      )}
    </li>
  );
}

export function PaymentsDashboard() {
  const { data, isPending, isError } = useDueMilestones(30);
  const { data: me } = useMe();
  const [reminding, setReminding] = useState<Milestone | null>(null);

  const canManage = !!(
    me?.role === "sales" ||
    me?.role === "tl" ||
    me?.role === "hr" ||
    me?.role === "ceo" ||
    me?.is_admin
  );

  const today = new Date().toISOString().slice(0, 10);
  const in7 = new Date(Date.now() + 7 * 86400000).toISOString().slice(0, 10);
  const { overdue, dueSoon, upcoming } = useMemo(() => {
    const all = data?.milestones ?? [];
    return {
      overdue: all.filter((m) => m.status === "overdue" || m.due_date < today),
      dueSoon: all.filter((m) => m.status !== "overdue" && m.due_date >= today && m.due_date <= in7),
      upcoming: all.filter((m) => m.status !== "overdue" && m.due_date > in7),
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data]);

  const overdueTotal = overdue.reduce((s, m) => s + (milestoneAmount(m) - milestonePaid(m)), 0);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Payments"
        description="Milestone payments due across all projects — overdue first, then the next 30 days."
      />

      {isPending && (
        <div className="grid gap-4 md:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-24" />
          ))}
        </div>
      )}

      {isError && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>Couldn’t load payments</EmptyTitle>
            <EmptyDescription>Try again in a moment.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {!isPending && !isError && (
        <>
          <div className="grid gap-4 md:grid-cols-3">
            <Card className={overdue.length > 0 ? "border-red-300 dark:border-red-800" : undefined}>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">Overdue</CardTitle>
              </CardHeader>
              <CardContent className="text-2xl font-bold text-red-600 dark:text-red-400">
                {overdue.length > 0 ? `${overdue.length} · ${money(overdueTotal, "BDT")}` : "—"}
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">Due in 7 days</CardTitle>
              </CardHeader>
              <CardContent className="text-2xl font-bold">{dueSoon.length}</CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">Upcoming (8–30 days)</CardTitle>
              </CardHeader>
              <CardContent className="text-2xl font-bold">{upcoming.length}</CardContent>
            </Card>
          </div>

          {overdue.length === 0 && dueSoon.length === 0 && upcoming.length === 0 && (
            <Card>
              <CardContent className="py-12 text-center text-muted-foreground">
                <CircleDollarSignIcon className="mx-auto mb-3 size-8 opacity-40" />
                <p>No payments due in the next 30 days.</p>
              </CardContent>
            </Card>
          )}

          {overdue.length > 0 && (
            <Card className="border-red-300 dark:border-red-800">
              <CardHeader>
                <CardTitle className="text-base text-red-700 dark:text-red-400">
                  Overdue ({overdue.length})
                </CardTitle>
              </CardHeader>
              <CardContent className="p-0">
                <ul className="divide-y">
                  {overdue.map((m) => (
                    <MilestoneRow key={m.id} m={m} canManage={canManage} onRemind={setReminding} />
                  ))}
                </ul>
              </CardContent>
            </Card>
          )}

          {dueSoon.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle className="text-base">
                  <CalendarClockIcon className="mr-2 inline size-4" />
                  Due in the next 7 days ({dueSoon.length})
                </CardTitle>
              </CardHeader>
              <CardContent className="p-0">
                <ul className="divide-y">
                  {dueSoon.map((m) => (
                    <MilestoneRow key={m.id} m={m} canManage={canManage} onRemind={setReminding} />
                  ))}
                </ul>
              </CardContent>
            </Card>
          )}

          {upcoming.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle className="text-base">Upcoming — 8 to 30 days ({upcoming.length})</CardTitle>
              </CardHeader>
              <CardContent className="p-0">
                <ul className="divide-y">
                  {upcoming.map((m) => (
                    <MilestoneRow key={m.id} m={m} canManage={canManage} onRemind={setReminding} />
                  ))}
                </ul>
              </CardContent>
            </Card>
          )}
        </>
      )}

      <RemindDialog
        open={!!reminding}
        onOpenChange={(v) => !v && setReminding(null)}
        projectId={reminding?.project_id ?? ""}
        milestone={reminding}
      />
    </div>
  );
}
