"use client";

import { AwardIcon, ChartColumnIcon, MedalIcon, StarIcon, TrendingUpIcon } from "lucide-react";
import { PageHeader } from "@/components/page-header";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useLeaderboard, useRevenueForecast, type ForecastMonth } from "@/lib/insights";
import { formatMoney } from "@/lib/format";
import { cn } from "@/lib/utils";

const MONTH_NAMES = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

function monthLabel(ym: string) {
  const [y, m] = ym.split("-").map(Number);
  return `${MONTH_NAMES[m - 1]} ${y}`;
}

function ForecastChart({ months }: { months: ForecastMonth[] }) {
  const max = Math.max(1, ...months.map((m) => m.projected));
  return (
    <div className="flex h-48 items-end gap-2 sm:gap-3">
      {months.map((m) => (
        <div key={m.month} className="flex min-w-0 flex-1 flex-col items-center gap-1.5">
          <div className="flex h-36 w-full items-end rounded-md bg-muted/60 p-1">
            <div
              className="w-full rounded-sm bg-primary transition-all"
              style={{ height: `${Math.max(3, (m.projected / max) * 100)}%` }}
              title={`${monthLabel(m.month)}: ${formatMoney(String(m.projected), m.currency)} (${m.count} projects)`}
            />
          </div>
          <div className="text-center">
            <p className="text-[11px] font-medium">{monthLabel(m.month).split(" ")[0]}</p>
            <p className="text-[10px] text-muted-foreground tabular-nums">{m.projected > 0 ? formatMoney(String(m.projected), m.currency) : "—"}</p>
          </div>
        </div>
      ))}
    </div>
  );
}

const MEDALS = ["text-amber-500", "text-slate-400", "text-amber-700"];

function RankCell({ rank }: { rank: number }) {
  if (rank < 3) return <MedalIcon className={cn("size-5", MEDALS[rank])} aria-label={`Rank ${rank + 1}`} />;
  return <span className="text-sm text-muted-foreground tabular-nums">{rank + 1}</span>;
}

export default function InsightsPage() {
  const forecast = useRevenueForecast();
  const board = useLeaderboard();

  return (
    <div className="space-y-6">
      <PageHeader
        title="Insights"
        description="Projected revenue from the active pipeline and how the team is performing."
      />

      <div className="grid gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <ChartColumnIcon className="size-5" /> Revenue forecast
            </CardTitle>
            <CardDescription>Projected revenue by expected completion month, from active projects.</CardDescription>
          </CardHeader>
          <CardContent>
            {forecast.isPending ? (
              <Skeleton className="h-48 w-full" />
            ) : forecast.isError ? (
              <p className="py-8 text-center text-sm text-muted-foreground">Couldn't load the forecast.</p>
            ) : (
              <ForecastChart months={forecast.data.months} />
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <TrendingUpIcon className="size-5" /> Pipeline
            </CardTitle>
            <CardDescription>Total value of active projects.</CardDescription>
          </CardHeader>
          <CardContent>
            {forecast.isPending ? (
              <Skeleton className="h-16 w-2/3" />
            ) : forecast.isError ? (
              <p className="text-sm text-muted-foreground">—</p>
            ) : (
              <>
                <p className="text-3xl font-bold tabular-nums">
                  {formatMoney(String(forecast.data.total_pipeline), forecast.data.currency)}
                </p>
                <p className="mt-1 text-sm text-muted-foreground">
                  across {forecast.data.months.reduce((n, m) => n + m.count, 0)} active priced projects
                </p>
              </>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <AwardIcon className="size-5" /> Performance leaderboard
          </CardTitle>
          <CardDescription>Members and team leads ranked by completed projects.</CardDescription>
        </CardHeader>
        <CardContent>
          {board.isPending ? (
            <div className="space-y-2">
              {Array.from({ length: 5 }, (_, i) => (
                <Skeleton key={i} className="h-10 w-full" />
              ))}
            </div>
          ) : board.isError ? (
            <p className="py-8 text-center text-sm text-muted-foreground">Couldn't load the leaderboard.</p>
          ) : board.data.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">No members yet.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-12">Rank</TableHead>
                  <TableHead>Member</TableHead>
                  <TableHead className="text-right">Completed</TableHead>
                  <TableHead className="text-right">On-time</TableHead>
                  <TableHead className="text-right">Active</TableHead>
                  <TableHead className="text-right">Avg rating</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {board.data.map((r, i) => (
                  <TableRow key={r.user_id}>
                    <TableCell>
                      <RankCell rank={i} />
                    </TableCell>
                    <TableCell>
                      <span className="font-medium">{r.name}</span>
                      <span className="ml-2 text-xs text-muted-foreground uppercase">{r.role}</span>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">{r.completed}</TableCell>
                    <TableCell className="text-right tabular-nums">
                      {r.on_time_pct === null ? "—" : `${r.on_time_pct.toFixed(0)}%`}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">{r.active}</TableCell>
                    <TableCell className="text-right tabular-nums">
                      {r.avg_rating === null ? (
                        "—"
                      ) : (
                        <span className="inline-flex items-center gap-1">
                          <StarIcon className="size-3.5 fill-amber-400 text-amber-400" />
                          {r.avg_rating.toFixed(1)}
                        </span>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
