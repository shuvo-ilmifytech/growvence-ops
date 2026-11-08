"use client";

import { useState } from "react";
import { ArrowRightIcon, CheckCheckIcon, HourglassIcon, MessageSquareIcon, PencilLineIcon, SendIcon } from "lucide-react";
import { toast } from "sonner";
import { useComment } from "@/lib/projects";
import { formatDate, formatDateTime, formatMoney, initials, relativeTime } from "@/lib/format";
import { PRIORITY_LABELS, ROLE_LABELS, type Priority, type ProjectDetail, type ProjectStatus } from "@/lib/types";
import { statusDot } from "@/components/project-badges";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

const FIELD_LABELS: Record<string, string> = {
  name: "Project name",
  profile_id: "Profile",
  client_name: "Client name",
  order_id: "Order ID",
  client_contact: "Client contact",
  project_type_id: "Project type",
  scope: "Scope",
  budget: "Budget",
  agreed_price: "Agreed price",
  store_access: "Store access",
  store_access_link: "Store access link",
  currency: "Currency",
  start_date: "Start date",
  deadline: "Deadline",
  priority: "Priority",
  special_notes: "Special notes",
  file_added: "File added",
  file_replaced: "File replaced",
  file_removed: "File removed",
};

const LONG_FIELDS = new Set(["scope", "special_notes"]);

type History = ProjectDetail["history"][number];
type Change = ProjectDetail["changes"][number];
type Comment = ProjectDetail["comments"][number];

type Item =
  | { kind: "status"; at: string; who: string; whoId: string; h: History }
  | { kind: "edit"; at: string; who: string; whoId: string; changes: Change[] }
  | { kind: "comment"; at: string; who: string; whoId: string; c: Comment };

// An assignment is recorded as a status row whose comment names who was added or removed:
// "assigned" in the old flow; now New → In Progress, or the same status when people change.
function isAssignment(h: History) {
  return h.to_status === "assigned" || (h.from_status === "new" && h.to_status === "in_progress") || h.from_status === h.to_status;
}

function statusSentence(h: History): string {
  const from = h.from_status;
  if (isAssignment(h) && h.comment) {
    return h.comment
      .replace(/^Assigned to /, "assigned it to ")
      .replace(/^Added /, "added ")
      .replace(/^Removed /, "removed ")
      .replace(/^Reassigned from /, "reassigned it from ");
  }
  switch (h.to_status) {
    case "new":
    case "draft":
      return "created the project";
    case "submitted":
      return from === "needs_info" ? "resubmitted with the requested info" : "submitted the project";
    case "needs_info":
      return "asked Sales for more information";
    case "assigned":
      return "assigned the project";
    case "in_progress":
      if (from === "assigned") return "started work";
      if (from === "blocked") return "unblocked it";
      if (from === "ready_for_review" || from === "ready_to_deliver") return "sent it back for changes";
      if (from === "delivered" || from === "completed") return "reopened it for a client revision";
      return "moved it to In Progress";
    case "blocked":
      return "marked it blocked";
    case "ready_for_review":
      return "sent it for delivery";
    case "ready_to_deliver":
      return from === "ready_for_review" ? "approved it for delivery" : "marked it ready to deliver";
    case "delivered":
      return "marked it delivered";
    case "completed":
      return "marked it completed";
    case "cancelled":
      return "cancelled the project";
  }
}

function displayValue(field: string, v: string | null, currency: string) {
  if (v == null || v === "") return "empty";
  if (field === "deadline" || field === "start_date") return formatDate(v);
  if (field === "budget" || field === "agreed_price") return formatMoney(v, currency);
  if (field === "priority") return PRIORITY_LABELS[v as Priority] ?? v;
  return v;
}

export function ProjectTimeline({
  detail,
  meId,
  previousViewedAt,
}: {
  detail: ProjectDetail;
  meId?: string;
  /** When the viewer last opened the project; newer items by others are marked New. */
  previousViewedAt?: string | null;
}) {
  const { project: p } = detail;
  const acksByChange = new Map<string, ProjectDetail["acknowledgements"]>();
  for (const a of detail.acknowledgements) acksByChange.set(a.project_change_id, [...(acksByChange.get(a.project_change_id) ?? []), a]);
  // Only "new since your last visit": a first visit marks nothing.
  const isNew = (it: Item) => !!previousViewedAt && it.whoId !== meId && it.at > previousViewedAt;

  // Edits from one save share a timestamp and author: show them together.
  const edits = new Map<string, Change[]>();
  for (const c of detail.changes) {
    const key = `${c.changed_by}|${c.created_at}`;
    edits.set(key, [...(edits.get(key) ?? []), c]);
  }

  const items: Item[] = [
    ...detail.history.map((h): Item => ({ kind: "status", at: h.created_at, who: h.changed_by_name, whoId: h.changed_by, h })),
    ...[...edits.values()].map(
      (cs): Item => ({ kind: "edit", at: cs[0]!.created_at, who: cs[0]!.changed_by_name, whoId: cs[0]!.changed_by, changes: cs }),
    ),
    ...detail.comments.map((c): Item => ({ kind: "comment", at: c.created_at, who: c.author_name, whoId: c.author_id, c })),
  ].sort((a, b) => a.at.localeCompare(b.at));

  return (
    <div>
      <ol className="relative space-y-5 before:absolute before:top-2 before:bottom-2 before:left-[15px] before:w-px before:bg-border">
        {items.map((it, i) => (
          <li key={i} className="relative flex gap-3">
            {it.kind === "comment" ? (
              <Avatar className="z-10 size-8 ring-4 ring-card">
                <AvatarFallback className="bg-accent text-xs font-medium text-accent-foreground">{initials(it.who)}</AvatarFallback>
              </Avatar>
            ) : (
              <div className="z-10 flex size-8 shrink-0 items-center justify-center rounded-full bg-card ring-4 ring-card">
                <div className="flex size-6 items-center justify-center rounded-full border bg-background">
                  {it.kind === "edit" ? (
                    <PencilLineIcon className="size-3 text-muted-foreground" />
                  ) : (
                    <span className={cn("size-2 rounded-full", statusDot(it.h.to_status as ProjectStatus))} />
                  )}
                </div>
              </div>
            )}
            <div className="min-w-0 flex-1 pt-1">
              {it.kind === "status" && (
                <>
                  <p className="text-sm">
                    <span className="font-medium">{it.who}</span> <span className="text-muted-foreground">{statusSentence(it.h)}</span>
                    <Time at={it.at} isNew={isNew(it)} />
                  </p>
                  {it.h.comment && !isAssignment(it.h) && (
                    <blockquote className="mt-2 border-l-2 border-primary/60 pl-3 text-sm whitespace-pre-wrap">{it.h.comment}</blockquote>
                  )}
                </>
              )}
              {it.kind === "edit" && (
                <>
                  <p className="text-sm">
                    <span className="font-medium">{it.who}</span>{" "}
                    <span className="text-muted-foreground">
                      {it.changes.every((c) => c.field.startsWith("file_")) ? "updated the files" : "edited the project"}
                    </span>
                    <Time at={it.at} isNew={isNew(it)} />
                  </p>
                  <ul className="mt-2 space-y-1.5 rounded-lg border bg-muted/30 p-3 text-sm">
                    {it.changes.map((c) => (
                      <li key={c.id} className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                        <span className="font-medium">{FIELD_LABELS[c.field] ?? c.field}</span>
                        {c.is_important && (
                          <Badge variant="outline" className="h-4 border-0 bg-amber-100 px-1.5 text-[10px] text-amber-800">
                            Important
                          </Badge>
                        )}
                        {c.field === "file_added" ? (
                          <span className="text-muted-foreground">{c.new_value}</span>
                        ) : c.field === "file_removed" ? (
                          <span className="text-muted-foreground line-through">{c.old_value}</span>
                        ) : c.field === "file_replaced" ? (
                          <span className="inline-flex items-center gap-1.5 text-muted-foreground">
                            <span>{c.old_value}</span>
                            <ArrowRightIcon className="size-3" />
                            <span className="text-foreground">{c.new_value}</span>
                          </span>
                        ) : LONG_FIELDS.has(c.field) ? (
                          <span className="w-full text-muted-foreground line-clamp-3 whitespace-pre-wrap">
                            {c.new_value ?? "cleared"}
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1.5 text-muted-foreground">
                            <span className="line-through decoration-muted-foreground/50">{displayValue(c.field, c.old_value, p.currency)}</span>
                            <ArrowRightIcon className="size-3" />
                            <span className="text-foreground">{displayValue(c.field, c.new_value, p.currency)}</span>
                          </span>
                        )}
                        {acksByChange.get(c.id)?.map((a) => (
                          <span
                            key={a.id}
                            className={cn(
                              "inline-flex items-center gap-1 text-xs",
                              a.acknowledged_at ? "text-emerald-700" : "text-amber-700",
                            )}
                            title={a.acknowledged_at ? formatDateTime(a.acknowledged_at) : undefined}
                          >
                            {a.acknowledged_at ? <CheckCheckIcon className="size-3" /> : <HourglassIcon className="size-3" />}
                            {a.acknowledged_at
                              ? `Acknowledged by ${a.user_name} ${relativeTime(a.acknowledged_at)}`
                              : `Waiting for ${a.user_name} to acknowledge`}
                          </span>
                        ))}
                      </li>
                    ))}
                  </ul>
                </>
              )}
              {it.kind === "comment" && (
                <div className="rounded-lg border bg-card p-3">
                  <p className="mb-1 text-sm">
                    <span className="font-medium">{it.who}</span>{" "}
                    <span className="text-xs text-muted-foreground">{ROLE_LABELS[it.c.author_role]}</span>
                    <Time at={it.at} isNew={isNew(it)} />
                  </p>
                  <p className="text-sm whitespace-pre-wrap">{it.c.body}</p>
                </div>
              )}
            </div>
          </li>
        ))}
      </ol>
      {detail.permissions.can_comment && <Composer projectId={p.id} />}
    </div>
  );
}

function Time({ at, isNew }: { at: string; isNew?: boolean }) {
  return (
    <>
      <time dateTime={at} title={formatDateTime(at)} className="ml-2 text-xs text-muted-foreground">
        {relativeTime(at)}
      </time>
      {isNew && (
        <Badge className="ml-2 h-4 px-1.5 text-[10px]" aria-label="New since your last visit">
          New
        </Badge>
      )}
    </>
  );
}

function Composer({ projectId }: { projectId: string }) {
  const [body, setBody] = useState("");
  const comment = useComment(projectId);

  function send() {
    if (!body.trim()) return;
    comment.mutate(body.trim(), {
      onSuccess: () => setBody(""),
      onError: (err) => toast.error(err.message),
    });
  }

  return (
    <div className="mt-6 flex gap-3">
      <div className="flex size-8 shrink-0 items-center justify-center rounded-full bg-muted">
        <MessageSquareIcon className="size-4 text-muted-foreground" />
      </div>
      <div className="flex-1 space-y-2">
        <Textarea
          value={body}
          onChange={(e) => setBody(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) send();
          }}
          placeholder="Write a comment…"
          rows={3}
        />
        <div className="flex items-center justify-between">
          <span className="text-xs text-muted-foreground">Ctrl + Enter to send</span>
          <Button size="sm" onClick={send} disabled={!body.trim() || comment.isPending}>
            {comment.isPending ? <Spinner /> : <SendIcon />}
            Comment
          </Button>
        </div>
      </div>
    </div>
  );
}
