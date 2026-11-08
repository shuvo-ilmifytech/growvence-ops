"use client";

import { useState } from "react";
import { CheckIcon, EyeIcon, EyeOffIcon, SendIcon } from "lucide-react";
import { toast } from "sonner";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  useAddTaskComment,
  useRequestApproval,
  useSetCardVisible,
  useTaskApprovals,
  useTaskComments,
} from "@/lib/clients";
import { formatDateTime, relativeTime } from "@/lib/format";
import { useMe } from "@/lib/session";
import type { Card } from "@/lib/types";

function initials(name: string | null) {
  return (name ?? "?")
    .split(" ")
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0])
    .join("")
    .toUpperCase();
}

/**
 * Client collaboration on one card: visibility, shared comments and the
 * approval flow. Lives inside the card dialog, below the team's own comments.
 * Only project cards (not client-request cards) have a client to share with.
 */
export function CardCollab({ card, projectId }: { card: Card; projectId: string }) {
  const { data: me } = useMe();
  const comments = useTaskComments(projectId, card.id);
  const approvals = useTaskApprovals(projectId, card.id);
  const addComment = useAddTaskComment(projectId, card.id);
  const setVisible = useSetCardVisible(projectId, card.id);
  const requestApproval = useRequestApproval(projectId, card.id);
  const [body, setBody] = useState("");
  const [internal, setInternal] = useState(false);

  // Only Sales/TL/admins manage the client side of a task.
  const canManage = !!me && (me.is_admin || me.role === "sales" || me.role === "tl");
  const isProjectCard = !!card.project_id;
  if (!isProjectCard) return null;

  const pending = (approvals.data?.approvals ?? []).find((a) => a.status === "pending");
  const lastDecision = (approvals.data?.approvals ?? []).find((a) => a.status !== "pending");

  return (
    <div className="space-y-4 border-t pt-4">
      <div className="flex flex-wrap items-center gap-3">
        <h4 className="text-sm font-medium">Client collaboration</h4>
        {card.visible_to_client ? (
          <Badge className="bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200">
            <EyeIcon className="size-3" /> Visible to client
          </Badge>
        ) : (
          <Badge className="bg-muted text-muted-foreground">
            <EyeOffIcon className="size-3" /> Hidden from client
          </Badge>
        )}
        {pending && <Badge className="bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200">Approval pending</Badge>}
        {lastDecision?.status === "approved" && (
          <Badge className="bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200">
            <CheckIcon className="size-3" /> Approved by client
          </Badge>
        )}
        {lastDecision?.status === "changes_requested" && (
          <Badge className="bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200">Changes requested</Badge>
        )}
      </div>

      {canManage && (
        <div className="flex flex-wrap items-center gap-4">
          <label className="flex items-center gap-2 text-sm">
            <Switch
              checked={card.visible_to_client}
              disabled={setVisible.isPending}
              onCheckedChange={(v) =>
                setVisible.mutate(v, {
                  onSuccess: () => toast.success(v ? "Task is now visible to the client." : "Task hidden from the client."),
                  onError: (err) => toast.error(err.message),
                })
              }
            />
            Share this task with the client
          </label>
          {card.visible_to_client && !pending && (
            <Button
              size="sm"
              variant="outline"
              disabled={requestApproval.isPending}
              onClick={() =>
                requestApproval.mutate(undefined, {
                  onSuccess: () => toast.success("Approval requested from the client."),
                  onError: (err) => toast.error(err.message),
                })
              }
            >
              Request client approval
            </Button>
          )}
        </div>
      )}

      {lastDecision?.status === "changes_requested" && lastDecision.review_note && (
        <p className="rounded-lg bg-red-50 p-3 text-sm text-red-800 dark:bg-red-950 dark:text-red-200">
          Client asked for changes: {lastDecision.review_note}
        </p>
      )}

      <div className="space-y-3">
        <h5 className="text-sm font-medium">Shared comments</h5>
        {comments.isPending ? (
          <p className="text-sm text-muted-foreground">Loading…</p>
        ) : (comments.data?.comments ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">No shared comments yet. Internal-only comments stay with the team.</p>
        ) : (
          <ul className="space-y-3">
            {(comments.data?.comments ?? []).map((m) => (
              <li key={m.id} className="flex gap-2">
                <Avatar className="size-6">
                  <AvatarFallback className="bg-accent text-[10px] font-medium text-accent-foreground">{initials(m.author_name)}</AvatarFallback>
                </Avatar>
                <div className="min-w-0">
                  <div className="text-xs text-muted-foreground">
                    <span className="font-medium text-foreground">{m.author_name ?? "Someone"}</span>
                    {m.is_internal && <Badge className="ml-1.5 bg-muted text-muted-foreground">Internal only</Badge>}
                    {" · "}
                    <span title={formatDateTime(m.created_at)}>{relativeTime(m.created_at)}</span>
                  </div>
                  <p className="text-sm whitespace-pre-wrap">{m.body}</p>
                </div>
              </li>
            ))}
          </ul>
        )}
        <form
          className="grid gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (!body.trim()) return;
            addComment.mutate(
              { body: body.trim(), is_internal: internal },
              { onSuccess: () => (setBody(""), setInternal(false)), onError: (err) => toast.error(err.message) },
            );
          }}
        >
          <Textarea
            value={body}
            onChange={(e) => setBody(e.target.value)}
            rows={2}
            placeholder="Write a comment the client can see…"
            aria-label="Shared comment"
            maxLength={5000}
          />
          <div className="flex items-center justify-between gap-2">
            <label className="flex items-center gap-2 text-xs text-muted-foreground">
              <Switch checked={internal} onCheckedChange={setInternal} />
              Internal only (team sees it, client doesn&apos;t)
            </label>
            <Button type="submit" variant="outline" size="sm" disabled={addComment.isPending || !body.trim()}>
              <SendIcon className="size-4" /> Comment
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}
