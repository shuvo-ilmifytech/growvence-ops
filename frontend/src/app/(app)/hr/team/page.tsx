"use client";

import Link from "next/link";
import { useState } from "react";
import { DownloadIcon, PlaneIcon, UsersIcon } from "lucide-react";
import { useMe } from "@/lib/session";
import { formatDate } from "@/lib/format";
import { clock, DAY_LABELS, hours, monthOf, scheduleLabel, useDepartments, useHrMe, useSheet, useWhosOut, weekday } from "@/lib/hr";
import { PageHeader } from "@/components/page-header";
import { DAY_CODE, DAY_TONE, DayLegend } from "@/components/hr/hr-badges";
import { MonthNav } from "@/components/hr/month-calendar";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

const ALL = "all";

export default function TeamAttendancePage() {
  const { data: me } = useMe();
  const hrMe = useHrMe();
  const today = hrMe.data?.today ?? "";
  const [month, setMonth] = useState<string | null>(null);
  const [dept, setDept] = useState(ALL);
  const shown = month ?? monthOf(today);
  const seesAll = !!me && (me.role === "ceo" || me.role === "hr");
  const departments = useDepartments();
  const sheet = useSheet({ month: shown, department_id: dept === ALL ? undefined : dept });
  const s = sheet.data;
  const tz = s?.schedule.timezone ?? "UTC";

  return (
    <div className="space-y-6">
      <PageHeader
        title="Team attendance"
        description={seesAll ? "Everyone's attendance, day by day." : "Attendance for you and the people who report to you."}
        actions={
          <>
            {seesAll && (departments.data?.length ?? 0) > 0 && (
              <Select value={dept} onValueChange={setDept}>
                <SelectTrigger className="w-44">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={ALL}>All departments</SelectItem>
                  {departments.data!.map((d) => (
                    <SelectItem key={d.id} value={d.id}>
                      {d.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
            {shown && <MonthNav month={shown} onChange={setMonth} />}
            {seesAll && shown && (
              <Button variant="outline" asChild>
                <a href={`/api/hr/attendance/export.csv?month=${shown}`} download>
                  <DownloadIcon /> CSV
                </a>
              </Button>
            )}
          </>
        }
      />

      {today && <WhosOut today={today} />}

      <Card className="gap-0 py-0">
        {!s ? (
          <Skeleton className="m-4 h-64" />
        ) : s.people.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <UsersIcon />
              </EmptyMedia>
              <EmptyTitle>Nobody here</EmptyTitle>
              <EmptyDescription>No one in this department yet.</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <div className={cn("overflow-x-auto", sheet.isFetching && "opacity-70")}>
            <table className="w-full border-separate border-spacing-0 text-xs">
              <thead>
                <tr>
                  <th className="sticky left-0 z-10 min-w-44 border-b bg-card px-4 py-2 text-left font-medium">Person</th>
                  {s.people[0]!.days.map((d) => (
                    <th
                      key={d.date}
                      className={cn(
                        "min-w-7 border-b px-0.5 py-2 text-center font-medium tabular-nums",
                        s.schedule.off_days.includes(weekday(d.date)) && "text-muted-foreground",
                        d.date === s.today && "text-primary",
                      )}
                    >
                      {Number(d.date.slice(8))}
                    </th>
                  ))}
                  {["Present", "Late", "Absent", "Leave", "Hours"].map((h) => (
                    <th key={h} className="border-b px-2 py-2 text-right font-medium whitespace-nowrap">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {s.people.map((p) => (
                  <tr key={p.user_id} className="group">
                    <td className="sticky left-0 z-10 border-b bg-card px-4 py-1.5 group-hover:bg-muted">
                      <Link href={`/hr/people/${p.user_id}`} className="block truncate font-medium hover:underline">
                        {p.name}
                      </Link>
                      <span className="block truncate text-muted-foreground" title={scheduleLabel(p.schedule)}>
                        {p.schedule.shift_name
                          ? `${p.schedule.shift_name} · ${p.schedule.work_start}–${p.schedule.work_end}`
                          : p.designation || p.department_name || "Office hours"}
                      </span>
                    </td>
                    {p.days.map((d) => (
                      <td key={d.date} className="border-b px-0.5 py-1.5 text-center">
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <span
                              tabIndex={0}
                              className={cn(
                                "inline-flex h-6 w-6 items-center justify-center rounded text-[10px] font-semibold",
                                DAY_TONE[d.status],
                              )}
                            >
                              {DAY_CODE[d.status]}
                            </span>
                          </TooltipTrigger>
                          <TooltipContent>
                            {formatDate(d.date)} · {DAY_LABELS[d.status]}
                            {d.leave_type && ` · ${d.leave_type}`}
                            {d.holiday && ` · ${d.holiday}`}
                            {d.check_in && ` · In ${clock(d.check_in, tz)}`}
                            {d.check_out && ` · Out ${clock(d.check_out, tz)}`}
                            {d.worked_minutes > 0 && ` · ${hours(d.worked_minutes)}`}
                            {d.source === "correction" && " · Corrected"}
                          </TooltipContent>
                        </Tooltip>
                      </td>
                    ))}
                    <td className="border-b px-2 text-right tabular-nums">{(p.summary.present ?? 0) + (p.summary.late ?? 0)}</td>
                    <td className="border-b px-2 text-right tabular-nums text-amber-700 dark:text-amber-400">{p.summary.late ?? 0}</td>
                    <td className="border-b px-2 text-right tabular-nums text-red-700 dark:text-red-400">{p.summary.absent ?? 0}</td>
                    <td className="border-b px-2 text-right tabular-nums">{(p.summary.leave ?? 0) + (p.summary.half_day_leave ?? 0) * 0.5}</td>
                    <td className="border-b px-2 pr-4 text-right tabular-nums whitespace-nowrap">{hours(p.worked_minutes)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <div className="border-t px-4 py-3">
          <DayLegend />
        </div>
      </Card>
    </div>
  );
}

function addDays(day: string, n: number) {
  const [y, m, d] = day.split("-").map(Number);
  const t = new Date(y!, m! - 1, d! + n);
  return `${t.getFullYear()}-${String(t.getMonth() + 1).padStart(2, "0")}-${String(t.getDate()).padStart(2, "0")}`;
}

function WhosOut({ today }: { today: string }) {
  const week = addDays(today, 6);
  const out = useWhosOut(today, week);
  const rows = out.data ?? [];
  const now = rows.filter((o) => o.start_date <= today && o.end_date >= today);
  const soon = rows.filter((o) => o.start_date > today);
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <PlaneIcon className="size-4 text-primary" /> Who&apos;s out
        </CardTitle>
        <CardDescription>Approved leave today and over the next 7 days.</CardDescription>
      </CardHeader>
      <CardContent className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <OutList title="Today" rows={now} empty="Everyone's in." />
        <OutList title="Coming up" rows={soon} empty="No leave booked this week." />
      </CardContent>
    </Card>
  );
}

function OutList({ title, rows, empty }: { title: string; rows: { user_id: string; user_name: string; leave_type_name: string; start_date: string; end_date: string; half_day: boolean }[]; empty: string }) {
  return (
    <div>
      <h3 className="mb-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">{title}</h3>
      {rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">{empty}</p>
      ) : (
        <ul className="space-y-1.5 text-sm">
          {rows.map((o) => (
            <li key={o.user_id + o.start_date} className="flex justify-between gap-2">
              <span className="truncate font-medium">{o.user_name}</span>
              <span className="shrink-0 text-muted-foreground">
                {o.leave_type_name} · {formatDate(o.start_date)}
                {o.end_date !== o.start_date && ` – ${formatDate(o.end_date)}`}
                {o.half_day && " (½)"}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
