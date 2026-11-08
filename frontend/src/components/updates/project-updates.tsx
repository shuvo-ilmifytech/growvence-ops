"use client";

import { useState } from "react";
import { MegaphoneIcon, MessageSquareIcon, PaperclipIcon, PlusIcon } from "lucide-react";
import { toast } from "sonner";
import { relativeTime } from "@/lib/format";
import { useCreateUpdate, useUpdates } from "@/lib/updates";
import type { ProjectDetail } from "@/lib/types";
import { DestinationLabel, UpdateStatusBadge } from "@/components/update-badges";
import { UpdateSheet } from "@/components/updates/update-sheet";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";

// Updates can be written from the start of work until the project is completed.
const WORKING = new Set(["in_progress", "blocked", "ready_for_review", "ready_to_deliver", "delivered"]);

export function ProjectUpdates({ detail, meId, role }: { detail: ProjectDetail; meId?: string; role?: string }) {
  const p = detail.project;
  // Whoever is assigned writes the updates: members, and leaders who took the project on.
  const canWrite = (role === "member" || role === "tl") && !!meId && p.member_ids.includes(meId) && WORKING.has(p.status);
  return <UpdatesPanel containerId={p.id} canWrite={canWrite} role={role} />;
}

/**
 * The client updates on one container — a project or a client request — and the button to
 * write the next one. Both run the same flow, so both read the same panel.
 */
export function UpdatesPanel({
  containerId,
  isRequest,
  canWrite,
  role,
  emptyHint,
}: {
  containerId: string;
  isRequest?: boolean;
  canWrite: boolean;
  role?: string;
  /** What to say when there is nothing here yet and this person can't write one. */
  emptyHint?: string;
}) {
  const list = useUpdates(isRequest ? { client_request_id: containerId, page_size: 100 } : { project_id: containerId, page_size: 100 });
  const create = useCreateUpdate(containerId, isRequest);
  const [openId, setOpenId] = useState<string | null>(null);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <MegaphoneIcon className="size-4 text-muted-foreground" /> Client updates
          {list.data && list.data.total > 0 && <span className="text-sm font-normal text-muted-foreground">{list.data.total}</span>}
        </CardTitle>
        <CardDescription>
          {role === "sales" ? "Approved updates ready for you to post, and what's been sent." : "Written by the team, approved by the Team Leader, posted by Sales."}
        </CardDescription>
        {canWrite && (
          <CardAction>
            <Button
              size="sm"
              disabled={create.isPending}
              onClick={() =>
                create.mutate(
                  { destination: "inbox" },
                  { onSuccess: (d) => setOpenId(d.update.id), onError: (e) => toast.error(e.message) },
                )
              }
            >
              <PlusIcon /> New update
            </Button>
          </CardAction>
        )}
      </CardHeader>
      <CardContent>
        {list.isPending && <Skeleton className="h-20 w-full" />}
        {list.data?.updates.length === 0 && (
          <Empty className="border border-dashed py-8">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <MegaphoneIcon />
              </EmptyMedia>
              <EmptyTitle className="text-sm">No updates yet</EmptyTitle>
              <EmptyDescription>
                {canWrite
                  ? "Write the first update for the client."
                  : (emptyHint ?? (role === "sales" ? "Approved updates will appear here." : "The assigned member writes updates here."))}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        <ul className="space-y-2">
          {list.data?.updates.map((u) => (
            <li key={u.id}>
              <button
                type="button"
                onClick={() => setOpenId(u.id)}
                className="w-full rounded-lg border p-3 text-left transition hover:border-primary/40 hover:bg-muted/30"
              >
                <div className="flex flex-wrap items-center gap-2 text-xs">
                  <span className="font-mono text-muted-foreground">UPD-{u.number}</span>
                  <UpdateStatusBadge status={u.status} />
                  <DestinationLabel destination={u.destination} className="text-xs" />
                  {u.destination === "delivery" && u.revision_number !== null && u.revision_number > 0 && (
                    <span className="text-muted-foreground">Revision {u.revision_number}</span>
                  )}
                  <span className="ml-auto text-muted-foreground">
                    {u.author_name} · {relativeTime(u.updated_at)}
                  </span>
                </div>
                <p className="mt-1.5 line-clamp-2 text-sm">
                  {u.message || <span className="italic text-muted-foreground">Empty draft</span>}
                </p>
                {(u.attachment_count > 0 || u.ops_comment_count + u.sales_comment_count > 0) && (
                  <div className="mt-1.5 flex gap-3 text-xs text-muted-foreground">
                    {u.attachment_count > 0 && (
                      <span className="inline-flex items-center gap-1">
                        <PaperclipIcon className="size-3" /> {u.attachment_count}
                      </span>
                    )}
                    {u.ops_comment_count + u.sales_comment_count > 0 && (
                      <span className="inline-flex items-center gap-1">
                        <MessageSquareIcon className="size-3" /> {u.ops_comment_count + u.sales_comment_count}
                      </span>
                    )}
                  </div>
                )}
              </button>
            </li>
          ))}
        </ul>
      </CardContent>
      <UpdateSheet updateId={openId} onClose={() => setOpenId(null)} />
    </Card>
  );
}
