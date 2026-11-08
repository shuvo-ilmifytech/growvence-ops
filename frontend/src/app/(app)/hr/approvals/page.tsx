"use client";

import Link from "next/link";
import { useState } from "react";
import { CheckIcon, InboxIcon, PlaneIcon, TimerResetIcon, XIcon } from "lucide-react";
import { formatDate, relativeTime } from "@/lib/format";
import { clock, days, useApprovals, useHrMe, useWhosOut } from "@/lib/hr";
import { useMe } from "@/lib/session";
import { PageHeader } from "@/components/page-header";
import { ReviewDialog, type ReviewTarget } from "@/components/hr/hr-dialogs";
import { leaveDates } from "@/components/hr/my-hr";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";

export default function ApprovalsPage() {
  const ap = useApprovals();
  const { data: user } = useMe();
  const isHR = user?.role === "hr" || user?.role === "ceo";
  // A Team Lead's approval passes the request on to HR; HR's (or the CEO's) is final.
  const leaveDetail = (l: { status: string; start_date: string; end_date: string; half_day: boolean }) =>
    leaveDates(l) + (l.status === "pending" && !isHR ? ". After you approve, HR gives the final approval." : "");
  const me = useHrMe();
  const tz = me.data?.schedule.timezone ?? "UTC";
  const [target, setTarget] = useState<ReviewTarget | null>(null);
  const leave = ap.data?.leave ?? [];
  const corrections = ap.data?.corrections ?? [];

  return (
    <div className="space-y-6">
      <PageHeader
        title="Approvals"
        description="Leave is approved by the Team Lead first, then HR gives the final approval. Corrections need one approval."
      />

      {ap.isPending && <Skeleton className="h-48 w-full" />}
      {ap.isError && <p className="text-sm text-destructive">{ap.error.message}</p>}
      {ap.isSuccess && leave.length + corrections.length === 0 && (
        <Card>
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <InboxIcon />
              </EmptyMedia>
              <EmptyTitle>All caught up</EmptyTitle>
              <EmptyDescription>Nothing is waiting for your approval.</EmptyDescription>
            </EmptyHeader>
          </Empty>
        </Card>
      )}

      {leave.length > 0 && (
        <section className="space-y-3">
          <h2 className="flex items-center gap-2 text-sm font-medium">
            <PlaneIcon className="size-4 text-primary" /> Leave requests <span className="text-muted-foreground">{leave.length}</span>
          </h2>
          {leave.map((l) => (
            <Card key={l.id} className="py-4">
              <CardContent className="flex flex-wrap items-start justify-between gap-4 px-4">
                <div className="min-w-0 space-y-1">
                  <div className="font-medium">
                    <Link href={`/hr/people/${l.user_id}`} className="hover:underline">
                      {l.user_name}
                    </Link>{" "}
                    <span className="font-normal text-muted-foreground">
                      · {l.leave_type_name} leave{!l.is_paid && " (unpaid)"}
                    </span>
                  </div>
                  <div className="text-sm">
                    {leaveDates(l)} · <span className="font-medium tabular-nums">{days(l.days)}</span> {Number(l.days) === 1 ? "day" : "days"}
                  </div>
                  {l.reason && <p className="text-sm text-muted-foreground">“{l.reason}”</p>}
                  <p className="text-xs text-muted-foreground">
                    Requested {relativeTime(l.created_at)} ·{" "}
                    {l.status === "awaiting_hr" ? (
                      <span className="font-medium text-orange-700 dark:text-orange-400">
                        Final approval · approved by {l.manager_reviewed_by_name}
                        {l.manager_note && ` (“${l.manager_note}”)`}
                      </span>
                    ) : (
                      <span className="font-medium text-amber-700 dark:text-amber-400">Step 1 of 2 · Team Lead</span>
                    )}
                  </p>
                  <OverlapHint from={l.start_date} to={l.end_date} self={l.user_id} />
                </div>
                <ReviewButtons
                  onApprove={() => setTarget({ kind: "leave", id: l.id, approve: true, title: `${l.user_name}'s leave`, detail: leaveDetail(l) })}
                  onReject={() => setTarget({ kind: "leave", id: l.id, approve: false, title: `${l.user_name}'s leave`, detail: leaveDates(l) })}
                />
              </CardContent>
            </Card>
          ))}
        </section>
      )}

      {corrections.length > 0 && (
        <section className="space-y-3">
          <h2 className="flex items-center gap-2 text-sm font-medium">
            <TimerResetIcon className="size-4 text-primary" /> Attendance corrections{" "}
            <span className="text-muted-foreground">{corrections.length}</span>
          </h2>
          {corrections.map((c) => {
            const detail = `${formatDate(c.work_date)}: ${clock(c.check_in_at, tz)} – ${clock(c.check_out_at, tz)}`;
            return (
              <Card key={c.id} className="py-4">
                <CardContent className="flex flex-wrap items-start justify-between gap-4 px-4">
                  <div className="min-w-0 space-y-1">
                    <div className="font-medium">{c.user_name}</div>
                    <div className="text-sm tabular-nums">{detail}</div>
                    <p className="text-sm text-muted-foreground">“{c.reason}”</p>
                    <p className="text-xs text-muted-foreground">Requested {relativeTime(c.created_at)}</p>
                  </div>
                  <ReviewButtons
                    onApprove={() => setTarget({ kind: "corrections", id: c.id, approve: true, title: `${c.user_name}'s correction`, detail })}
                    onReject={() => setTarget({ kind: "corrections", id: c.id, approve: false, title: `${c.user_name}'s correction`, detail })}
                  />
                </CardContent>
              </Card>
            );
          })}
        </section>
      )}

      <ReviewDialog target={target} onOpenChange={(o) => !o && setTarget(null)} />
    </div>
  );
}

function ReviewButtons({ onApprove, onReject }: { onApprove: () => void; onReject: () => void }) {
  return (
    <div className="flex gap-2">
      <Button variant="outline" size="sm" onClick={onReject}>
        <XIcon /> Reject
      </Button>
      <Button size="sm" onClick={onApprove}>
        <CheckIcon /> Approve
      </Button>
    </div>
  );
}

/** Who else is already off on those days, so the manager can judge cover. */
function OverlapHint({ from, to, self }: { from: string; to: string; self: string }) {
  const out = useWhosOut(from, to).data?.filter((o) => o.user_id !== self) ?? [];
  if (!out.length) return null;
  return (
    <p className="text-xs text-amber-700 dark:text-amber-400">
      Also off then: {out.map((o) => `${o.user_name} (${formatDate(o.start_date)}${o.end_date !== o.start_date ? `–${formatDate(o.end_date)}` : ""})`).join(", ")}
    </p>
  );
}
