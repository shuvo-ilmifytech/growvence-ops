"use client";

import { createContext, useContext, useState } from "react";
import Link from "next/link";
import { formatDate, formatMoney } from "@/lib/format";
import { useAnalyticsProjects, type DrillFilter, type Metric } from "@/lib/analytics";
import { formatHours } from "@/lib/reports";
import { cn } from "@/lib/utils";
import { StatusBadge } from "@/components/project-badges";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

// Every number on the Analytics page opens the projects behind it. The page provides the
// range; a number adds what it counts (the metric) and who for (team, person, type, sales).

type Drill = { metric: Metric; filter: DrillFilter; title: string };
type Ctx = { from: string; to: string; teamId: string | null; open: (d: Drill) => void };

const DrillContext = createContext<Ctx | null>(null);

export function DrillProvider({ from, to, teamId, children }: { from: string; to: string; teamId: string | null; children: React.ReactNode }) {
  const [drill, setDrill] = useState<Drill | null>(null);
  return (
    <DrillContext.Provider value={{ from, to, teamId, open: setDrill }}>
      {children}
      {drill && <DrillDialog drill={drill} from={from} to={to} onClose={() => setDrill(null)} />}
    </DrillContext.Provider>
  );
}

/** A number that opens its projects. The page's team filter applies unless the filter names one. */
export function DrillNumber({
  metric,
  filter = {},
  title,
  children,
  className,
}: {
  metric: Metric;
  filter?: DrillFilter;
  title: string;
  children: React.ReactNode;
  className?: string;
}) {
  const ctx = useContext(DrillContext);
  if (!ctx) return <>{children}</>;
  const f = { ...(ctx.teamId ? { team_id: ctx.teamId } : {}), ...filter };
  return (
    <button
      type="button"
      className={cn("cursor-pointer rounded-sm tabular-nums underline decoration-dotted decoration-muted-foreground/50 underline-offset-4 hover:decoration-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none", className)}
      onClick={(e) => {
        e.stopPropagation();
        ctx.open({ metric, filter: f, title });
      }}
    >
      {children}
    </button>
  );
}

// "Now" lists ignore the period, so the dialog says so.
const NOW: Metric[] = ["open", "member_open", "overdue", "awaiting_client", "new", "in_progress", "blocked", "ready_for_review", "ready_to_deliver"];

function lateText(h: number | null) {
  if (h === null) return "—";
  if (h <= 0) return "on time";
  return `${formatHours(h, 1)} late`;
}

function DrillDialog({ drill, from, to, onClose }: { drill: Drill; from: string; to: string; onClose: () => void }) {
  const q = useAnalyticsProjects(drill.metric, from, to, drill.filter);
  const rows = q.data;
  const delivered = rows?.some((p) => p.delivered_at);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-5xl">
        <DialogHeader>
          <DialogTitle>{drill.title}</DialogTitle>
          <DialogDescription>
            {rows ? `${rows.length} project${rows.length === 1 ? "" : "s"}` : "Loading…"}
            {NOW.includes(drill.metric) ? ", as of now." : `, ${formatDate(from)} to ${formatDate(to)}.`}
          </DialogDescription>
        </DialogHeader>
        {!rows ? (
          <Skeleton className="h-48" />
        ) : rows.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">No projects.</p>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Project</TableHead>
                  <TableHead>Team · worked on by</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>{delivered ? "Delivered" : "Received"}</TableHead>
                  <TableHead>Due</TableHead>
                  <TableHead className="text-right">On time?</TableHead>
                  <TableHead className="text-right">Revisions</TableHead>
                  <TableHead className="text-right">Net</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((p) => (
                  <TableRow key={p.id}>
                    <TableCell className="max-w-64">
                      <Link href={`/projects/${p.id}`} className="block truncate font-medium hover:underline">
                        <span className="mr-1.5 font-mono text-xs text-muted-foreground">#{p.number}</span>
                        {p.name}
                      </Link>
                      <div className="truncate text-xs text-muted-foreground">
                        {p.client_name} · {p.type_name ?? "No type"} · {p.sales_name}
                      </div>
                    </TableCell>
                    <TableCell className="text-sm">
                      {p.team_name ?? "No team"}
                      <div className="text-xs text-muted-foreground">{p.members || "Nobody yet"}</div>
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={p.status} />
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-sm">
                      {formatDate(p.delivered_at ?? p.first_submitted_at)}
                      {p.turnaround_h !== null && <div className="text-xs text-muted-foreground">after {formatHours(p.turnaround_h, 1)}</div>}
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-sm">{p.due ? formatDate(p.due) : "—"}</TableCell>
                    <TableCell className={cn("text-right text-sm whitespace-nowrap", p.late_h !== null && p.late_h > 0 && "font-medium text-red-600")}>{lateText(p.late_h)}</TableCell>
                    <TableCell className="text-right tabular-nums">{p.revision_count || "—"}</TableCell>
                    <TableCell className="text-right whitespace-nowrap tabular-nums">{p.net ? formatMoney(p.net, p.currency) : "—"}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
