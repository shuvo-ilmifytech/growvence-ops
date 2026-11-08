"use client";

import Link from "next/link";
import { ArrowRightIcon, BanknoteIcon, UsersRoundIcon } from "lucide-react";
import { useCompany } from "@/lib/reports";
import { formatDate, formatMoney, relativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import { PriorityLabel, StatusBadge } from "@/components/project-badges";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

function monthName(month: string) {
  const [y, m] = month.split("-").map(Number);
  return new Date(y, (m ?? 1) - 1, 1).toLocaleDateString("en", { month: "long" });
}

/** The CEO's snapshot: money and people this month, then the teams behind it. */
export function CompanyOverview() {
  const { data, isPending } = useCompany();
  const k = data?.kpis;
  const month = data?.month ? monthName(data.month) : "this month";

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <Card className="gap-2">
          <CardHeader>
            <CardDescription>Received for projects completed in {month}, after the marketplace fee</CardDescription>
            <CardAction>
              <BanknoteIcon className="size-4 text-muted-foreground" />
            </CardAction>
          </CardHeader>
          <CardContent>
            {isPending ? (
              <Skeleton className="h-9 w-32" />
            ) : (data?.revenue_this_month.length ?? 0) === 0 ? (
              <div className="text-3xl font-semibold tabular-nums">—</div>
            ) : (
              <div className="space-y-1">
                {data?.revenue_this_month.map((r) => (
                  <div key={r.currency} className="flex items-baseline gap-2">
                    <span className="text-2xl font-semibold tabular-nums">{formatMoney(r.total, r.currency)}</span>
                    <span className="text-xs text-muted-foreground">
                      {r.projects} project{r.projects === 1 ? "" : "s"}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>

        <Card className="gap-2">
          <CardHeader>
            <CardDescription>Delivered in {month}</CardDescription>
          </CardHeader>
          <CardContent>
            {isPending ? <Skeleton className="h-9 w-16" /> : <div className="text-3xl font-semibold tabular-nums">{k?.completed_this_month ?? 0}</div>}
            <p className="mt-1 text-xs text-muted-foreground">
              {k?.live_projects ?? 0} live · {k?.unassigned_projects ?? 0} waiting to be assigned
            </p>
          </CardContent>
        </Card>

        <Card className="gap-2">
          <CardHeader>
            <CardDescription>People in today</CardDescription>
            <CardAction>
              <UsersRoundIcon className="size-4 text-muted-foreground" />
            </CardAction>
          </CardHeader>
          <CardContent>
            {isPending ? (
              <Skeleton className="h-9 w-24" />
            ) : (
              <div className="text-3xl font-semibold tabular-nums">
                {k?.checked_in_today ?? 0}
                <span className="text-base font-normal text-muted-foreground"> / {k?.headcount ?? 0}</span>
              </div>
            )}
            <p className="mt-1 text-xs text-muted-foreground">{k?.on_leave_today ?? 0} on approved leave</p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Teams</CardTitle>
          <CardDescription>What each team is carrying, and what it has finished in {month}.</CardDescription>
          <CardAction>
            <Button variant="ghost" size="sm" asChild>
              <Link href="/admin/teams">
                Manage teams <ArrowRightIcon />
              </Link>
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          {isPending ? (
            <div className="space-y-2">
              {Array.from({ length: 3 }, (_, i) => (
                <Skeleton key={i} className="h-10" />
              ))}
            </div>
          ) : (data?.teams.length ?? 0) === 0 ? (
            <Empty className="border border-dashed py-8">
              <EmptyHeader>
                <EmptyTitle className="text-sm">No teams yet</EmptyTitle>
                <EmptyDescription>Create a team to route projects by type.</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className="-mx-2 overflow-x-auto px-2">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Team</TableHead>
                    <TableHead>Lead</TableHead>
                    <TableHead className="text-right">People</TableHead>
                    <TableHead className="text-right">Live</TableHead>
                    <TableHead className="text-right">Unassigned</TableHead>
                    <TableHead className="text-right">Late</TableHead>
                    <TableHead className="text-right">Delivered</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data?.teams.map((t) => (
                    <TableRow key={t.id}>
                      <TableCell className="font-medium">{t.name}</TableCell>
                      <TableCell className="text-muted-foreground">{t.lead_name ?? "No lead"}</TableCell>
                      <TableCell className="text-right tabular-nums">{t.members}</TableCell>
                      <TableCell className="text-right tabular-nums">{t.live}</TableCell>
                      <TableCell className={cn("text-right tabular-nums", t.unassigned && "text-sky-600")}>{t.unassigned || "—"}</TableCell>
                      <TableCell className={cn("text-right tabular-nums", t.overdue && "font-medium text-red-600")}>{t.overdue || "—"}</TableCell>
                      <TableCell className="text-right tabular-nums text-muted-foreground">{t.completed_this_month}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Overdue</CardTitle>
            <CardDescription>Past its delivery deadline and still open.</CardDescription>
            <CardAction>
              <Button variant="ghost" size="sm" asChild>
                <Link href="/projects?view=all&overdue=true">
                  View all <ArrowRightIcon />
                </Link>
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            {isPending ? (
              <Skeleton className="h-24" />
            ) : (data?.overdue.length ?? 0) === 0 ? (
              <Empty className="border border-dashed py-8">
                <EmptyHeader>
                  <EmptyTitle className="text-sm">All on time</EmptyTitle>
                  <EmptyDescription>Nothing is past its deadline.</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <ul className="-mx-2 divide-y">
                {data?.overdue.slice(0, 6).map((p) => (
                  <li key={p.id}>
                    <Link href={`/projects/${p.id}`} className="flex items-center gap-3 rounded-md px-2 py-2 hover:bg-muted/60">
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm font-medium">
                          <span className="mr-1.5 font-mono text-xs text-muted-foreground">#{p.number}</span>
                          {p.name ?? "Untitled"}
                        </div>
                        <div className="mt-0.5 text-xs text-muted-foreground">{p.assignee_name ?? "Nobody assigned"}</div>
                      </div>
                      <span className="shrink-0 text-xs font-medium text-red-600 tabular-nums">
                        {p.days_overdue} day{p.days_overdue === 1 ? "" : "s"} late
                      </span>
                      <StatusBadge status={p.status} />
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Waiting to be assigned</CardTitle>
            <CardDescription>Added by Sales, not yet picked up by a team.</CardDescription>
            <CardAction>
              <Button variant="ghost" size="sm" asChild>
                <Link href="/projects?view=all&status=new">
                  View all <ArrowRightIcon />
                </Link>
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            {isPending ? (
              <Skeleton className="h-24" />
            ) : (data?.unassigned.length ?? 0) === 0 ? (
              <Empty className="border border-dashed py-8">
                <EmptyHeader>
                  <EmptyTitle className="text-sm">Nothing waiting</EmptyTitle>
                  <EmptyDescription>Every new project has a team on it.</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <ul className="-mx-2 divide-y">
                {data?.unassigned.slice(0, 6).map((p) => (
                  <li key={p.id}>
                    <Link href={`/projects/${p.id}`} className="flex items-center gap-3 rounded-md px-2 py-2 hover:bg-muted/60">
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm font-medium">
                          <span className="mr-1.5 font-mono text-xs text-muted-foreground">#{p.number}</span>
                          {p.name ?? "Untitled"}
                        </div>
                        <div className="mt-0.5 flex items-center gap-2 text-xs text-muted-foreground">
                          <PriorityLabel priority={p.priority} className="text-xs" />
                          <span>·</span>
                          <span>from {p.sales_name}</span>
                        </div>
                      </div>
                      <span className="shrink-0 text-xs text-muted-foreground">
                        {p.waiting_since ? `waiting ${relativeTime(p.waiting_since)}` : `due ${formatDate(p.deadline)}`}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
