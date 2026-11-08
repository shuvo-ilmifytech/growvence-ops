"use client";

import Link from "next/link";
import { useState } from "react";
import { CalendarDaysIcon, CalendarPlusIcon, InboxIcon, PlaneIcon, TimerResetIcon } from "lucide-react";
import { api } from "@/lib/api";
import { formatDate, formatDateTime } from "@/lib/format";
import {
  clock,
  days,
  monthOf,
  remaining,
  useHrAction,
  useHrMe,
  useMyCorrections,
  useMyLeave,
  useSheet,
  type Balance,
  type LeaveRequest,
} from "@/lib/hr";
import { PageHeader } from "@/components/page-header";
import { LeaveStatusBadge } from "@/components/hr/hr-badges";
import { CorrectionDialog, LeaveDialog } from "@/components/hr/hr-dialogs";
import { MonthCalendar, MonthNav, SummaryLine } from "@/components/hr/month-calendar";
import { TodayCard } from "@/components/hr/today-card";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export function MyHr() {
  const me = useHrMe();
  const [leaveOpen, setLeaveOpen] = useState(false);
  const [fixOpen, setFixOpen] = useState(false);
  const [month, setMonth] = useState<string | null>(null);
  const today = me.data?.today ?? "";
  const shown = month ?? monthOf(today);
  const userId = me.data?.employee.user_id;
  const sheet = useSheet({ month: shown, user_id: userId });

  if (me.isError) return <p className="text-sm text-destructive">{me.error.message}</p>;
  if (!me.data) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-10 w-64" />
        <Skeleton className="h-36 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }
  const d = me.data;
  const person = sheet.data?.people[0];

  return (
    <div className="space-y-6">
      <PageHeader
        title="My HR"
        description="Check in, keep track of your attendance and request leave."
        actions={
          <>
            {d.approval_count > 0 && (
              <Button variant="outline" asChild>
                <Link href="/hr/approvals">
                  <InboxIcon /> {d.approval_count} to approve
                </Link>
              </Button>
            )}
            <Button onClick={() => setLeaveOpen(true)}>
              <CalendarPlusIcon /> Request leave
            </Button>
          </>
        }
      />

      <TodayCard me={d} onCorrection={() => setFixOpen(true)} />

      <Balances balances={d.balances} />

      <Card>
        <CardHeader className="flex flex-wrap items-center justify-between gap-2">
          <CardTitle className="flex items-center gap-2">
            <CalendarDaysIcon className="size-4 text-primary" /> Attendance
          </CardTitle>
          <MonthNav month={shown} onChange={setMonth} />
        </CardHeader>
        <CardContent className="space-y-4">
          {sheet.data && person ? (
            <>
              <SummaryLine person={person} />
              <MonthCalendar sheet={sheet.data} person={person} />
            </>
          ) : (
            <Skeleton className="h-72 w-full" />
          )}
        </CardContent>
      </Card>

      <MyLeave userId={userId} today={today} onApply={() => setLeaveOpen(true)} />
      <MyCorrections tz={d.schedule.timezone} />

      <LeaveDialog open={leaveOpen} onOpenChange={setLeaveOpen} balances={d.balances} today={today} />
      <CorrectionDialog open={fixOpen} onOpenChange={setFixOpen} today={today} />
    </div>
  );
}

function Balances({ balances }: { balances: Balance[] }) {
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
      {balances.map((b) => {
        const left = remaining(b);
        const pct = b.is_limited && Number(b.entitled) > 0 ? Math.max(0, Math.min(100, (left / Number(b.entitled)) * 100)) : 0;
        return (
          <Card key={b.leave_type_id} className="gap-2 py-4">
            <CardContent className="space-y-2 px-4">
              <div className="flex items-baseline justify-between gap-2">
                <span className="text-sm font-medium">{b.leave_type_name}</span>
                {!b.is_paid && !/unpaid/i.test(b.leave_type_name) && <span className="text-xs text-muted-foreground">Unpaid</span>}
              </div>
              {b.is_limited ? (
                <>
                  <div className="text-2xl font-semibold tabular-nums">
                    {days(left)}
                    <span className="text-sm font-normal text-muted-foreground"> / {days(b.entitled)} left</span>
                  </div>
                  <div className="h-1.5 overflow-hidden rounded-full bg-muted">
                    <div className="h-full rounded-full bg-primary" style={{ width: `${pct}%` }} />
                  </div>
                </>
              ) : (
                <div className="text-2xl font-semibold tabular-nums">
                  {days(b.approved)}
                  <span className="text-sm font-normal text-muted-foreground"> taken</span>
                </div>
              )}
              <p className="text-xs text-muted-foreground">
                {b.is_limited ? `${days(b.approved)} taken` : "No yearly limit"}
                {Number(b.pending) > 0 && ` · ${days(b.pending)} pending`}
              </p>
            </CardContent>
          </Card>
        );
      })}
    </div>
  );
}

export function leaveDates(l: Pick<LeaveRequest, "start_date" | "end_date" | "half_day">) {
  const range = l.start_date === l.end_date ? formatDate(l.start_date) : `${formatDate(l.start_date)} – ${formatDate(l.end_date)}`;
  return l.half_day ? `${range} (half day)` : range;
}

function MyLeave({ userId, today, onApply }: { userId?: string; today: string; onApply: () => void }) {
  const leave = useMyLeave(userId);
  const cancel = useHrAction((id: string) => api(`/hr/me/leave/${id}/cancel`, { method: "POST" }), "Leave cancelled");
  const rows = leave.data ?? [];
  return (
    <Card className="pb-0">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <PlaneIcon className="size-4 text-primary" /> My leave
        </CardTitle>
        <CardDescription>Your Team Lead approves first, then HR gives the final approval. You can cancel until the leave starts.</CardDescription>
      </CardHeader>
      {rows.length === 0 && leave.isSuccess ? (
        <Empty className="border-t">
          <EmptyHeader>
            <EmptyTitle>No leave yet</EmptyTitle>
            <EmptyDescription>
              <Button variant="link" className="h-auto p-0" onClick={onApply}>
                Request leave
              </Button>{" "}
              when you need time off.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-6">Dates</TableHead>
              <TableHead>Type</TableHead>
              <TableHead className="text-right">Days</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="hidden md:table-cell">Note</TableHead>
              <TableHead className="w-24" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((l) => {
              const cancellable = l.status === "pending" || l.status === "awaiting_hr" || (l.status === "approved" && l.start_date > today);
              return (
                <TableRow key={l.id}>
                  <TableCell className="pl-6 font-medium">{leaveDates(l)}</TableCell>
                  <TableCell>{l.leave_type_name}</TableCell>
                  <TableCell className="text-right tabular-nums">{days(l.days)}</TableCell>
                  <TableCell>
                    <LeaveStatusBadge status={l.status} />
                  </TableCell>
                  <TableCell className="hidden max-w-64 truncate text-sm text-muted-foreground md:table-cell" title={l.review_note || l.reason}>
                    {l.review_note ? `${l.reviewed_by_name}: ${l.review_note}` : l.manager_note ? `${l.manager_reviewed_by_name}: ${l.manager_note}` : l.reason}
                  </TableCell>
                  <TableCell className="pr-6 text-right">
                    {cancellable && (
                      <Button variant="ghost" size="sm" disabled={cancel.isPending} onClick={() => cancel.mutate(l.id)}>
                        Cancel
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      )}
    </Card>
  );
}

function MyCorrections({ tz }: { tz: string }) {
  const list = useMyCorrections();
  const cancel = useHrAction((id: string) => api(`/hr/me/corrections/${id}/cancel`, { method: "POST" }), "Correction cancelled");
  const rows = list.data ?? [];
  if (!rows.length) return null;
  return (
    <Card className="pb-0">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <TimerResetIcon className="size-4 text-primary" /> Attendance corrections
        </CardTitle>
      </CardHeader>
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="pl-6">Day</TableHead>
            <TableHead>Times</TableHead>
            <TableHead>Status</TableHead>
            <TableHead className="hidden md:table-cell">Reason</TableHead>
            <TableHead className="w-24" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((c) => (
            <TableRow key={c.id}>
              <TableCell className="pl-6 font-medium">{formatDate(c.work_date)}</TableCell>
              <TableCell className="tabular-nums">
                {clock(c.check_in_at, tz)} – {clock(c.check_out_at, tz)}
              </TableCell>
              <TableCell>
                <LeaveStatusBadge status={c.status} />
              </TableCell>
              <TableCell className="hidden max-w-64 truncate text-sm text-muted-foreground md:table-cell" title={c.reason}>
                {c.review_note ? `${c.reviewed_by_name}: ${c.review_note}` : c.reason}
                <span className="sr-only"> · requested {formatDateTime(c.created_at)}</span>
              </TableCell>
              <TableCell className="pr-6 text-right">
                {c.status === "pending" && (
                  <Button variant="ghost" size="sm" disabled={cancel.isPending} onClick={() => cancel.mutate(c.id)}>
                    Cancel
                  </Button>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Card>
  );
}
