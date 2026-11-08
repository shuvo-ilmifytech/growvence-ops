"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
  ArrowDownToLineIcon,
  BanknoteIcon,
  CheckCheckIcon,
  ClockAlertIcon,
  DownloadIcon,
  FolderKanbanIcon,
  PackageCheckIcon,
  TargetIcon,
  TimerIcon,
  type LucideIcon,
} from "lucide-react";
import { formatDate, formatMoney } from "@/lib/format";
import { onTimeRate, sumRevenue, useAnalytics, type AnalyticsPerson, type AnalyticsTeam, type Metric, type RevenueRow } from "@/lib/analytics";
import { formatHours } from "@/lib/reports";
import { useMe } from "@/lib/session";
import { cn } from "@/lib/utils";
import { DrillNumber, DrillProvider } from "@/components/analytics/drill";
import { FlowChart } from "@/components/analytics/flow-chart";
import { TargetsCard } from "@/components/analytics/targets-card";
import { TimeCard } from "@/components/analytics/time-card";
import { TrendCard } from "@/components/analytics/trend-card";
import { PageHeader } from "@/components/page-header";
import { BarList } from "@/components/reports/bar-list";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

const RANGES: Record<string, string> = {
  "7": "Last 7 days",
  "30": "Last 30 days",
  month: "This month",
  last_month: "Last month",
  "90": "Last 90 days",
  "180": "Last 6 months",
  "365": "Last 12 months",
};

function dayString(d: Date) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

function rangeDays(range: string): [string, string] {
  const now = new Date();
  if (range === "month") return [dayString(new Date(now.getFullYear(), now.getMonth(), 1)), dayString(now)];
  if (range === "last_month")
    return [dayString(new Date(now.getFullYear(), now.getMonth() - 1, 1)), dayString(new Date(now.getFullYear(), now.getMonth(), 0))];
  return [dayString(new Date(now.getTime() - (Number(range) - 1) * 864e5)), dayString(now)];
}

const pct = (n: number, of: number) => (of ? `${Math.round((n / of) * 100)}%` : "—");

/** "$1,234 · Tk 5,000": each currency's total, largest first. */
function money(totals: Record<string, number>) {
  const e = Object.entries(totals).filter(([, v]) => v);
  if (!e.length) return "—";
  return e
    .sort((a, b) => b[1] - a[1])
    .map(([cur, v]) => formatMoney(String(v), cur))
    .join(" · ");
}

/**
 * The change against the previous period, coloured by whether it's good news. up: is a
 * rise good (true), bad (false) or neither (null)?
 */
function Delta({ now, before, up, unit = "", fmt }: { now: number; before: number; up: boolean | null; unit?: string; fmt?: (n: number) => string }) {
  const diff = now - before;
  if (!Number.isFinite(diff)) return null;
  if (Math.abs(diff) < 1e-9) return <span className="text-muted-foreground">same as before</span>;
  const good = up === null ? null : diff > 0 === up;
  return (
    <span className={cn(good === true && "text-emerald-700 dark:text-emerald-400", good === false && "text-red-600 dark:text-red-400", good === null && "text-muted-foreground")}>
      {diff > 0 ? "▲" : "▼"} {fmt ? fmt(Math.abs(diff)) : Math.abs(Math.round(diff))}
      {unit} vs before
    </span>
  );
}

export function AnalyticsView() {
  const { data: me } = useMe();
  const router = useRouter();
  const pathname = usePathname();
  const sp = useSearchParams();
  const range = sp.get("range") && RANGES[sp.get("range")!] ? sp.get("range")! : "30";
  const teamId = sp.get("team");
  const month = /^\d{4}-\d{2}$/.test(sp.get("month") ?? "") ? sp.get("month")! : dayString(new Date()).slice(0, 7);
  const [from, to] = rangeDays(range);
  const allowed = !!me?.permissions.includes("oversight.view_analytics");
  const isCEO = me?.role === "ceo";
  const q = useAnalytics(from, to, teamId, allowed);
  const d = allowed ? q.data : undefined;

  const go = (patch: Record<string, string | null>) => {
    const next = new URLSearchParams(sp);
    for (const [k, v] of Object.entries(patch)) {
      if (v) next.set(k, v);
      else next.delete(k);
    }
    router.replace(`${pathname}?${next}`, { scroll: false });
  };

  if (me && !allowed) {
    return (
      <Empty className="min-h-[50vh]">
        <EmptyHeader>
          <EmptyTitle>Analytics is by invitation</EmptyTitle>
          <EmptyDescription>The CEO can give you access from the Users page.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  const t = d?.totals;
  const p = d?.previous_totals;
  const team = d?.teams.find((x) => x.id === teamId);
  const revenue = d ? sumRevenue(d.revenue) : {};
  const prevRevenue = d ? sumRevenue(d.previous_revenue) : {};
  const mainCur = Object.keys(revenue).sort((a, b) => revenue[b]! - revenue[a]!)[0] ?? "USD";
  const rate = (x: { on_time: number; delivered_with_due: number }) => (x.delivered_with_due ? (x.on_time / x.delivered_with_due) * 100 : NaN);
  const exportQ = new URLSearchParams({ from, to, ...(teamId ? { team_id: teamId } : {}) });

  return (
    <DrillProvider from={from} to={to} teamId={teamId}>
      <div className="space-y-6">
        <PageHeader
          title="Analytics"
          description={`What came in, what went out and what it earned ${team ? `for ${team.name} ` : ""}between ${formatDate(from)} and ${formatDate(to)}. ▲▼ compare with the same length of time just before${d ? ` (${formatDate(d.previous_range.from)} to ${formatDate(d.previous_range.to)})` : ""}. Click any underlined number to see its projects.`}
          actions={
            <>
              <Select value={teamId ?? "all"} onValueChange={(v) => go({ team: v === "all" ? null : v })}>
                <SelectTrigger className="w-44" aria-label="Team">
                  <SelectValue placeholder="All teams" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All teams</SelectItem>
                  {d?.teams.map((x) => (
                    <SelectItem key={x.id} value={x.id}>
                      {x.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select value={range} onValueChange={(v) => go({ range: v })}>
                <SelectTrigger className="w-40" aria-label="Period">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {Object.entries(RANGES).map(([value, label]) => (
                    <SelectItem key={value} value={value}>
                      {label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button variant="outline" asChild>
                <a href={`/api/analytics/export.csv?${exportQ}`}>
                  <DownloadIcon /> CSV
                </a>
              </Button>
            </>
          }
        />

        <div className={cn("grid grid-cols-2 gap-4 md:grid-cols-4", q.isFetching && q.isPlaceholderData && "opacity-60")}>
          <Stat icon={ArrowDownToLineIcon} label="Incoming" value={t && <DrillNumber metric="incoming" title="Orders that came in">{t.incoming}</DrillNumber>}
            sub={t && p && <Delta now={t.incoming} before={p.incoming} up={null} />} />
          <Stat icon={PackageCheckIcon} label="Delivered" value={t && <DrillNumber metric="delivered" title="Delivered">{t.delivered}</DrillNumber>}
            sub={t && p && <Delta now={t.delivered} before={p.delivered} up />} />
          <Stat icon={CheckCheckIcon} label="Completed" value={t && <DrillNumber metric="completed" title="Accepted by the client">{t.completed}</DrillNumber>}
            sub={t && p && <Delta now={t.completed} before={p.completed} up />} />
          <Stat icon={BanknoteIcon} label="Revenue delivered" value={d && money(revenue)}
            sub={d && <Delta now={revenue[mainCur] ?? 0} before={prevRevenue[mainCur] ?? 0} up fmt={(n) => formatMoney(String(n), mainCur)} />} />
          <Stat icon={TargetIcon} label="On time" value={t && <DrillNumber metric="late" title="Delivered late" className={t.delivered_with_due && t.on_time / t.delivered_with_due < 0.8 ? "text-red-600" : undefined}>{onTimeRate(t)}</DrillNumber>}
            sub={t && p && <Delta now={rate(t)} before={rate(p)} up unit=" pts" />} />
          <Stat icon={TimerIcon} label="Order → delivery" value={t && formatHours(t.turnaround_median_h, t.delivered)}
            sub={t && p && (t.delivered && p.delivered ? <Delta now={t.turnaround_median_h} before={p.turnaround_median_h} up={false} fmt={(n) => formatHours(n, 1)} /> : `median of ${t.delivered}`)} />
          <Stat icon={FolderKanbanIcon} label="Open now" value={t && <DrillNumber metric="open" title="Open now">{t.live_now}</DrillNumber>}
            sub={t && `${t.new_now} waiting to be assigned`} />
          <Stat icon={ClockAlertIcon} label="Overdue now" value={t && <DrillNumber metric="overdue" title="Overdue now" className={t.overdue_now ? "text-red-600" : undefined}>{t.overdue_now}</DrillNumber>}
            sub={t && (t.blocked_now ? `${t.blocked_now} blocked` : "nothing blocked")} />
        </div>

        <TargetsCard month={month} onMonth={(m) => go({ month: m })} isCEO={isCEO} teamId={teamId} />

        <Card>
          <CardHeader>
            <CardTitle>Incoming vs delivered</CardTitle>
            <CardDescription>
              Orders received and orders delivered, per {d?.bucket ?? "day"}. When incoming stays above delivered the backlog grows.
            </CardDescription>
          </CardHeader>
          <CardContent>{d ? <FlowChart series={d.series} bucket={d.bucket} /> : <Skeleton className="h-64" />}</CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Team performance</CardTitle>
            <CardDescription>
              Every team in the period. On time means delivered by the Team Leader&apos;s due date, or the client deadline if none. Click a team&apos;s name to focus the page on it.
            </CardDescription>
          </CardHeader>
          <CardContent className="overflow-x-auto">
            {d ? (
              d.teams.length ? (
                <Table>
                  <TableHeader>
                    <TableRow className="hover:bg-transparent">
                      <TableHead>Team</TableHead>
                      <TableHead className="text-right">Incoming</TableHead>
                      <TableHead className="w-36">Delivered</TableHead>
                      <TableHead className="text-right">Revenue</TableHead>
                      <TableHead className="text-right">Completed</TableHead>
                      <TableHead className="text-right">On time</TableHead>
                      <TableHead className="text-right">Avg order → delivery</TableHead>
                      <TableHead className="text-right">Needed revisions</TableHead>
                      <TableHead className="text-right">Open now</TableHead>
                      <TableHead className="text-right">Overdue now</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {d.teams.map((x) => (
                      <TeamRow
                        key={x.id}
                        team={x}
                        revenue={d.revenue}
                        max={Math.max(1, ...d.teams.map((y) => y.delivered))}
                        active={x.id === teamId}
                        onPick={() => go({ team: x.id === teamId ? null : x.id })}
                      />
                    ))}
                  </TableBody>
                </Table>
              ) : (
                <p className="text-sm text-muted-foreground">No teams yet.</p>
              )
            ) : (
              <Skeleton className="h-40" />
            )}
          </CardContent>
        </Card>

        {d ? <TimeCard d={d} /> : <Skeleton className="h-72 rounded-xl" />}

        <Card>
          <CardHeader>
            <CardTitle>People</CardTitle>
            <CardDescription>
              Team members and leaders{team ? ` in ${team.name}` : ""}. A delivery counts for everyone who was on the project.
            </CardDescription>
          </CardHeader>
          <CardContent className="overflow-x-auto">
            {d ? (
              d.people.length ? (
                <Table>
                  <TableHeader>
                    <TableRow className="hover:bg-transparent">
                      <TableHead>Person</TableHead>
                      <TableHead>Team</TableHead>
                      <TableHead className="w-36">Delivered</TableHead>
                      <TableHead className="text-right">Completed</TableHead>
                      <TableHead className="text-right">On time</TableHead>
                      <TableHead className="text-right">Avg order → delivery</TableHead>
                      <TableHead className="text-right">Needed revisions</TableHead>
                      <TableHead className="text-right">Open now</TableHead>
                      <TableHead className="text-right">Overdue now</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {d.people.map((x) => (
                      <PersonRow key={x.id} person={x} max={Math.max(1, ...d.people.map((y) => y.delivered))} />
                    ))}
                  </TableBody>
                </Table>
              ) : (
                <p className="text-sm text-muted-foreground">Nobody in this team yet.</p>
              )
            ) : (
              <Skeleton className="h-40" />
            )}
          </CardContent>
        </Card>

        <TrendCard teamId={teamId} />

        <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle>Incoming by project type</CardTitle>
              <CardDescription>What kind of work came in during the period.</CardDescription>
            </CardHeader>
            <CardContent>
              {d ? (
                <BarList
                  emptyText="Nothing came in."
                  rows={d.types
                    .filter((x) => x.incoming > 0)
                    .map((x) => ({
                      label: <DrillNumber metric="incoming" filter={{ type_id: x.id }} title={`${x.name}: orders that came in`} className="no-underline hover:underline">{x.name}</DrillNumber>,
                      value: x.incoming,
                      hint: `${x.name}: ${x.delivered} delivered, ${x.live_now} open now. Incoming`,
                    }))}
                />
              ) : (
                <Skeleton className="h-40" />
              )}
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>Orders by salesperson</CardTitle>
              <CardDescription>Orders each person brought in during the period, and where those orders are now.</CardDescription>
            </CardHeader>
            <CardContent className="overflow-x-auto">
              {d ? (
                d.sales.length ? (
                  <Table>
                    <TableHeader>
                      <TableRow className="hover:bg-transparent">
                        <TableHead>Sales</TableHead>
                        <TableHead className="text-right">Orders</TableHead>
                        <TableHead className="text-right">Delivered</TableHead>
                        <TableHead className="text-right">Completed</TableHead>
                        <TableHead className="text-right">Cancelled</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {d.sales.map((s) => (
                        <TableRow key={s.id}>
                          <TableCell className="font-medium">{s.name}</TableCell>
                          <TableCell className="text-right">
                            <DrillNumber metric="incoming" filter={{ sales_id: s.id }} title={`Orders from ${s.name}`}>{s.incoming}</DrillNumber>
                          </TableCell>
                          <TableCell className="text-right tabular-nums">{s.delivered}</TableCell>
                          <TableCell className="text-right tabular-nums">{s.completed}</TableCell>
                          <TableCell className={cn("text-right tabular-nums", !s.cancelled && "text-muted-foreground")}>
                            {s.cancelled ? <DrillNumber metric="cancelled" filter={{ sales_id: s.id }} title={`Cancelled orders from ${s.name}`}>{s.cancelled}</DrillNumber> : "—"}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                ) : (
                  <p className="text-sm text-muted-foreground">No orders in this period.</p>
                )
              ) : (
                <Skeleton className="h-40" />
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </DrillProvider>
  );
}

function DeliveredBar({ value, max, metric, filter, title }: { value: number; max: number; metric: Metric; filter: Record<string, string>; title: string }) {
  return (
    <div className="flex items-center gap-2">
      <div className="h-2.5 rounded-r-[4px] bg-flow-out" style={{ width: `${(value / max) * 70}%`, minWidth: value ? 4 : 0 }} />
      {value ? <DrillNumber metric={metric} filter={filter} title={title}>{value}</DrillNumber> : <span className="tabular-nums">0</span>}
    </div>
  );
}

function Perf({ p, filter, who }: { p: AnalyticsTeam | AnalyticsPerson; filter: Record<string, string>; who: string }) {
  return (
    <>
      <TableCell className="text-right">
        {p.delivered_with_due > p.on_time ? (
          <DrillNumber metric="late" filter={filter} title={`${who}: delivered late`}>{onTimeRate(p)}</DrillNumber>
        ) : (
          <span className="tabular-nums">{onTimeRate(p)}</span>
        )}
      </TableCell>
      <TableCell className="text-right tabular-nums">{formatHours(p.turnaround_avg_h, p.delivered)}</TableCell>
      <TableCell className="text-right">
        {p.delivered_with_revisions ? (
          <DrillNumber metric="revisions" filter={filter} title={`${who}: needed revisions`}>{pct(p.delivered_with_revisions, p.delivered)}</DrillNumber>
        ) : (
          <span className="tabular-nums">{pct(0, p.delivered)}</span>
        )}
      </TableCell>
    </>
  );
}

function TeamRow({ team: x, revenue, max, active, onPick }: { team: AnalyticsTeam; revenue: RevenueRow[]; max: number; active: boolean; onPick: () => void }) {
  const f = { team_id: x.id };
  return (
    <TableRow className={cn(active && "bg-muted/60")}>
      <TableCell>
        <button type="button" className="text-left font-medium hover:underline" onClick={onPick}>
          {x.name}
        </button>
        <div className="text-xs text-muted-foreground">
          {x.lead_name ? `Lead: ${x.lead_name}` : "No lead"} · {x.members + (x.lead_name ? 1 : 0)} people
        </div>
      </TableCell>
      <TableCell className="text-right">
        <DrillNumber metric="incoming" filter={f} title={`${x.name}: orders that came in`}>{x.incoming}</DrillNumber>
      </TableCell>
      <TableCell>
        <DeliveredBar value={x.delivered} max={max} metric="delivered" filter={f} title={`${x.name}: delivered`} />
      </TableCell>
      <TableCell className="text-right whitespace-nowrap tabular-nums">{money(sumRevenue(revenue, x.id))}</TableCell>
      <TableCell className="text-right tabular-nums">{x.completed}</TableCell>
      <Perf p={x} filter={f} who={x.name} />
      <TableCell className="text-right">
        <DrillNumber metric="open" filter={f} title={`${x.name}: open now`}>{x.live_now}</DrillNumber>
        {x.new_now > 0 && <div className="text-[11px] text-muted-foreground">{x.new_now} unassigned</div>}
      </TableCell>
      <TableCell className={cn("text-right", x.overdue_now > 0 && "font-medium text-red-600")}>
        {x.overdue_now ? <DrillNumber metric="overdue" filter={f} title={`${x.name}: overdue now`}>{x.overdue_now}</DrillNumber> : "—"}
      </TableCell>
    </TableRow>
  );
}

function PersonRow({ person: p, max }: { person: AnalyticsPerson; max: number }) {
  // A person's numbers are theirs on any team's projects.
  const f = { user_id: p.id, team_id: "" };
  return (
    <TableRow>
      <TableCell>
        <div className="font-medium">{p.name}</div>
        {p.role === "tl" && <div className="text-xs text-muted-foreground">Team Leader</div>}
      </TableCell>
      <TableCell className="text-muted-foreground">{p.team_name ?? "No team"}</TableCell>
      <TableCell>
        <DeliveredBar value={p.delivered} max={max} metric="delivered" filter={f} title={`${p.name}: delivered`} />
      </TableCell>
      <TableCell className="text-right tabular-nums">{p.completed}</TableCell>
      <Perf p={p} filter={f} who={p.name} />
      <TableCell className="text-right">
        <DrillNumber metric="member_open" filter={f} title={`${p.name}: open now`}>{p.open_now}</DrillNumber>
      </TableCell>
      <TableCell className={cn("text-right", p.overdue_now > 0 && "font-medium text-red-600")}>
        {p.overdue_now ? <DrillNumber metric="overdue" filter={f} title={`${p.name}: overdue now`}>{p.overdue_now}</DrillNumber> : "—"}
      </TableCell>
    </TableRow>
  );
}

function Stat({ icon: Icon, label, value, sub }: { icon: LucideIcon; label: string; value?: React.ReactNode; sub?: React.ReactNode }) {
  return (
    <Card className="h-full gap-1 py-4">
      <CardContent className="space-y-1 px-4">
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          {label}
          <Icon className="size-3.5" />
        </div>
        {value === undefined ? <Skeleton className="h-8 w-16" /> : <div className="text-2xl font-semibold tabular-nums">{value}</div>}
        {sub && <div className="text-[11px] text-muted-foreground">{sub}</div>}
      </CardContent>
    </Card>
  );
}
