"use client";

import { use, useState } from "react";
import { CalendarIcon, MegaphoneIcon, MessageSquareIcon, SendIcon, WalletIcon } from "lucide-react";
import { toast } from "sonner";
import { ClientCollabSection } from "@/components/client/collab-section";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import {
  useClientFeedback,
  useClientProject,
  useClientUpdates,
  useSubmitFeedback,
} from "@/lib/client";
import { relativeTime } from "@/lib/format";
import {
  milestoneAmount,
  milestonePaid,
  useClientMilestones,
  type MilestoneStatus,
} from "@/lib/milestones";
import { money } from "@/lib/payments";
import { cn } from "@/lib/utils";

const STATUS_TONE: Record<string, string> = {
  in_progress: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200",
  blocked: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200",
  ready_for_review: "bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200",
  completed: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200",
};

const STATUS_LABEL: Record<string, string> = {
  submitted: "Submitted",
  needs_info: "Needs info",
  assigned: "Assigned",
  in_progress: "In progress",
  blocked: "Blocked",
  ready_for_review: "In review",
  completed: "Completed",
};

const MILESTONE_TONE: Record<MilestoneStatus, string> = {
  scheduled: "bg-muted text-muted-foreground",
  invoiced: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200",
  partially_paid: "bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200",
  paid: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200",
  overdue: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200",
};

const MILESTONE_LABEL: Record<MilestoneStatus, string> = {
  scheduled: "Scheduled",
  invoiced: "Invoiced",
  partially_paid: "Partially paid",
  paid: "Paid",
  overdue: "Overdue",
};

export function ClientProjectView({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const milestones = useClientMilestones(id);
  const project = useClientProject(id);
  const updates = useClientUpdates(id);
  const feedback = useClientFeedback(id);
  const submit = useSubmitFeedback(id);
  const [draft, setDraft] = useState("");

  const p = project.data;

  async function onSubmit() {
    const body = draft.trim();
    if (!body) return;
    try {
      await submit.mutateAsync(body);
      setDraft("");
      toast.success("Feedback sent to our team.");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Couldn't send feedback.");
    }
  }

  if (project.isPending) return <Skeleton className="h-64" />;
  if (project.isError || !p) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>Project not found</EmptyTitle>
          <EmptyDescription>You may not have access to this project.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={`#${p.number} ${p.name}`}
        description="Project overview and updates from our team."
      />

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Status</CardTitle>
          </CardHeader>
          <CardContent>
            <Badge className={cn("text-sm", STATUS_TONE[p.status] ?? "bg-muted")}>
              {STATUS_LABEL[p.status] ?? p.status}
            </Badge>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Priority</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm font-semibold capitalize">{p.priority}</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Due date</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="flex items-center gap-1.5 text-sm font-semibold">
              <CalendarIcon className="size-4 text-muted-foreground" />
              {p.due_date ?? "—"}
            </p>
          </CardContent>
        </Card>
      </div>

      {p.scope && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Project scope</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="whitespace-pre-line text-sm text-muted-foreground">{p.scope}</p>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <WalletIcon className="size-4" /> My payments
          </CardTitle>
        </CardHeader>
        <CardContent>
          {milestones.isPending ? (
            <div className="space-y-3">
              <Skeleton className="h-12" />
              <Skeleton className="h-12" />
            </div>
          ) : (milestones.data?.milestones ?? []).length === 0 ? (
            <p className="text-sm text-muted-foreground">No payment schedule shared yet.</p>
          ) : (
            <ul className="divide-y">
              {(milestones.data?.milestones ?? []).map((m) => (
                <li key={m.id} className="flex flex-wrap items-center gap-3 py-3 first:pt-0 last:pb-0">
                  <div className="min-w-0 flex-1">
                    <p className="font-medium">
                      {m.label}
                      {m.invoice_number && (
                        <span className="ml-2 text-sm font-normal text-muted-foreground">
                          · {m.invoice_number}
                        </span>
                      )}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {money(milestoneAmount(m), m.currency)} · due{" "}
                      {new Date(m.due_date + "T00:00:00").toLocaleDateString()}
                      {milestonePaid(m) > 0 && ` · paid ${money(milestonePaid(m), m.currency)}`}
                    </p>
                  </div>
                  <Badge className={cn(MILESTONE_TONE[m.status], "capitalize")}>
                    {MILESTONE_LABEL[m.status]}
                  </Badge>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <ClientCollabSection projectId={id} />

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <MegaphoneIcon className="size-4" /> Updates from our team
          </CardTitle>
        </CardHeader>
        <CardContent>
          {updates.isPending ? (
            <div className="space-y-3">
              <Skeleton className="h-16" />
              <Skeleton className="h-16" />
            </div>
          ) : (updates.data?.updates ?? []).length === 0 ? (
            <p className="text-sm text-muted-foreground">No updates shared yet.</p>
          ) : (
            <ul className="space-y-4">
              {(updates.data?.updates ?? []).map((u) => (
                <li key={u.id} className="rounded-lg bg-muted/50 p-4">
                  <p className="whitespace-pre-line text-sm">{u.message}</p>
                  <p className="mt-2 text-xs text-muted-foreground">
                    Sent {relativeTime(u.sent_at)}
                  </p>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <MessageSquareIcon className="size-4" /> Your feedback
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-col gap-2">
            <Textarea
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder="Share your thoughts, requests or questions about this project…"
              rows={3}
              maxLength={10000}
            />
            <div className="flex justify-end">
              <Button onClick={onSubmit} disabled={!draft.trim() || submit.isPending}>
                <SendIcon className="size-4" />
                {submit.isPending ? "Sending…" : "Send feedback"}
              </Button>
            </div>
          </div>
          {feedback.isPending ? (
            <Skeleton className="h-16" />
          ) : (feedback.data?.feedback ?? []).length === 0 ? (
            <p className="text-sm text-muted-foreground">No feedback yet.</p>
          ) : (
            <ul className="space-y-3">
              {(feedback.data?.feedback ?? []).map((f) => (
                <li
                  key={f.id}
                  className={cn(
                    "rounded-lg p-3 text-sm",
                    f.mine ? "bg-primary/10" : "bg-muted/50",
                  )}
                >
                  <p className="whitespace-pre-line">{f.body}</p>
                  <p className="mt-1.5 text-xs text-muted-foreground">
                    {relativeTime(f.created_at)}
                  </p>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
