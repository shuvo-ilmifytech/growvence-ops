"use client";

import { ArrowRightIcon, BellRingIcon, CheckIcon } from "lucide-react";
import { toast } from "sonner";
import { useAcknowledge } from "@/lib/projects";
import { formatDate, formatMoney } from "@/lib/format";
import type { ProjectDetail } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";

const LABELS: Record<string, string> = { scope: "Scope", budget: "Budget", deadline: "Delivery deadline" };

/** Shown to assigned members when Sales changed scope, budget or the delivery deadline. */
export function AckBanner({ detail, meId }: { detail: ProjectDetail; meId?: string }) {
  const ack = useAcknowledge(detail.project.id);
  if (!detail.permissions.can_acknowledge) return null;

  const pendingIds = new Set(
    detail.acknowledgements.filter((a) => a.user_id === meId && !a.acknowledged_at).map((a) => a.project_change_id),
  );
  const changes = detail.changes.filter((c) => pendingIds.has(c.id));
  const p = detail.project;
  const show = (field: string, v: string | null) =>
    v == null ? "empty" : field === "deadline" ? formatDate(v) : field === "budget" ? formatMoney(v, p.currency) : v;

  return (
    <div className="rounded-lg border border-amber-300 bg-amber-50 p-4 text-amber-950">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex gap-3">
          <BellRingIcon className="mt-0.5 size-5 shrink-0 text-amber-600" />
          <div className="space-y-2">
            <p className="font-medium">Sales changed important details. Please review and acknowledge.</p>
            <ul className="space-y-1 text-sm">
              {changes.map((c) => (
                <li key={c.id} className="flex flex-wrap items-baseline gap-x-2">
                  <span className="font-medium">{LABELS[c.field] ?? c.field}</span>
                  {c.field === "scope" ? (
                    <span className="line-clamp-2 w-full whitespace-pre-wrap text-amber-900/80">{c.new_value}</span>
                  ) : (
                    <span className="inline-flex items-center gap-1.5 text-amber-900/80">
                      <span className="line-through">{show(c.field, c.old_value)}</span>
                      <ArrowRightIcon className="size-3" />
                      <span className="font-medium text-amber-950">{show(c.field, c.new_value)}</span>
                    </span>
                  )}
                </li>
              ))}
            </ul>
          </div>
        </div>
        <Button
          onClick={() =>
            ack.mutate(undefined, {
              onSuccess: () => toast.success("Acknowledged. The Team Leader can see you've reviewed the changes."),
              onError: (e) => toast.error(e.message),
            })
          }
          disabled={ack.isPending}
        >
          {ack.isPending ? <Spinner /> : <CheckIcon />}
          Acknowledge {detail.pending_acks > 1 ? `${detail.pending_acks} changes` : "change"}
        </Button>
      </div>
    </div>
  );
}
