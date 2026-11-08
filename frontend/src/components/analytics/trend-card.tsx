"use client";

import { useState } from "react";
import { formatMoney } from "@/lib/format";
import { monthLabel, useTrend } from "@/lib/analytics";
import { cn } from "@/lib/utils";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

// Month by month: each team's revenue against its target, and each person's deliveries,
// shaded by how many (one hue, darker = more) so a slow month stands out.

export function TrendCard({ teamId }: { teamId: string | null }) {
  const [months, setMonths] = useState(6);
  const [view, setView] = useState<"teams" | "people">("teams");
  const q = useTrend(months, teamId);
  const d = q.data;
  const maxPerson = Math.max(1, ...(d?.people.flatMap((p) => Object.values(p.months)) ?? []));
  return (
    <Card>
      <CardHeader>
        <CardTitle>Month by month</CardTitle>
        <CardDescription>
          {view === "teams"
            ? "Each team's revenue delivered (after the fee) against its target, and how many projects it delivered."
            : "Projects each person delivered per month. Darker means more."}
        </CardDescription>
      </CardHeader>
      <CardContent className="overflow-x-auto">
        <div className="mb-4 flex flex-wrap items-center gap-2">
          <Tabs value={view} onValueChange={(v) => setView(v as "teams" | "people")}>
            <TabsList>
              <TabsTrigger value="teams">Teams</TabsTrigger>
              <TabsTrigger value="people">People</TabsTrigger>
            </TabsList>
          </Tabs>
          <Tabs value={String(months)} onValueChange={(v) => setMonths(Number(v))}>
            <TabsList>
              <TabsTrigger value="6">6 mo</TabsTrigger>
              <TabsTrigger value="12">12 mo</TabsTrigger>
            </TabsList>
          </Tabs>
        </div>
        {!d ? (
          <Skeleton className="h-40" />
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>{view === "teams" ? "Team" : "Person"}</TableHead>
                {d.months.map((m) => (
                  <TableHead key={m} className="text-right whitespace-nowrap">
                    {monthLabel(m)}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {view === "teams"
                ? d.teams.map((t) => (
                    <TableRow key={t.id}>
                      <TableCell className="font-medium">{t.name}</TableCell>
                      {d.months.map((m) => {
                        const c = t.months[m]!;
                        const cur = c.target?.currency ?? Object.keys(c.revenue)[0] ?? "USD";
                        const got = Number(c.revenue[cur] ?? 0);
                        const hit = c.target ? got >= Number(c.target.amount) : null;
                        return (
                          <TableCell key={m} className="text-right align-top whitespace-nowrap">
                            <div className="font-medium tabular-nums">{got ? formatMoney(String(got), cur) : "—"}</div>
                            {c.target && (
                              <div className={cn("text-[11px] tabular-nums", hit ? "text-emerald-700 dark:text-emerald-400" : "text-muted-foreground")}>
                                {Math.round((got / Number(c.target.amount)) * 100)}% of {formatMoney(c.target.amount, c.target.currency)}
                              </div>
                            )}
                            <div className="text-[11px] text-muted-foreground tabular-nums">
                              {c.delivered} delivered
                            </div>
                          </TableCell>
                        );
                      })}
                    </TableRow>
                  ))
                : d.people.map((p) => (
                    <TableRow key={p.id}>
                      <TableCell>
                        <div className="font-medium">{p.name}</div>
                        <div className="text-xs text-muted-foreground">{p.team_name ?? "No team"}</div>
                      </TableCell>
                      {d.months.map((m) => {
                        const n = p.months[m] ?? 0;
                        return (
                          <TableCell key={m} className="p-1 text-right">
                            <div
                              className="rounded-sm px-2 py-1.5 tabular-nums"
                              style={n ? { backgroundColor: `color-mix(in oklab, var(--fin-income) ${10 + (n / maxPerson) * 35}%, transparent)` } : undefined}
                            >
                              {n || <span className="text-muted-foreground">—</span>}
                            </div>
                          </TableCell>
                        );
                      })}
                    </TableRow>
                  ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}
