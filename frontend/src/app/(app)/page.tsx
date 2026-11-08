"use client";

import Link from "next/link";
import { useEffect } from "react";
import { useRouter } from "next/navigation";
import {
  AlertOctagonIcon,
  ArrowRightIcon,
  ClockAlertIcon,
  FolderKanbanIcon,
  InboxIcon,
  ListChecksIcon,
  MegaphoneIcon,
  PackageCheckIcon,
  PlayIcon,
  PlusIcon,
  SendIcon,
  type LucideIcon,
} from "lucide-react";
import { useProjects } from "@/lib/projects";
import { useUpdates } from "@/lib/updates";
import { can, useMe } from "@/lib/session";
import { formatDate, relativeTime } from "@/lib/format";
import { projectRoleOf, type Project, type ProjectRole, type ProjectStatus, type Role, type UpdateStatus } from "@/lib/types";
import { PriorityLabel, StatusBadge } from "@/components/project-badges";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { HrHome } from "@/components/hr/hr-home";
import { OfficeHome } from "@/components/finance/office-home";
import { useHrMe } from "@/lib/hr";
import { CompanyOverview } from "@/components/dashboard/company-overview";
import { TeamWorkload } from "@/components/dashboard/team-workload";
import { WhosOut } from "@/components/dashboard/whos-out";

const ROLE_INTRO: Record<Role, string> = {
  sales:
    "Create projects, keep their details up to date and send approved updates to clients.",
  tl: "Review new projects, assign them to the team and approve updates before they go out.",
  member: "Work on the projects assigned to you and write updates for review.",
  ceo: "See every project, update and report across the team.",
  hr: "Look after people, attendance and leave.",
  office_admin: "Raise an invoice for what the office needs; HR and the CEO approve it.",
  client: "See your projects, their progress and updates.",
  investor: "See your share, earnings and withdrawals.",
};


type Stat = {
  label: string;
  icon: LucideIcon;
  statuses?: ProjectStatus[];
  overdue?: boolean;
  tone?: string;
};

const ACTIVE: ProjectStatus[] = ["new", "in_progress", "blocked", "ready_for_review", "ready_to_deliver"];

const STATS: Record<ProjectRole, Stat[]> = {
  sales: [
    { label: "To deliver on Fiverr", icon: SendIcon, statuses: ["ready_to_deliver"], tone: "text-amber-600" },
    { label: "In the works", icon: PlayIcon, statuses: ["new", "in_progress", "blocked", "ready_for_review"] },
    { label: "Delivered, waiting on client", icon: PackageCheckIcon, statuses: ["delivered"] },
    { label: "Completed", icon: ListChecksIcon, statuses: ["completed"] },
  ],
  tl: [
    {
      label: "Waiting to be assigned",
      icon: InboxIcon,
      statuses: ["new"],
      tone: "text-sky-600",
    },
    {
      label: "To check before delivery",
      icon: ListChecksIcon,
      statuses: ["ready_for_review"],
      tone: "text-violet-600",
    },
    {
      label: "Blocked",
      icon: AlertOctagonIcon,
      statuses: ["blocked"],
      tone: "text-red-600",
    },
    {
      label: "Overdue",
      icon: ClockAlertIcon,
      overdue: true,
      tone: "text-red-600",
    },
  ],
  member: [
    { label: "In progress", icon: PlayIcon, statuses: ["in_progress"] },
    {
      label: "Blocked",
      icon: AlertOctagonIcon,
      statuses: ["blocked"],
      tone: "text-red-600",
    },
    { label: "In review", icon: ListChecksIcon, statuses: ["ready_for_review"] },
    { label: "Ready to deliver", icon: SendIcon, statuses: ["ready_to_deliver"] },
  ],
  ceo: [
    { label: "Active projects", icon: FolderKanbanIcon, statuses: ACTIVE },
    {
      label: "Unassigned",
      icon: InboxIcon,
      statuses: ["new"],
      tone: "text-sky-600",
    },
    {
      label: "Blocked",
      icon: AlertOctagonIcon,
      statuses: ["blocked"],
      tone: "text-red-600",
    },
    {
      label: "Overdue",
      icon: ClockAlertIcon,
      overdue: true,
      tone: "text-red-600",
    },
  ],
};

const ATTENTION: Record<
  ProjectRole,
  {
    title: string;
    empty: string;
    statuses?: ProjectStatus[];
    overdue?: boolean;
  }
> = {
  sales: {
    title: "Ready to deliver on Fiverr",
    empty: "Nothing to deliver right now.",
    statuses: ["ready_to_deliver"],
  },
  tl: {
    title: "Waiting on you",
    empty: "No projects to assign or check.",
    statuses: ["new", "ready_for_review", "blocked"],
  },
  member: {
    title: "Your work",
    empty: "No projects assigned to you right now.",
    statuses: ["in_progress", "blocked"],
  },
  ceo: {
    title: "Overdue projects",
    empty: "Nothing is overdue.",
    overdue: true,
  },
};

const UPDATE_QUEUE: Record<
  ProjectRole,
  { label: string; hint: string; view: string; statuses: UpdateStatus[] }
> = {
  tl: {
    label: "Updates waiting for your approval",
    hint: "Approve or send back what the team wrote.",
    view: "approve",
    statuses: ["pending_approval"],
  },
  sales: {
    label: "Approved updates to send",
    hint: "Post them on the marketplace, then mark them as sent.",
    view: "send",
    statuses: ["approved"],
  },
  member: {
    label: "Updates that need your work",
    hint: "Drafts and updates sent back by the Team Leader.",
    view: "work",
    statuses: ["draft", "changes_requested"],
  },
  ceo: {
    label: "Approved updates not yet sent",
    hint: "Waiting on Sales to post them.",
    view: "unsent",
    statuses: ["approved"],
  },
};

function greeting() {
  const h = new Date().getHours();
  return h < 12 ? "Good morning" : h < 17 ? "Good afternoon" : "Good evening";
}

function href(s: { statuses?: ProjectStatus[]; overdue?: boolean }) {
  const p = new URLSearchParams({ view: "all" });
  if (s.statuses) p.set("status", s.statuses.join(","));
  if (s.overdue) p.set("overdue", "true");
  return `/projects?${p}`;
}

export default function DashboardPage() {
  const { data: me } = useMe();
  const router = useRouter();
  // Clients get their own portal, not the internal dashboard.
  useEffect(() => {
    if (me?.role === "client") router.replace("/client");
    if (me?.role === "investor") router.replace("/investor");
  }, [me, router]);
  // Counts by status come with any list call; one extra call for the overdue total.
  const all = useProjects({ page_size: 1 });
  const overdue = useProjects({ overdue: true, page_size: 1 });
  const role = projectRoleOf(me?.role);
  const attention = role ? ATTENTION[role] : undefined;
  const focus = useProjects({
    status: attention?.statuses?.join(","),
    overdue: attention?.overdue,
    sort: "due",
    page_size: 6,
  });
  const recent = useProjects({ sort: "activity", page_size: 6 });
  const updateCounts = useUpdates({ page_size: 1 }, !!me).data?.counts;
  const queue = role ? UPDATE_QUEUE[role] : undefined;
  const manager = useHrMe(!!me && !!projectRoleOf(me.role)).data?.employee;
  const isCeo = role === "ceo";

  if (me?.role === "hr") return <HrHome name={me.name} />;
  if (me?.role === "office_admin") return <OfficeHome name={me.name} />;

  const count = (s: Stat) =>
    s.overdue
      ? overdue.data?.total
      : s.statuses?.reduce((n, st) => n + (all.data?.counts[st] ?? 0), 0);

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="space-y-1">
          {me ? (
            <>
              <h1 className="text-2xl font-semibold tracking-tight">
                {greeting()}, {me.name.split(" ")[0]}
              </h1>
              <p className="text-sm text-muted-foreground">
                {ROLE_INTRO[me.role]}
              </p>
              {manager?.manager_id && (
                <p className="text-sm">
                  <span className="text-muted-foreground">You report to </span>
                  <Link
                    href={`/hr/people/${manager.manager_id}`}
                    className="font-medium hover:underline"
                  >
                    {manager.manager_name}
                  </Link>
                </p>
              )}
            </>
          ) : (
            <>
              <Skeleton className="h-8 w-64" />
              <Skeleton className="h-4 w-96 max-w-full" />
            </>
          )}
        </div>
        {can(me, "project.create") && (
          <Button asChild>
            <Link href="/projects/new">
              <PlusIcon /> New project
            </Link>
          </Button>
        )}
      </div>

      {isCeo && <CompanyOverview />}

      {!isCeo && (
        <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
          {(role ? STATS[role] : Array.from({ length: 4 }, () => null)).map(
            (s, i) =>
              s ? (
                <Link key={s.label} href={href(s)} className="group">
                  <Card className="h-full gap-2 transition group-hover:border-primary/40 group-hover:shadow-sm">
                    <CardHeader>
                      <CardDescription>{s.label}</CardDescription>
                      <CardAction>
                        <s.icon
                          className={cn("size-4 text-muted-foreground", s.tone)}
                        />
                      </CardAction>
                    </CardHeader>
                    <CardContent>
                      {count(s) === undefined ? (
                        <Skeleton className="h-9 w-10" />
                      ) : (
                        <div className="text-3xl font-semibold tabular-nums">
                          {count(s)}
                        </div>
                      )}
                    </CardContent>
                  </Card>
                </Link>
              ) : (
                <Skeleton key={i} className="h-28 rounded-xl" />
              ),
          )}
        </div>
      )}

      {queue && updateCounts && (
        <Link href={`/updates?view=${queue.view}`} className="group block">
          <Card className="flex-row items-center gap-4 px-6 transition group-hover:border-primary/40">
            <div className="flex size-10 items-center justify-center rounded-lg bg-accent text-accent-foreground">
              <MegaphoneIcon className="size-5" />
            </div>
            <div className="flex-1">
              <div className="text-sm font-medium">{queue.label}</div>
              <div className="text-xs text-muted-foreground">{queue.hint}</div>
            </div>
            <div className="text-2xl font-semibold tabular-nums">
              {queue.statuses.reduce((n, s) => n + (updateCounts[s] ?? 0), 0)}
            </div>
            <ArrowRightIcon className="size-4 text-muted-foreground" />
          </Card>
        </Link>
      )}

      {(role === "tl" || role === "ceo") && (
        <>
          <TeamWorkload />
          <WhosOut />
        </>
      )}

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        {!isCeo && (
          <ProjectListCard
            title={attention?.title ?? "…"}
            empty={attention?.empty ?? ""}
            projects={focus.data?.projects}
            moreHref={attention ? href(attention) : "/projects"}
            showDue
          />
        )}
        <ProjectListCard
          title="Recently updated"
          empty="No projects yet."
          projects={recent.data?.projects}
          moreHref="/projects"
        />
      </div>
    </div>
  );
}

function ProjectListCard({
  title,
  empty,
  projects,
  moreHref,
  showDue,
}: {
  title: string;
  empty: string;
  projects?: Project[];
  moreHref: string;
  showDue?: boolean;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardAction>
          <Button variant="ghost" size="sm" asChild>
            <Link href={moreHref}>
              View all <ArrowRightIcon />
            </Link>
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        {!projects ? (
          <div className="space-y-3">
            {Array.from({ length: 4 }, (_, i) => (
              <Skeleton key={i} className="h-12 w-full" />
            ))}
          </div>
        ) : projects.length === 0 ? (
          <Empty className="border border-dashed py-8">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <FolderKanbanIcon />
              </EmptyMedia>
              <EmptyTitle className="text-sm">All clear</EmptyTitle>
              <EmptyDescription>{empty}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <ul className="-mx-2 divide-y">
            {projects.map((p) => (
              <li key={p.id}>
                <Link
                  href={`/projects/${p.id}`}
                  className="flex items-center gap-3 rounded-md px-2 py-2.5 hover:bg-muted/60"
                >
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-medium">
                      {p.has_updates && (
                        <span
                          className="mr-1.5 inline-block size-2 rounded-full bg-primary align-middle"
                          title="New updates"
                        />
                      )}
                      <span className="mr-1.5 font-mono text-xs text-muted-foreground">
                        #{p.number}
                      </span>
                      {p.name ?? "Untitled"}
                    </div>
                    <div className="mt-0.5 flex items-center gap-2 text-xs text-muted-foreground">
                      <PriorityLabel
                        priority={p.priority}
                        className="text-xs"
                      />
                      <span>·</span>
                      {showDue ? (
                        <span
                          className={cn(
                            p.is_overdue && "font-medium text-red-600",
                          )}
                        >
                          Due {formatDate(p.effective_due)}
                        </span>
                      ) : (
                        <span>{relativeTime(p.last_activity_at)}</span>
                      )}
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
  );
}
