"use client";

import Link from "next/link";
import { useMemo } from "react";
import {
  AlarmClockIcon,
  ArrowRightIcon,
  CalendarCheck2Icon,
  CheckCheckIcon,
  FolderKanbanIcon,
  KanbanIcon,
  MegaphoneIcon,
  type LucideIcon,
} from "lucide-react";
import { useAllCards } from "@/lib/cards";
import { useProjects } from "@/lib/projects";
import { useUpdates } from "@/lib/updates";
import { useHrMe } from "@/lib/hr";
import { useHydrated, useMe } from "@/lib/session";
import { clock } from "@/lib/hr";
import { formatDate } from "@/lib/format";
import { CARD_COLUMN_LABELS, projectRoleOf, type AllCard, type ProjectRole, type ProjectStatus, type UpdateStatus } from "@/lib/types";
import { cn } from "@/lib/utils";
import { PageHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/project-badges";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";

/** HR run people, not projects, so they stay on their own home. */

// Which of my projects count as "on my plate", per role.
const MINE: Record<ProjectRole, { statuses: ProjectStatus[]; title: string; empty: string }> = {
  sales: { statuses: ["ready_to_deliver", "delivered"], title: "Projects to deliver or close", empty: "Nothing to deliver." },
  tl: { statuses: ["new", "ready_for_review", "blocked"], title: "Projects waiting on you", empty: "Nothing to assign or check." },
  member: { statuses: ["in_progress", "blocked"], title: "Projects you're working on", empty: "Nothing assigned to you." },
  ceo: { statuses: ["blocked", "new"], title: "Projects that need a push", empty: "Nothing stuck." },
};

const UPDATES: Record<ProjectRole, { label: string; view: string; statuses: UpdateStatus[] }> = {
  sales: { label: "Approved updates to send to clients", view: "send", statuses: ["approved"] },
  tl: { label: "Updates waiting for your approval", view: "approve", statuses: ["pending_approval"] },
  member: { label: "Updates to write or fix", view: "work", statuses: ["draft", "changes_requested"] },
  ceo: { label: "Approved updates not yet sent", view: "unsent", statuses: ["approved"] },
};

const CARD_TITLE: Record<ProjectRole, string> = {
  sales: "Client requests you logged",
  tl: "Cards assigned to you",
  member: "Your cards",
  ceo: "Cards assigned to you",
};

type Bucket = { key: string; label: string; tone?: string; icon?: LucideIcon };
const BUCKETS: Bucket[] = [
  { key: "late", label: "Late", tone: "text-red-600" },
  { key: "today", label: "Due today", tone: "text-amber-600" },
  { key: "week", label: "Next 7 days" },
  { key: "later", label: "Later" },
  { key: "none", label: "No date" },
];

/** Which bucket a card falls in, by its due day in the viewer's own timezone. */
function bucketOf(due: string | null, today: Date): string {
  if (!due) return "none";
  const day = new Date(`${due.slice(0, 10)}T00:00:00`);
  const diff = Math.round((day.getTime() - today.getTime()) / 864e5);
  if (diff < 0) return "late";
  if (diff === 0) return "today";
  return diff <= 7 ? "week" : "later";
}

export default function MyWorkPage() {
  const { data: me } = useMe();
  const role = projectRoleOf(me?.role);
  const mine = role ? MINE[role] : undefined;
  const cards = useAllCards({ mine: true });
  const projects = useProjects({ status: mine?.statuses.join(","), assignee: role === "member" ? me?.id : undefined, sort: "due", page_size: 8 });
  const updates = useUpdates({ page_size: 1 }, !!me);
  const hr = useHrMe(!!me && !!projectRoleOf(me.role));

  // Open = anything not in Done, on any project still on the board, so this page and
  // the board never disagree about what's left.
  const open = useMemo(() => (cards.data?.cards ?? []).filter((c) => c.column_key !== "done"), [cards.data]);
  // "Today" is the viewer's own day, read in the browser so the page stays prerenderable.
  const hydrated = useHydrated();
  const today = useMemo(() => {
    if (!hydrated) return null;
    const n = new Date();
    return new Date(n.getFullYear(), n.getMonth(), n.getDate());
  }, [hydrated]);
  const grouped = useMemo(() => {
    const out: Record<string, AllCard[]> = {};
    if (!today) return out;
    for (const c of open) (out[bucketOf(c.due_date, today)] ??= []).push(c);
    return out;
  }, [open, today]);

  const queue = role ? UPDATES[role] : undefined;
  const waiting = queue ? queue.statuses.reduce((n, s) => n + (updates.data?.counts[s] ?? 0), 0) : 0;
  const late = grouped.late?.length ?? 0;
  const tz = hr.data?.schedule?.timezone ?? "UTC";
  const checkedIn = hr.data?.today_record?.check_in_at;
  const pendingLeave = hr.data?.pending_leave ?? 0;

  return (
    <div className="space-y-6">
      <PageHeader
        title="My work"
        description="Everything on your plate in one place: your cards by when they&rsquo;re due, your projects, and what's waiting for you elsewhere."
      />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Tile label="Cards open" value={open.length} icon={KanbanIcon} href="/board" />
        <Tile label="Late" value={late} icon={AlarmClockIcon} tone={late ? "text-red-600" : undefined} href="/board" />
        <Tile label={queue ? "Updates waiting" : "Updates"} value={waiting} icon={MegaphoneIcon} href={`/updates?view=${queue?.view ?? "all"}`} />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{role ? CARD_TITLE[role] : "Cards"}</CardTitle>
          <CardDescription>Grouped by when they&rsquo;re due.</CardDescription>
          <CardAction>
            <Button variant="ghost" size="sm" asChild>
              <Link href="/board">
                Open the board <ArrowRightIcon />
              </Link>
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent className="space-y-5">
          {cards.isPending || !today ? (
            <div className="space-y-2">
              {Array.from({ length: 4 }, (_, i) => (
                <Skeleton key={i} className="h-12" />
              ))}
            </div>
          ) : open.length === 0 ? (
            <Empty className="border border-dashed py-8">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <CheckCheckIcon />
                </EmptyMedia>
                <EmptyTitle className="text-sm">All clear</EmptyTitle>
                <EmptyDescription>No cards on your plate right now.</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            BUCKETS.filter((b) => grouped[b.key]?.length).map((b) => (
              <section key={b.key} className="space-y-1">
                <h3 className={cn("px-1 text-xs font-medium tracking-wide uppercase", b.tone ?? "text-muted-foreground")}>
                  {b.label}
                  <span className="ml-1.5 tabular-nums opacity-70">{grouped[b.key].length}</span>
                </h3>
                <ul className="-mx-2 divide-y">
                  {grouped[b.key].map((c) => (
                    <li key={c.id}>
                      <Link
                        href={c.client_request_id ? `/requests/${c.client_request_id}?card=${c.id}` : `/projects/${c.project_id}?tab=board&card=${c.id}`}
                        className="flex items-center gap-3 rounded-md px-2 py-2 hover:bg-muted/60"
                      >
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-sm font-medium">{c.title}</div>
                          <div className="mt-0.5 truncate text-xs text-muted-foreground">
                            {c.client_request_id ? "CR-" : "#"}
                            {c.project_number} {c.project_name ?? "Untitled"}
                            {c.source === "client" && <span className="ml-1.5">· from the client</span>}
                          </div>
                        </div>
                        <span className="shrink-0 text-xs text-muted-foreground">{CARD_COLUMN_LABELS[c.column_key]}</span>
                        <span className={cn("shrink-0 text-xs tabular-nums", b.key === "late" ? "font-medium text-red-600" : "text-muted-foreground")}>
                          {c.due_date ? formatDate(c.due_date) : "—"}
                        </span>
                      </Link>
                    </li>
                  ))}
                </ul>
              </section>
            ))
          )}
        </CardContent>
      </Card>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>{mine?.title ?? "Your projects"}</CardTitle>
            <CardAction>
              <Button variant="ghost" size="sm" asChild>
                <Link href="/projects">
                  View all <ArrowRightIcon />
                </Link>
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            {projects.isPending ? (
              <div className="space-y-2">
                {Array.from({ length: 3 }, (_, i) => (
                  <Skeleton key={i} className="h-12" />
                ))}
              </div>
            ) : (projects.data?.projects.length ?? 0) === 0 ? (
              <Empty className="border border-dashed py-8">
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <FolderKanbanIcon />
                  </EmptyMedia>
                  <EmptyTitle className="text-sm">All clear</EmptyTitle>
                  <EmptyDescription>{mine?.empty ?? "Nothing here."}</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <ul className="-mx-2 divide-y">
                {projects.data?.projects.map((p) => (
                  <li key={p.id}>
                    <Link href={`/projects/${p.id}`} className="flex items-center gap-3 rounded-md px-2 py-2.5 hover:bg-muted/60">
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm font-medium">
                          <span className="mr-1.5 font-mono text-xs text-muted-foreground">#{p.number}</span>
                          {p.name ?? "Untitled"}
                        </div>
                        <div className={cn("mt-0.5 text-xs", p.is_overdue ? "font-medium text-red-600" : "text-muted-foreground")}>
                          Due {formatDate(p.effective_due)}
                        </div>
                      </div>
                      <StatusBadge status={p.status} />
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>

        <div className="space-y-6">
          {queue && (
            <Link href={`/updates?view=${queue.view}`} className="group block">
              <Card className="flex-row items-center gap-4 px-6 transition group-hover:border-primary/40">
                <div className="flex size-10 items-center justify-center rounded-lg bg-accent text-accent-foreground">
                  <MegaphoneIcon className="size-5" />
                </div>
                <div className="flex-1 text-sm font-medium">{queue.label}</div>
                <div className="text-2xl font-semibold tabular-nums">{waiting}</div>
                <ArrowRightIcon className="size-4 text-muted-foreground" />
              </Card>
            </Link>
          )}
          <Link href="/hr" className="group block">
            <Card className="flex-row items-center gap-4 px-6 transition group-hover:border-primary/40">
              <div className="flex size-10 items-center justify-center rounded-lg bg-accent text-accent-foreground">
                <CalendarCheck2Icon className="size-5" />
              </div>
              <div className="flex-1">
                <div className="text-sm font-medium">
                  {checkedIn ? `Checked in at ${clock(checkedIn, tz)}` : hr.data?.is_working_day === false ? "Not a working day" : "Not checked in yet"}
                </div>
                <div className="text-xs text-muted-foreground">
                  {pendingLeave > 0
                    ? `${pendingLeave} leave request${pendingLeave > 1 ? "s" : ""} awaiting a decision`
                    : "Attendance and leave"}
                </div>
              </div>
              <ArrowRightIcon className="size-4 text-muted-foreground" />
            </Card>
          </Link>
        </div>
      </div>
    </div>
  );
}

function Tile({ label, value, icon: Icon, tone, href }: { label: string; value: number; icon: LucideIcon; tone?: string; href: string }) {
  return (
    <Link href={href} className="group">
      <Card className="h-full gap-2 transition group-hover:border-primary/40 group-hover:shadow-sm">
        <CardHeader>
          <CardDescription>{label}</CardDescription>
          <CardAction>
            <Icon className={cn("size-4 text-muted-foreground", tone)} />
          </CardAction>
        </CardHeader>
        <CardContent>
          <div className={cn("text-3xl font-semibold tabular-nums", tone)}>{value}</div>
        </CardContent>
      </Card>
    </Link>
  );
}
