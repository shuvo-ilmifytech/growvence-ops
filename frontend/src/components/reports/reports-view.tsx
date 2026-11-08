"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
  AlertOctagonIcon,
  ClockAlertIcon,
  DownloadIcon,
  FolderKanbanIcon,
  HourglassIcon,
  InboxIcon,
  TimerIcon,
  type LucideIcon,
} from "lucide-react";
import { formatDate, formatMoney, relativeTime } from "@/lib/format";
import { formatHours, useReportSummary } from "@/lib/reports";
import { useMe } from "@/lib/session";
import {
  DESTINATION_LABELS,
  DESTINATIONS,
  STATUS_LABELS,
  STATUSES,
  UPDATE_STATUS_LABELS,
  type UpdateStatus,
} from "@/lib/types";
import { PageHeader } from "@/components/page-header";
import { PriorityLabel, StatusBadge } from "@/components/project-badges";
import { BarList } from "@/components/reports/bar-list";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";

const RANGES = [
  { value: "7", label: "Last 7 days" },
  { value: "30", label: "Last 30 days" },
  { value: "90", label: "Last 90 days" },
  { value: "365", label: "Last 12 months" },
];
const UPDATE_COLS: UpdateStatus[] = ["draft", "pending_approval", "changes_requested", "approved", "sent"];
const ACTIVE = new Set(["new", "in_progress", "blocked", "ready_for_review", "ready_to_deliver"]);

function dayString(d: Date) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

export function ReportsView() {
  const { data: me } = useMe();
  const router = useRouter();
  const pathname = usePathname();
  const sp = useSearchParams();
  const days = sp.get("days") ?? "30";
  const to = new Date();
  const from = new Date(to.getTime() - (Number(days) - 1) * 864e5);
  const fromS = dayString(from);
  const toS = dayString(to);
  const report = useReportSummary(fromS, toS);
  const d = report.data;

  if (me && me.role !== "ceo") {
    return (
      <Empty className="min-h-[50vh]">
        <EmptyHeader>
          <EmptyTitle>Reports are for the CEO</EmptyTitle>
          <EmptyDescription>Ask an admin if you need access.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  const byStatus = new Map(d?.projects_by_status.map((s) => [s.status, s.n]));
  const active = d?.projects_by_status.filter((s) => ACTIVE.has(s.status)).reduce((n, s) => n + s.n, 0) ?? 0;
  const matrix = new Map(d?.updates_by_destination_status.map((u) => [`${u.destination}/${u.status}`, u.n]));
  const range = `from=${fromS}&to=${toS}`;

  return (
    <div className="space-y-6">
      <PageHeader
        title="Reports"
        description={`Live snapshot of the team, plus activity between ${formatDate(fromS)} and ${formatDate(toS)}.`}
        actions={
          <>
            <Select value={days} onValueChange={(v) => router.replace(`${pathname}?days=${v}`, { scroll: false })}>
              <SelectTrigger className="w-40">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {RANGES.map((r) => (
                  <SelectItem key={r.value} value={r.value}>
                    {r.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button variant="outline" asChild>
              <a href={`/api/reports/export/updates.csv?${range}`}>
                <DownloadIcon /> Updates CSV
              </a>
            </Button>
            <Button variant="outline" asChild>
              <a href={`/api/reports/export/projects.csv?${range}`}>
                <DownloadIcon /> Projects CSV
              </a>
            </Button>
          </>
        }
      />

      {/* Headline numbers */}
      <div className={cn("grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-6", report.isFetching && report.isPlaceholderData && "opacity-60")}>
        <Stat icon={FolderKanbanIcon} label="Active projects" value={d ? String(active) : undefined} href="/projects" />
        <Stat icon={ClockAlertIcon} label="Overdue" value={d ? String(d.overdue.length) : undefined} tone={d?.overdue.length ? "text-red-600" : undefined} href="/projects?view=all&overdue=true" />
        <Stat icon={InboxIcon} label="Unassigned" value={d ? String(d.unassigned.length) : undefined} href="/projects?view=all&status=new" />
        <Stat
          icon={TimerIcon}
          label="Submit → assign"
          sub={d ? `median of ${d.timing.assigned_n}` : undefined}
          value={d ? formatHours(d.timing.assign_median_h, d.timing.assigned_n) : undefined}
        />
        <Stat
          icon={HourglassIcon}
          label="Submit → complete"
          sub={d ? `median of ${d.timing.completed_n}` : undefined}
          value={d ? formatHours(d.timing.complete_median_h, d.timing.completed_n) : undefined}
        />
        <Stat
          icon={AlertOctagonIcon}
          label="Approval turnaround"
          sub={d ? `median of ${d.approval_turnaround.n} reviews` : undefined}
          value={d ? formatHours(d.approval_turnaround.median_h, d.approval_turnaround.n) : undefined}
        />
      </div>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Projects by status</CardTitle>
            <CardDescription>All projects right now.</CardDescription>
          </CardHeader>
          <CardContent>
            {d ? (
              <BarList
                rows={STATUSES.filter((s) => byStatus.get(s)).map((s) => ({ label: STATUS_LABELS[s], value: byStatus.get(s) ?? 0, hint: STATUS_LABELS[s] }))}
                emptyText="No projects yet."
              />
            ) : (
              <Skeleton className="h-48" />
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Team workload</CardTitle>
            <CardDescription>Projects currently assigned to each member.</CardDescription>
          </CardHeader>
          <CardContent>
            {d ? (
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead>Member</TableHead>
                    <TableHead className="w-1/3">Active</TableHead>
                    <TableHead className="text-right">Blocked</TableHead>
                    <TableHead className="text-right">Overdue</TableHead>
                    <TableHead className="text-right">Completed</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {d.projects_by_member.map((m) => {
                    const max = Math.max(1, ...d.projects_by_member.map((x) => x.active));
                    return (
                      <TableRow key={m.id}>
                        <TableCell className="font-medium">{m.name}</TableCell>
                        <TableCell>
                          <div className="flex items-center gap-2">
                            <div className="h-2.5 rounded-r-[4px] bg-primary" style={{ width: `${(m.active / max) * 70}%`, minWidth: m.active ? 4 : 0 }} />
                            <span className="tabular-nums">{m.active}</span>
                          </div>
                        </TableCell>
                        <TableCell className="text-right tabular-nums">{m.blocked || "—"}</TableCell>
                        <TableCell className={cn("text-right tabular-nums", m.overdue > 0 && "font-medium text-red-600")}>{m.overdue || "—"}</TableCell>
                        <TableCell className="text-right tabular-nums">{m.completed}</TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            ) : (
              <Skeleton className="h-48" />
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Overdue projects</CardTitle>
            <CardDescription>Past the Team Leader&apos;s due date, or the delivery deadline if none.</CardDescription>
          </CardHeader>
          <CardContent>
            {d && d.overdue.length === 0 && <p className="text-sm text-muted-foreground">Nothing is overdue.</p>}
            <ul className="divide-y">
              {d?.overdue.map((p) => (
                <li key={p.id}>
                  <Link href={`/projects/${p.id}`} className="flex items-center gap-3 py-2.5 text-sm hover:bg-muted/40">
                    <div className="min-w-0 flex-1">
                      <div className="truncate font-medium">
                        <span className="mr-1.5 font-mono text-xs text-muted-foreground">#{p.number}</span>
                        {p.name}
                      </div>
                      <div className="text-xs text-muted-foreground">
                        {p.assignee_name ?? "Unassigned"} · due {formatDate(p.due)}
                      </div>
                    </div>
                    <StatusBadge status={p.status} />
                    <span className="w-20 text-right font-medium tabular-nums text-red-600">
                      {p.days_overdue} day{p.days_overdue === 1 ? "" : "s"}
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Unassigned queue</CardTitle>
            <CardDescription>Submitted and waiting for the Team Leader.</CardDescription>
          </CardHeader>
          <CardContent>
            {d && d.unassigned.length === 0 && <p className="text-sm text-muted-foreground">The queue is empty.</p>}
            <ul className="divide-y">
              {d?.unassigned.map((p) => {
                const hours = (report.dataUpdatedAt - new Date(p.waiting_since).getTime()) / 36e5;
                return (
                  <li key={p.id}>
                    <Link href={`/projects/${p.id}`} className="flex items-center gap-3 py-2.5 text-sm hover:bg-muted/40">
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-medium">
                          <span className="mr-1.5 font-mono text-xs text-muted-foreground">#{p.number}</span>
                          {p.name}
                        </div>
                        <div className="text-xs text-muted-foreground">
                          {p.sales_name} · deadline {formatDate(p.deadline)}
                        </div>
                      </div>
                      <PriorityLabel priority={p.priority} />
                      <span className={cn("w-24 text-right text-xs", hours > 24 ? "font-medium text-red-600" : "text-muted-foreground")}>
                        waiting {relativeTime(p.waiting_since).replace(" ago", "")}
                      </span>
                    </Link>
                  </li>
                );
              })}
            </ul>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Client updates</CardTitle>
          <CardDescription>
            Written in the period, by where they go and where they are now.
            {d && d.approval_turnaround.n > 0 && (
              <>
                {" "}
                Average review time {formatHours(d.approval_turnaround.avg_h, d.approval_turnaround.n)};{" "}
                {d.approval_turnaround.sent_back} of {d.approval_turnaround.n} reviews were sent back.
              </>
            )}
          </CardDescription>
        </CardHeader>
        <CardContent className="overflow-x-auto">
          {d ? (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Update to</TableHead>
                  {UPDATE_COLS.map((s) => (
                    <TableHead key={s} className="text-right">
                      {UPDATE_STATUS_LABELS[s]}
                    </TableHead>
                  ))}
                  <TableHead className="text-right">Total</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {DESTINATIONS.map((dest) => {
                  const total = UPDATE_COLS.reduce((n, s) => n + (matrix.get(`${dest}/${s}`) ?? 0), 0);
                  return (
                    <TableRow key={dest}>
                      <TableCell className="font-medium">{DESTINATION_LABELS[dest]}</TableCell>
                      {UPDATE_COLS.map((s) => {
                        const n = matrix.get(`${dest}/${s}`) ?? 0;
                        return (
                          <TableCell key={s} className={cn("text-right tabular-nums", !n && "text-muted-foreground")}>
                            {n || "—"}
                          </TableCell>
                        );
                      })}
                      <TableCell className="text-right font-medium tabular-nums">{total}</TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          ) : (
            <Skeleton className="h-32" />
          )}
        </CardContent>
      </Card>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        <Card>
          <CardHeader>
            <CardTitle>Revisions</CardTitle>
            <CardDescription>Projects completed in the period.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {d ? (
              <>
                <div className="flex gap-8">
                  <div>
                    <div className="text-3xl font-semibold tabular-nums">{d.revisions.avg_revisions.toFixed(1)}</div>
                    <div className="text-xs text-muted-foreground">avg revisions per project</div>
                  </div>
                  <div>
                    <div className="text-3xl font-semibold tabular-nums">
                      {d.revisions.with_revisions}/{d.revisions.completed_n}
                    </div>
                    <div className="text-xs text-muted-foreground">needed at least one</div>
                  </div>
                </div>
                {d.top_revisions.length > 0 && (
                  <div>
                    <div className="mb-2 text-xs font-medium text-muted-foreground">Most revised (all time)</div>
                    <ul className="space-y-1.5 text-sm">
                      {d.top_revisions.map((p) => (
                        <li key={p.id} className="flex justify-between gap-2">
                          <Link href={`/projects/${p.id}`} className="truncate hover:underline">
                            #{p.number} {p.name}
                          </Link>
                          <span className="tabular-nums">{p.revision_count}</span>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
              </>
            ) : (
              <Skeleton className="h-32" />
            )}
          </CardContent>
        </Card>

        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>Sales by person</CardTitle>
            <CardDescription>Projects created, submitted and completed in the period; revenue is what was received for completed projects (agreed price less the marketplace fee).</CardDescription>
          </CardHeader>
          <CardContent className="overflow-x-auto">
            {d ? (
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead>Sales</TableHead>
                    <TableHead className="text-right">Created</TableHead>
                    <TableHead className="text-right">Submitted</TableHead>
                    <TableHead className="text-right">Completed</TableHead>
                    <TableHead className="text-right">Active now</TableHead>
                    <TableHead className="text-right">Revenue</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {d.sales_by_person.map((s) => (
                    <TableRow key={s.id}>
                      <TableCell className="font-medium">{s.name}</TableCell>
                      <TableCell className="text-right tabular-nums">{s.created}</TableCell>
                      <TableCell className="text-right tabular-nums">{s.submitted}</TableCell>
                      <TableCell className="text-right tabular-nums">{s.completed}</TableCell>
                      <TableCell className="text-right tabular-nums">{s.active_now}</TableCell>
                      <TableCell className="text-right tabular-nums">
                        {Object.keys(s.revenue).length
                          ? Object.entries(s.revenue).map(([cur, total]) => <div key={cur}>{formatMoney(total, cur)}</div>)
                          : "—"}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            ) : (
              <Skeleton className="h-32" />
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function Stat({ icon: Icon, label, value, sub, tone, href }: { icon: LucideIcon; label: string; value?: string; sub?: string; tone?: string; href?: string }) {
  const body = (
    <Card className={cn("h-full gap-1 py-4", href && "transition hover:border-primary/40")}>
      <CardContent className="space-y-1 px-4">
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          {label}
          <Icon className="size-3.5" />
        </div>
        {value === undefined ? <Skeleton className="h-8 w-16" /> : <div className={cn("text-2xl font-semibold tabular-nums", tone)}>{value}</div>}
        {sub && <div className="text-[11px] text-muted-foreground">{sub}</div>}
      </CardContent>
    </Card>
  );
  return href ? <Link href={href}>{body}</Link> : body;
}
