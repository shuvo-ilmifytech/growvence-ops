"use client";

import { useEffect, useState } from "react";
import { PauseIcon, PlayIcon, TimerIcon, TrashIcon } from "lucide-react";
import {
  elapsedMinutes,
  formatDuration,
  useDeleteTimeEntry,
  useRunningTimer,
  useStartTimer,
  useStopTimer,
  useTimeEntries,
  type TimeEntry,
} from "@/lib/time";
import { useProjects } from "@/lib/projects";
import { useUserDirectory } from "@/lib/teams";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";

function fmtClock(iso: string): string {
  return new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function fmtDate(iso: string): string {
  return new Date(iso).toLocaleDateString([], { month: "short", day: "numeric" });
}

function TimerWidget() {
  const { data: running, isPending } = useRunningTimer();
  const { data: projects } = useProjects({ page_size: 100 });
  const start = useStartTimer();
  const stop = useStopTimer();
  const [projectId, setProjectId] = useState("");
  const [description, setDescription] = useState("");
  const [, setTick] = useState(0);

  const timer = running?.running ?? null;
  useEffect(() => {
    if (!timer) return;
    const id = setInterval(() => setTick((t) => t + 1), 30_000);
    return () => clearInterval(id);
  }, [timer?.id]);

  if (isPending) return <Skeleton className="h-40" />;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <TimerIcon className="size-4" /> {timer ? "Timer running" : "Start a timer"}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {timer ? (
          <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
            <div>
              <p className="text-3xl font-bold tabular-nums" aria-live="polite">
                {formatDuration(elapsedMinutes(timer.started_at))}
              </p>
              <p className="mt-1 text-sm text-muted-foreground">
                {timer.project_name ?? "No project"} · {timer.description || "No description"} · started {fmtClock(timer.started_at)}
              </p>
            </div>
            <Button size="lg" variant="destructive" onClick={() => stop.mutate()} disabled={stop.isPending}>
              <PauseIcon /> {stop.isPending ? "Stopping…" : "Stop"}
            </Button>
          </div>
        ) : (
          <div className="flex flex-col gap-3 md:flex-row md:items-end">
            <div className="grid flex-1 gap-1.5">
              <Label htmlFor="time-project">Project</Label>
              <Select value={projectId} onValueChange={setProjectId}>
                <SelectTrigger id="time-project">
                  <SelectValue placeholder="Select a project (optional)" />
                </SelectTrigger>
                <SelectContent>
                  {(projects?.projects ?? []).map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      #{p.number} {p.name || "Untitled"}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid flex-1 gap-1.5">
              <Label htmlFor="time-desc">What are you working on?</Label>
              <Input
                id="time-desc"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="e.g. Homepage hero section"
              />
            </div>
            <Button
              size="lg"
              onClick={() => start.mutate({ project_id: projectId || undefined, description })}
              disabled={start.isPending}
            >
              <PlayIcon /> {start.isPending ? "Starting…" : "Start"}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function EntryRow({ entry, canDelete }: { entry: TimeEntry; canDelete: boolean }) {
  const del = useDeleteTimeEntry();
  return (
    <tr className="border-b last:border-0">
      <td className="px-3 py-2 text-sm whitespace-nowrap">{fmtDate(entry.started_at)}</td>
      <td className="px-3 py-2 text-sm">{entry.project_name ?? <span className="text-muted-foreground">—</span>}</td>
      <td className="px-3 py-2 text-sm text-muted-foreground">{entry.description || "—"}</td>
      <td className="px-3 py-2 text-sm whitespace-nowrap tabular-nums">
        {fmtClock(entry.started_at)} – {entry.ended_at ? fmtClock(entry.ended_at) : "now"}
      </td>
      <td className="px-3 py-2 text-sm font-medium whitespace-nowrap tabular-nums">
        {entry.ended_at ? formatDuration(entry.duration_minutes) : `${formatDuration(elapsedMinutes(entry.started_at))} ●`}
      </td>
      {canDelete && (
        <td className="px-3 py-2 text-right">
          <Button variant="ghost" size="icon" aria-label="Delete entry" onClick={() => del.mutate(entry.id)} disabled={del.isPending}>
            <TrashIcon className="size-4" />
          </Button>
        </td>
      )}
    </tr>
  );
}

export default function TimePage() {
  const [projectId, setProjectId] = useState("");
  const [userId, setUserId] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const { data, isPending } = useTimeEntries({
    project_id: projectId || undefined,
    user_id: userId || undefined,
    from: from || undefined,
    to: to || undefined,
  });
  const { data: projects } = useProjects({ page_size: 100 });
  const { data: users } = useUserDirectory();

  const entries = data?.entries ?? [];
  const canViewAll = data?.can_view_all ?? false;

  return (
    <div className="space-y-6">
      <PageHeader title="Time tracking" description="Run a timer while you work, then review your timesheet." />

      <TimerWidget />

      <div className="grid gap-3 md:grid-cols-2">
        <Card>
          <CardContent className="pt-6">
            <p className="text-xs text-muted-foreground uppercase">Today</p>
            <p className="mt-1 text-2xl font-bold tabular-nums">{formatDuration(Number(data?.today_total ?? 0))}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <p className="text-xs text-muted-foreground uppercase">Last 7 days</p>
            <p className="mt-1 text-2xl font-bold tabular-nums">{formatDuration(Number(data?.week_total ?? 0))}</p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Timesheet</CardTitle>
          <div className="flex flex-col gap-2 pt-2 md:flex-row">
            <Select value={projectId} onValueChange={setProjectId}>
              <SelectTrigger className="md:w-56" aria-label="Filter by project">
                <SelectValue placeholder="All projects" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="">All projects</SelectItem>
                {(projects?.projects ?? []).map((p) => (
                  <SelectItem key={p.id} value={p.id}>
                    #{p.number} {p.name || "Untitled"}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {canViewAll && (
              <Select value={userId} onValueChange={setUserId}>
                <SelectTrigger className="md:w-56" aria-label="Filter by member">
                  <SelectValue placeholder="All members" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="">All members</SelectItem>
                  {(users ?? []).map((u) => (
                    <SelectItem key={u.id} value={u.id}>
                      {u.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
            <Input type="date" value={from} onChange={(e) => setFrom(e.target.value)} className="md:w-44" aria-label="From date" />
            <Input type="date" value={to} onChange={(e) => setTo(e.target.value)} className="md:w-44" aria-label="To date" />
          </div>
        </CardHeader>
        <CardContent>
          {isPending ? (
            <div className="space-y-2">
              {Array.from({ length: 5 }, (_, i) => (
                <Skeleton key={i} className="h-10 w-full" />
              ))}
            </div>
          ) : entries.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">No time entries yet. Start the timer above to log your first one.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full">
                <thead>
                  <tr className="border-b text-left text-xs text-muted-foreground uppercase">
                    <th className="px-3 py-2 font-medium">Date</th>
                    <th className="px-3 py-2 font-medium">Project</th>
                    <th className="px-3 py-2 font-medium">Description</th>
                    <th className="px-3 py-2 font-medium">Time</th>
                    <th className="px-3 py-2 font-medium">Duration</th>
                    <th className="px-3 py-2" />
                  </tr>
                </thead>
                <tbody>
                  {entries.map((e) => (
                    <EntryRow key={e.id} entry={e} canDelete />
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
