"use client";

import Link from "next/link";
import { use } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
  ArrowLeftIcon,
  BriefcaseIcon,
  CalendarIcon,
  CircleDollarSignIcon,
  ContactIcon,
  FlagIcon,
  HashIcon,
  LayersIcon,
  UsersIcon,
  StoreIcon,
  UserIcon,
  UserRoundCheckIcon,
  type LucideIcon,
} from "lucide-react";
import { useMarkViewed, useProject } from "@/lib/projects";
import { useMe } from "@/lib/session";
import { formatDate, formatDateTime, formatMoney, relativeTime } from "@/lib/format";
import type { ProjectDetail } from "@/lib/types";
import { OverdueBadge, PriorityLabel, RevisionBadge, StatusBadge, StoreAccessBadge } from "@/components/project-badges";
import { ProjectActions } from "@/components/projects/project-actions";
import { ProjectTimeline } from "@/components/projects/project-timeline";
import { ProjectFiles } from "@/components/projects/project-files";
import { StoreAccessCard } from "@/components/projects/store-access-card";
import { AckBanner } from "@/components/projects/ack-banner";
import { ProjectBoard } from "@/components/projects/project-board";
import { ProjectClientTab } from "@/components/projects/project-client";
import { useBoard } from "@/lib/cards";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ProjectUpdates } from "@/components/updates/project-updates";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

const STATUS_HINT: Partial<Record<string, (d: ProjectDetail) => string | null>> = {
  new: (d) => (d.permissions.can_assign ? "Assign it to someone on the team (or take it yourself) to start it." : "Waiting for the Team Leader to assign it."),
  ready_for_review: (d) =>
    d.actions.some((a) => a.to === "ready_to_deliver")
      ? "The team sent this for delivery. Check it, then approve it for delivery or send it back."
      : "Waiting for the Team Leader's check.",
  ready_to_deliver: (d) =>
    d.actions.some((a) => a.to === "delivered")
      ? "Ready: deliver it on Fiverr, then mark it delivered. Marking the Delivery update as sent does this too."
      : "Checked and waiting for Sales to deliver it on Fiverr.",
  delivered: (d) =>
    d.actions.some((a) => a.to === "completed")
      ? "Delivered. Mark it completed when the client accepts (or Fiverr completes the order), or record a revision."
      : "Delivered. Waiting for the client.",
};

export function ProjectView({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const detail = useProject(id);
  const { data: me } = useMe();
  const viewed = useMarkViewed(id);
  const board = useBoard(id);
  // The tab and an open card live in the URL, so a notification can link straight to a card.
  const sp = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const openCard = sp.get("card");
  const tab = openCard ? "board" : (sp.get("tab") ?? "overview");
  const setParams = (next: Record<string, string | null>) => {
    const q = new URLSearchParams(sp);
    for (const [k, v] of Object.entries(next)) {
      if (v) q.set(k, v);
      else q.delete(k);
    }
    router.replace(q.size ? `${pathname}?${q}` : pathname, { scroll: false });
  };

  if (detail.isPending) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-4 w-24" />
        <Skeleton className="h-9 w-96 max-w-full" />
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
          <Skeleton className="h-96 lg:col-span-2" />
          <Skeleton className="h-96" />
        </div>
      </div>
    );
  }
  if (detail.isError) {
    return (
      <Empty className="min-h-[50vh]">
        <EmptyHeader>
          <EmptyTitle>Project not found</EmptyTitle>
          <EmptyDescription>It may not exist, or you don&apos;t have access to it.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  const d = detail.data;
  const p = d.project;
  const hint = STATUS_HINT[p.status]?.(d);
  // The Client tab is for the people who work with the client (HR has no
  // project access, so they never reach this page at all).
  const canCollab = !!me && (me.is_admin || ["sales", "tl", "member", "ceo"].includes(me.role));

  return (
    <div className="space-y-6">
      <Link href="/projects" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeftIcon className="size-4" /> Projects
      </Link>

      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0 space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-sm text-muted-foreground">#{p.number}</span>
            <StatusBadge status={p.status} />
            {p.store_access !== "given" && p.status !== "cancelled" && p.status !== "completed" && (
              <StoreAccessBadge access={p.store_access} short />
            )}
            {p.is_overdue && <OverdueBadge />}
            <RevisionBadge count={p.revision_count} />
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">
            {p.name ?? <span className="italic text-muted-foreground">Untitled</span>}
          </h1>
          <p className="text-sm text-muted-foreground">
            {[p.client_name, p.profile_name].filter(Boolean).join(" · ") || "No client details yet"}
          </p>
        </div>
        <ProjectActions detail={d} />
      </div>

      <AckBanner detail={d} meId={me?.id} />

      {hint && (
        <div className="rounded-lg border border-primary/30 bg-accent px-4 py-3 text-sm text-accent-foreground">{hint}</div>
      )}

      <Tabs value={tab} onValueChange={(v) => setParams({ tab: v === "overview" ? null : v, card: null })}>
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="board">
            {board.data?.own_only ? "Client requests" : "Board"}
            {!!board.data?.cards.length && <span className="ml-1 text-xs text-muted-foreground tabular-nums">{board.data.cards.length}</span>}
          </TabsTrigger>
          {canCollab && <TabsTrigger value="client">Client</TabsTrigger>}
        </TabsList>
        <TabsContent value="board" className="mt-4">
          <ProjectBoard detail={d} openCardId={openCard} onOpenCard={(cid) => setParams({ tab: "board", card: cid })} />
        </TabsContent>
        {canCollab && (
          <TabsContent value="client" className="mt-4">
            <ProjectClientTab projectId={id} />
          </TabsContent>
        )}
        <TabsContent value="overview" className="mt-4">
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        <div className="space-y-6 lg:col-span-2">
          <Card>
            <CardHeader>
              <CardTitle>Scope & requirements</CardTitle>
            </CardHeader>
            <CardContent>
              {p.scope ? (
                <p className="text-sm leading-relaxed whitespace-pre-wrap">{p.scope}</p>
              ) : (
                <p className="text-sm text-muted-foreground">No scope written yet.</p>
              )}
            </CardContent>
          </Card>
          {p.special_notes && (
            <Card>
              <CardHeader>
                <CardTitle>Special notes</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-sm leading-relaxed whitespace-pre-wrap">{p.special_notes}</p>
              </CardContent>
            </Card>
          )}
          <ProjectUpdates detail={d} meId={me?.id} role={me?.role} />
          <Card>
            <CardHeader>
              <CardTitle>Activity</CardTitle>
            </CardHeader>
            <CardContent>
              <ProjectTimeline detail={d} meId={me?.id} previousViewedAt={viewed.isSuccess ? viewed.data : undefined} />
            </CardContent>
          </Card>
        </div>

        <div className="space-y-6">
          <StoreAccessCard detail={d} />
          <ProjectFiles detail={d} />
          <Card>
            <CardHeader>
              <CardTitle>Details</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <Row icon={UsersIcon} label="Team">
                {p.team_name ?? <span className="text-muted-foreground">No team</span>}
              </Row>
              <Row icon={UserRoundCheckIcon} label={p.member_names.length > 1 ? "Members" : "Assigned to"}>
                {p.member_names.length ? p.member_names.join(", ") : <span className="text-muted-foreground">Unassigned</span>}
              </Row>
              <Row icon={FlagIcon} label="Priority">
                <PriorityLabel priority={p.priority} />
              </Row>
              <Row icon={CalendarIcon} label="Due date">
                <span className={cn(p.is_overdue && "font-medium text-red-600")}>{formatDate(p.due_date)}</span>
              </Row>
              <Row icon={CalendarIcon} label="Delivery deadline">
                {formatDate(p.deadline)}
              </Row>
              <Row icon={CalendarIcon} label="Start date">
                {formatDate(p.start_date)}
              </Row>
              <Separator />
              <Row icon={HashIcon} label="Order ID">
                <span className="font-mono text-xs">{p.order_id ?? "—"}</span>
              </Row>
              <Row icon={StoreIcon} label="Profile">
                {p.profile_name ?? "—"}
              </Row>
              <Row icon={LayersIcon} label="Type">
                {p.project_type_name ?? "—"}
              </Row>
              <Row icon={BriefcaseIcon} label="Client">
                {p.client_name ?? "—"}
              </Row>
              <Row icon={ContactIcon} label="Contact">
                <span className="break-all">{p.client_contact ?? "—"}</span>
              </Row>
              <Separator />
              <Row icon={CircleDollarSignIcon} label="Agreed price">
                <span className="font-medium">{formatMoney(p.agreed_price, p.currency)}</span>
              </Row>
              <Row icon={CircleDollarSignIcon} label={`Marketplace fee (${Number(p.platform_fee_percent)}%)`}>
                {p.platform_fee ? `− ${formatMoney(p.platform_fee, p.currency)}` : "—"}
              </Row>
              <Row icon={CircleDollarSignIcon} label="You receive">
                <span className="font-medium">{p.net_amount ? formatMoney(p.net_amount, p.currency) : "—"}</span>
              </Row>
              <Separator />
              <Row icon={UserIcon} label="Sales">
                {p.created_by_name}
              </Row>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Timeline</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 text-sm">
              <Stamp label="Created" at={p.created_at} />
              <Stamp label="First submitted" at={p.first_submitted_at} />
              <Stamp label="First assigned" at={p.first_assigned_at} />
              <Stamp label="Completed" at={p.completed_at} />
              <Stamp label="Cancelled" at={p.cancelled_at} />
              <Stamp label="Last activity" at={p.last_activity_at} relative />
            </CardContent>
          </Card>

          {d.assignments.length > 1 && (
            <Card>
              <CardHeader>
                <CardTitle>Assignment history</CardTitle>
              </CardHeader>
              <CardContent>
                <ol className="space-y-2 text-sm">
                  {d.assignments.map((a) => (
                    <li key={a.id} className="flex justify-between gap-2">
                      <span className={cn(a.unassigned_at && "text-muted-foreground")}>{a.user_name}</span>
                      <span className="text-xs text-muted-foreground">
                        {formatDateTime(a.assigned_at)}
                        {a.unassigned_at ? ` – ${formatDateTime(a.unassigned_at)}` : " – now"}
                      </span>
                    </li>
                  ))}
                </ol>
              </CardContent>
            </Card>
          )}
        </div>
      </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}

function Row({ icon: Icon, label, children }: { icon: LucideIcon; label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 text-sm">
      <span className="flex shrink-0 items-center gap-2 text-muted-foreground">
        <Icon className="size-4" />
        {label}
      </span>
      <span className="min-w-0 text-right">{children}</span>
    </div>
  );
}

function Stamp({ label, at, relative }: { label: string; at: string | null; relative?: boolean }) {
  if (!at) return null;
  return (
    <div className="flex justify-between gap-4">
      <span className="text-muted-foreground">{label}</span>
      <span title={formatDateTime(at)}>{relative ? relativeTime(at) : formatDateTime(at)}</span>
    </div>
  );
}
