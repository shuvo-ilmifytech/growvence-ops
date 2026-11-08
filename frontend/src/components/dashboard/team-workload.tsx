"use client";

import Link from "next/link";
import { ArrowRightIcon, PlaneIcon, UsersRoundIcon } from "lucide-react";
import { useWorkload } from "@/lib/reports";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

/**
 * Who is carrying what, so a Team Leader can hand out the next project fairly.
 * A Team Leader sees their own team; the CEO sees everyone, team by team.
 */
export function TeamWorkload({ enabled = true }: { enabled?: boolean }) {
  const { data, isPending } = useWorkload(enabled);
  const members = data?.members ?? [];
  const title = data?.team ? `${data.team} team workload` : data?.scope === "company" ? "Workload across the company" : "Team workload";

  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>Open work per person right now, and what has slipped past its date.</CardDescription>
        <CardAction>
          <Button variant="ghost" size="sm" asChild>
            <Link href="/projects?view=all&status=submitted">
              Assign work <ArrowRightIcon />
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
        ) : members.length === 0 ? (
          <Empty className="border border-dashed py-8">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <UsersRoundIcon />
              </EmptyMedia>
              <EmptyTitle className="text-sm">No members yet</EmptyTitle>
              <EmptyDescription>
                {data?.scope === "team" ? "Nobody has been added to your team." : "No active members to show."}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <div className="-mx-2 overflow-x-auto px-2">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Person</TableHead>
                  {data?.scope === "company" && <TableHead>Team</TableHead>}
                  <TableHead className="text-right">Projects</TableHead>
                  <TableHead className="text-right">Late</TableHead>
                  <TableHead className="text-right">Blocked</TableHead>
                  <TableHead className="text-right">Cards</TableHead>
                  <TableHead className="text-right">Done (30d)</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {members.map((m) => (
                  <TableRow key={m.id}>
                    <TableCell className="font-medium">
                      <Link href={`/hr/people/${m.id}`} className="hover:underline">
                        {m.name}
                      </Link>
                      {m.on_leave_today && (
                        <Badge variant="outline" className="ml-2 gap-1 text-amber-600">
                          <PlaneIcon className="size-3" /> On leave
                        </Badge>
                      )}
                    </TableCell>
                    {data?.scope === "company" && (
                      <TableCell className="text-muted-foreground">{m.team_name ?? "No team"}</TableCell>
                    )}
                    <TableCell className="text-right tabular-nums">{m.open_projects}</TableCell>
                    <TableCell className={cn("text-right tabular-nums", m.overdue_projects && "font-medium text-red-600")}>
                      {m.overdue_projects || "—"}
                    </TableCell>
                    <TableCell className={cn("text-right tabular-nums", m.blocked_projects && "font-medium text-amber-600")}>
                      {m.blocked_projects || "—"}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {m.open_cards}
                      {m.late_cards > 0 && <span className="ml-1 text-xs text-red-600">({m.late_cards} late)</span>}
                    </TableCell>
                    <TableCell className="text-right tabular-nums text-muted-foreground">{m.completed_30d}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
