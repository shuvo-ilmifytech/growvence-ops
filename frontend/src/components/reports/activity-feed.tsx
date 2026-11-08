"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { ActivityIcon } from "lucide-react";
import { api } from "@/lib/api";
import { formatDateTime, initials, relativeTime } from "@/lib/format";
import { useActivity, type ActivityRow } from "@/lib/reports";
import { useMe } from "@/lib/session";
import { CARD_COLUMN_LABELS, STATUS_LABELS, UPDATE_STATUS_LABELS, type CardColumn, type DirectoryUser, type ProjectStatus, type UpdateStatus } from "@/lib/types";
import { PageHeader } from "@/components/page-header";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";

const TYPES = [
  { value: "project", label: "Projects" },
  { value: "update", label: "Updates" },
  { value: "user", label: "Users" },
  { value: "setting", label: "Settings" },
  { value: "auth", label: "Sign-ins" },
  { value: "team_target", label: "Targets" },
];

const S = (s: unknown) => STATUS_LABELS[s as ProjectStatus] ?? String(s);
const U = (s: unknown) => UPDATE_STATUS_LABELS[s as UpdateStatus] ?? String(s);

/** Turns an activity row into a sentence; unknown actions fall back to their key. */
function describe(a: ActivityRow): string {
  const m = a.metadata ?? {};
  const upd = m.number ? `UPD-${m.number}` : "an update";
  switch (a.action) {
    case "auth.login":
      return "signed in";
    case "project.created":
      return "created the project";
    case "request.raised":
      return `raised a client request for ${m.client}${m.order_id ? ` (${m.order_id})` : ""} and sent it to ${m.team}`;
    case "request.updated":
      return "changed what the client asked for";
    case "request.team_changed":
      return `moved it from ${m.from} to ${m.to}`;
    case "request.status_changed":
      return `${m.to === "open" ? "reopened it" : m.to === "done" ? "closed it" : "cancelled it"}${m.reason ? `: ${m.reason}` : ""}`;
    case "project.updated":
      return `edited ${((m.fields as string[]) ?? []).map((f) => f.replaceAll("_id", "").replaceAll("_", " ")).join(", ") || "the project"}`;
    case "project.status_changed":
      return `moved it from ${S(m.from)} to ${S(m.to)}${m.via_update ? ` (delivery UPD-${m.via_update})` : ""}`;
    case "project.assigned":
      return "assigned it";
    case "project.reassigned":
      return "reassigned it";
    case "project.planning_updated":
      return "changed priority / due date";
    case "project.commented":
      return "commented";
    case "project.file_added":
      return `added ${m.name}`;
    case "project.file_replaced":
      return `uploaded v${m.version} of ${m.name}`;
    case "project.file_removed":
      return `removed ${m.name}`;
    case "project.changes_acknowledged":
      return `acknowledged ${m.count} important change${m.count === 1 ? "" : "s"}`;
    case "update.created":
      return `started ${upd}`;
    case "update.edited":
      return m.from !== m.to ? `edited ${upd} (back to ${U(m.to)})` : `edited ${upd}`;
    case "update.submitted":
      return m.auto_approved ? `submitted ${upd} (auto-approved)` : `submitted ${upd} for approval`;
    case "update.approved":
      return `approved ${upd}`;
    case "update.changes_requested":
      return `requested changes on ${upd}`;
    case "update.sent":
      return `marked ${upd} as sent`;
    case "update.commented":
      return `commented on ${upd} (${m.thread} thread)`;
    case "update.destination_corrected":
      return `corrected where ${upd} goes`;
    case "update.attachment_added":
      return `attached a file to ${upd}`;
    case "update.attachment_removed":
      return `removed an attachment from ${upd}`;
    case "user.created":
      return `added user ${m.email} (${m.role})`;
    case "user.updated":
      return `changed ${Object.keys(m).join(", ")} of a user`;
    case "user.access_revoked":
      return `revoked ${m.name}'s access${m.reason ? ` (${m.reason})` : ""}`;
    case "user.access_restored":
      return `restored ${m.name}'s access`;
    case "user.access_changed":
      return `changed ${m.name}'s access`;
    case "card.created":
      return `added ${m.source === "client" ? "client request" : "task"} #${m.number} “${m.title}”`;
    case "card.updated":
      return `edited card #${m.number}`;
    case "card.moved":
      return `moved card #${m.number} “${m.title}” to ${CARD_COLUMN_LABELS[m.to as CardColumn] ?? m.to}`;
    case "card.assigned":
      return `changed who works on card #${m.number}`;
    case "card.commented":
      return `commented on card #${m.number}`;
    case "card.removed":
      return `removed card #${m.number} “${m.title}”`;
    case "user.password_reset":
      return "reset a user's password";
    case "team_target.set":
      return `set ${m.team}'s ${m.month} revenue target to ${m.amount} ${m.currency}`;
    case "team_target.cleared":
      return `removed ${m.team}'s ${m.month} revenue target`;
    case "setting.updated":
      return `changed setting ${m.key} to ${JSON.stringify(m.to)}`;
    case "profile.created":
    case "profile.updated":
    case "project_type.created":
    case "project_type.updated":
      return a.action.replace(".", " ").replace("_", " ");
  }
  return a.action;
}

export function ActivityFeed() {
  const { data: me } = useMe();
  const router = useRouter();
  const pathname = usePathname();
  const sp = useSearchParams();
  const type = sp.get("type") ?? "";
  const actor = sp.get("actor") ?? "";
  const users = useQuery({
    queryKey: ["directory", "all"],
    queryFn: () => api<{ users: DirectoryUser[] }>("/users/directory").then((r) => r.users),
  });
  const feed = useActivity({ type, actor_id: actor });
  const rows = feed.data?.pages.flat() ?? [];
  const isMgmt = me?.role === "ceo";

  function setParam(k: string, v: string) {
    const next = new URLSearchParams(sp);
    if (v && v !== "any") next.set(k, v);
    else next.delete(k);
    router.replace(`${pathname}?${next}`, { scroll: false });
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Activity"
        description={isMgmt ? "Everything that happens in the workspace, newest first." : "Everything that happens on projects, newest first."}
        actions={
          <>
            <Select value={type || "any"} onValueChange={(v) => setParam("type", v)}>
              <SelectTrigger className="w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="any">All activity</SelectItem>
                {TYPES.filter((t) => isMgmt || t.value === "project" || t.value === "update").map((t) => (
                  <SelectItem key={t.value} value={t.value}>
                    {t.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={actor || "any"} onValueChange={(v) => setParam("actor", v)}>
              <SelectTrigger className="w-44">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="any">Everyone</SelectItem>
                {users.data?.map((u) => (
                  <SelectItem key={u.id} value={u.id}>
                    {u.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </>
        }
      />
      <Card className="py-2">
        <CardContent className="px-2">
          {feed.isPending && (
            <div className="space-y-3 p-3">
              {Array.from({ length: 8 }, (_, i) => (
                <Skeleton key={i} className="h-10 w-full" />
              ))}
            </div>
          )}
          {feed.isError && <p className="p-4 text-sm text-destructive">{feed.error.message}</p>}
          {feed.isSuccess && rows.length === 0 && (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ActivityIcon />
                </EmptyMedia>
                <EmptyTitle>No activity</EmptyTitle>
                <EmptyDescription>Nothing matches these filters.</EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
          <ol className="divide-y">
            {rows.map((a) => (
              <li key={a.id} className="flex items-start gap-3 px-3 py-2.5 text-sm">
                <Avatar className="mt-0.5 size-7">
                  <AvatarFallback className="bg-accent text-[10px] font-medium text-accent-foreground">
                    {a.actor_name ? initials(a.actor_name) : "⚙"}
                  </AvatarFallback>
                </Avatar>
                <div className="min-w-0 flex-1">
                  <span className="font-medium">{a.actor_name ?? "System"}</span> <span className="text-muted-foreground">{describe(a)}</span>
                  {(a.project_id || a.client_request_id) && (
                    <>
                      {" "}
                      <span className="text-muted-foreground">on</span>{" "}
                      <Link
                        href={a.client_request_id ? `/requests/${a.client_request_id}` : `/projects/${a.project_id}`}
                        className="font-medium hover:underline"
                      >
                        {a.client_request_id ? "CR-" : "#"}
                        {a.project_number} {a.project_name}
                      </Link>
                    </>
                  )}
                </div>
                <time className="shrink-0 text-xs text-muted-foreground" dateTime={a.created_at} title={formatDateTime(a.created_at)}>
                  {relativeTime(a.created_at)}
                </time>
              </li>
            ))}
          </ol>
          {feed.hasNextPage && (
            <div className="flex justify-center p-3">
              <Button variant="outline" onClick={() => feed.fetchNextPage()} disabled={feed.isFetchingNextPage}>
                {feed.isFetchingNextPage && <Spinner />} Load more
              </Button>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
