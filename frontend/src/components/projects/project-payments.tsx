"use client";

import { use, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import {
  BellRingIcon,
  CalendarClockIcon,
  CircleDollarSignIcon,
  PencilIcon,
  PlusIcon,
  ReceiptTextIcon,
  TrashIcon,
} from "lucide-react";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { useProject } from "@/lib/projects";
import { useMe } from "@/lib/session";
import { useTeams } from "@/lib/teams";
import { PAYMENT_METHODS, money } from "@/lib/payments";
import {
  milestoneAmount,
  milestonePaid,
  milestoneRemaining,
  useCreateMilestone,
  useDeleteMilestone,
  useMilestones,
  useRecordMilestonePayment,
  useRemindMilestone,
  useUpdateMilestone,
  type Milestone,
  type MilestoneInput,
  type MilestoneStatus,
  type RecordPaymentInput,
} from "@/lib/milestones";
import { cn } from "@/lib/utils";

const STATUS_TONE: Record<MilestoneStatus, string> = {
  scheduled: "bg-muted text-muted-foreground",
  invoiced: "bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-300",
  partially_paid: "bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300",
  paid: "bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300",
  overdue: "bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-300",
};

const STATUS_LABEL: Record<MilestoneStatus, string> = {
  scheduled: "Scheduled",
  invoiced: "Invoiced",
  partially_paid: "Partially paid",
  paid: "Paid",
  overdue: "Overdue",
};

function fmtDate(d: string) {
  return new Date(d.length === 10 ? d + "T00:00:00" : d).toLocaleDateString();
}

function MilestoneDialog({
  open,
  onOpenChange,
  projectId,
  initial,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  projectId: string;
  initial?: Milestone | null;
}) {
  const create = useCreateMilestone(projectId);
  const update = useUpdateMilestone(projectId);
  const teams = useTeams();
  const [form, setForm] = useState<MilestoneInput>({
    label: "",
    amount: 0,
    currency: "BDT",
    due_date: "",
    team_id: null,
    sla_response_hours: null,
  });

  useEffect(() => {
    if (open) {
      setForm(
        initial
          ? {
              label: initial.label,
              amount: milestoneAmount(initial),
              currency: initial.currency,
              due_date: initial.due_date,
              team_id: initial.team_id ?? null,
              sla_response_hours: initial.sla_response_hours ?? null,
            }
          : { label: "", amount: 0, currency: "BDT", due_date: "", team_id: null, sla_response_hours: null },
      );
    }
  }, [open, initial]);

  const pending = create.isPending || update.isPending;
  const save = () => {
    if (initial) {
      update.mutate(
        { id: initial.id, input: form },
        { onSuccess: () => onOpenChange(false) },
      );
    } else {
      create.mutate(form, { onSuccess: () => onOpenChange(false) });
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{initial ? "Edit milestone" : "Add milestone"}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="ms-label">Label</Label>
            <Input
              id="ms-label"
              value={form.label}
              onChange={(e) => setForm({ ...form, label: e.target.value })}
              placeholder="Starting amount"
            />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="ms-amount">Amount</Label>
              <Input
                id="ms-amount"
                type="number"
                min="0"
                step="0.01"
                value={form.amount || ""}
                onChange={(e) => setForm({ ...form, amount: Number(e.target.value) })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="ms-currency">Currency</Label>
              <Select
                value={form.currency}
                onValueChange={(v) => setForm({ ...form, currency: v })}
              >
                <SelectTrigger id="ms-currency">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {["BDT", "USD"].map((c) => (
                    <SelectItem key={c} value={c}>
                      {c}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="space-y-2">
            <Label htmlFor="ms-due">Due date</Label>
            <Input
              id="ms-due"
              type="date"
              value={form.due_date}
              onChange={(e) => setForm({ ...form, due_date: e.target.value })}
            />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="ms-team">Team (optional)</Label>
              <Select
                value={form.team_id ?? "none"}
                onValueChange={(v) => setForm({ ...form, team_id: v === "none" ? null : v })}
              >
                <SelectTrigger id="ms-team">
                  <SelectValue placeholder="Project team" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">Project team</SelectItem>
                  {(teams.data ?? []).map((t) => (
                    <SelectItem key={t.id} value={t.id}>
                      {t.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="ms-sla">SLA response (hours, optional)</Label>
              <Input
                id="ms-sla"
                type="number"
                min="1"
                step="1"
                value={form.sla_response_hours ?? ""}
                onChange={(e) =>
                  setForm({
                    ...form,
                    sla_response_hours: e.target.value === "" ? null : Number(e.target.value),
                  })
                }
                placeholder="e.g. 24"
              />
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <Button onClick={save} disabled={pending || !form.label.trim() || form.amount <= 0 || !form.due_date}>
            {initial ? "Save changes" : "Add milestone"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function RecordPaymentDialog({
  open,
  onOpenChange,
  projectId,
  milestone,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  projectId: string;
  milestone: Milestone | null;
}) {
  const record = useRecordMilestonePayment(projectId);
  const remaining = milestone ? milestoneRemaining(milestone) : 0;
  const [form, setForm] = useState<RecordPaymentInput>({
    amount: 0,
    method: "Bank Transfer",
    reference: "",
    paid_at: new Date().toISOString().slice(0, 10),
    rate_to_bdt: "",
    note: "",
  });

  useEffect(() => {
    if (open && milestone) {
      setForm({
        amount: remaining,
        method: "Bank Transfer",
        reference: "",
        paid_at: new Date().toISOString().slice(0, 10),
        rate_to_bdt: "",
        note: "",
      });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, milestone?.id]);

  if (!milestone) return null;
  const needsRate = milestone.currency !== "BDT";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Record payment — {milestone.label}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <p className="text-sm text-muted-foreground">
            Remaining: <strong>{money(remaining, milestone.currency)}</strong> of{" "}
            {money(milestoneAmount(milestone), milestone.currency)}
          </p>
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="rp-amount">Amount ({milestone.currency})</Label>
              <Input
                id="rp-amount"
                type="number"
                min="0"
                step="0.01"
                value={form.amount || ""}
                onChange={(e) => setForm({ ...form, amount: Number(e.target.value) })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="rp-date">Paid on</Label>
              <Input
                id="rp-date"
                type="date"
                value={form.paid_at}
                onChange={(e) => setForm({ ...form, paid_at: e.target.value })}
              />
            </div>
          </div>
          <div className="space-y-2">
            <Label htmlFor="rp-method">Payment method</Label>
            <Select value={form.method} onValueChange={(v) => setForm({ ...form, method: v })}>
              <SelectTrigger id="rp-method">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PAYMENT_METHODS.map((m) => (
                  <SelectItem key={m} value={m}>
                    {m}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {needsRate && (
            <div className="space-y-2">
              <Label htmlFor="rp-rate">Rate to BDT (optional — uses the set rate for the date if empty)</Label>
              <Input
                id="rp-rate"
                type="number"
                min="0"
                step="0.0001"
                value={form.rate_to_bdt}
                onChange={(e) => setForm({ ...form, rate_to_bdt: e.target.value })}
                placeholder="e.g. 119.50"
              />
            </div>
          )}
          <div className="space-y-2">
            <Label htmlFor="rp-ref">Reference / transaction ID</Label>
            <Input
              id="rp-ref"
              value={form.reference}
              onChange={(e) => setForm({ ...form, reference: e.target.value })}
              placeholder="Txn ID, cheque no…"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="rp-note">Note</Label>
            <Textarea
              id="rp-note"
              value={form.note}
              onChange={(e) => setForm({ ...form, note: e.target.value })}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={record.isPending}>
            Cancel
          </Button>
          <Button
            onClick={() =>
              record.mutate(
                { id: milestone.id, input: form },
                { onSuccess: () => onOpenChange(false) },
              )
            }
            disabled={record.isPending || form.amount <= 0 || form.amount > remaining + 0.005}
          >
            Record payment
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function RemindDialog({
  open,
  onOpenChange,
  projectId,
  milestone,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  projectId: string;
  milestone: Milestone | null;
}) {
  const remind = useRemindMilestone(projectId);
  const [note, setNote] = useState("");
  useEffect(() => {
    if (open) setNote("");
  }, [open ]);
  if (!milestone) return null;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Remind client — {milestone.label}</DialogTitle>
        </DialogHeader>
        <p className="text-sm text-muted-foreground">
          Sends a payment reminder to the client (and Sales):{" "}
          {money(milestoneRemaining(milestone), milestone.currency)} due {fmtDate(milestone.due_date)}.
          At most one reminder per day.
        </p>
        <div className="space-y-2">
          <Label htmlFor="rm-note">Note (optional)</Label>
          <Textarea id="rm-note" value={note} onChange={(e) => setNote(e.target.value)} />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={remind.isPending}>
            Cancel
          </Button>
          <Button
            onClick={() =>
              remind.mutate(
                { id: milestone.id, note },
                { onSuccess: () => onOpenChange(false) },
              )
            }
            disabled={remind.isPending}
          >
            Send reminder
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function ProjectPayments({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const { data: project } = useProject(id);
  const { data, isPending, isError } = useMilestones(id);
  const { data: me } = useMe();
  const del = useDeleteMilestone(id);

  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<Milestone | null>(null);
  const [paying, setPaying] = useState<Milestone | null>(null);
  const [reminding, setReminding] = useState<Milestone | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<Milestone | null>(null);

  const milestones = data?.milestones ?? [];
  const summary = data?.summary;
  const canManage =
    me?.role === "sales" || me?.role === "tl" || me?.role === "hr" || me?.role === "ceo" || me?.is_admin;
  const valueMismatch = useMemo(() => {
    if (!summary || summary.project_value === null) return false;
    return Math.abs(summary.total - summary.project_value) > 0.01;
  }, [summary]);

  return (
    <div className="space-y-6">
      <PageHeader
        title={`Payments — ${project?.project?.name ?? "Project"}`}
        description="Starting amount and milestone payments, with due dates, invoices and reminders."
        actions={
          canManage ? (
            <Button
              onClick={() => {
                setEditing(null);
                setDialogOpen(true);
              }}
            >
              <PlusIcon className="size-4" /> Add milestone
            </Button>
          ) : undefined
        }
      />

      {isPending && (
        <div className="grid gap-4 md:grid-cols-4">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-24" />
          ))}
        </div>
      )}

      {summary && (
        <div className="grid gap-4 md:grid-cols-4">
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium text-muted-foreground">Total scheduled</CardTitle>
            </CardHeader>
            <CardContent className="text-2xl font-bold">
              {money(summary.total, summary.project_currency || "BDT")}
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium text-muted-foreground">Received</CardTitle>
            </CardHeader>
            <CardContent className="text-2xl font-bold text-green-700 dark:text-green-400">
              {money(summary.paid, summary.project_currency || "BDT")}
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium text-muted-foreground">Pending</CardTitle>
            </CardHeader>
            <CardContent className="text-2xl font-bold">
              {money(summary.pending, summary.project_currency || "BDT")}
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium text-muted-foreground">Overdue</CardTitle>
            </CardHeader>
            <CardContent className="text-2xl font-bold text-red-600 dark:text-red-400">
              {summary.overdue_count > 0
                ? `${money(summary.overdue_amount, summary.project_currency || "BDT")} (${summary.overdue_count})`
                : "—"}
            </CardContent>
          </Card>
        </div>
      )}

      {valueMismatch && summary && summary.project_value !== null && (
        <Card className="border-amber-300 bg-amber-50 dark:border-amber-800 dark:bg-amber-950/30">
          <CardContent className="py-3 text-sm">
            Milestones total {money(summary.total, summary.project_currency || "BDT")} but the project
            value is {money(summary.project_value, summary.project_currency || "BDT")}. Check the
            schedule — every milestone should add up to the agreed price.
          </CardContent>
        </Card>
      )}

      {isError && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>Couldn’t load payments</EmptyTitle>
            <EmptyDescription>Try again in a moment.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {!isPending && !isError && milestones.length === 0 && (
        <Card>
          <CardContent className="py-12 text-center text-muted-foreground">
            <CircleDollarSignIcon className="mx-auto mb-3 size-8 opacity-40" />
            <p>No milestones yet. Add the starting amount first, then the rest of the schedule.</p>
          </CardContent>
        </Card>
      )}

      {milestones.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Payment schedule ({milestones.length})</CardTitle>
          </CardHeader>
          <CardContent className="p-0">
            <ul className="divide-y">
              {milestones.map((m) => {
                const editable = m.status === "scheduled" || m.status === "invoiced";
                const deletable = editable && !m.invoice_id;
                const payable = m.status !== "paid";
                return (
                  <li key={m.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
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
                        {money(milestoneAmount(m), m.currency)} · due {fmtDate(m.due_date)}
                        {milestonePaid(m) > 0 &&
                          ` · paid ${money(milestonePaid(m), m.currency)}`}
                        {m.payment_method ? ` · via ${m.payment_method}` : ""}
                        {m.payment_reference ? ` · ${m.payment_reference}` : ""}
                        {m.team_name ? ` · team: ${m.team_name}` : ""}
                        {m.sla_response_hours ? ` · SLA ${m.sla_response_hours}h` : ""}
                      </p>
                    </div>
                    <Badge className={cn(STATUS_TONE[m.status], "capitalize")}>{STATUS_LABEL[m.status]}</Badge>
                    {canManage && (
                      <div className="flex items-center gap-1">
                        {payable && (
                          <Button variant="ghost" size="sm" onClick={() => setPaying(m)}>
                            Record payment
                          </Button>
                        )}
                        {payable && (
                          <Button variant="ghost" size="icon" onClick={() => setReminding(m)} aria-label="Remind client">
                            <BellRingIcon className="size-4" />
                          </Button>
                        )}
                        {m.invoice_id && (
                          <Link href={`/billing/${m.invoice_id}`}>
                            <Button variant="ghost" size="icon" aria-label="View invoice">
                              <ReceiptTextIcon className="size-4" />
                            </Button>
                          </Link>
                        )}
                        {editable && (
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => {
                              setEditing(m);
                              setDialogOpen(true);
                            }}
                            aria-label="Edit milestone"
                          >
                            <PencilIcon className="size-4" />
                          </Button>
                        )}
                        {deletable && (
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => setConfirmDelete(m)}
                            aria-label="Delete milestone"
                          >
                            <TrashIcon className="size-4" />
                          </Button>
                        )}
                      </div>
                    )}
                  </li>
                );
              })}
            </ul>
          </CardContent>
        </Card>
      )}

      <p className="text-sm text-muted-foreground">
        <Link href={`/projects/${id}`} className="underline underline-offset-2">
          Back to project
        </Link>
        {" · "}
        <Link href="/payments" className="underline underline-offset-2">
          All payments due
        </Link>
        {" · "}
        <CalendarClockIcon className="mr-1 inline size-3" />
        On a milestone’s due date an invoice is generated automatically and the client is notified.
      </p>

      <MilestoneDialog open={dialogOpen} onOpenChange={setDialogOpen} projectId={id} initial={editing} />
      <RecordPaymentDialog open={!!paying} onOpenChange={(v) => !v && setPaying(null)} projectId={id} milestone={paying} />
      <RemindDialog open={!!reminding} onOpenChange={(v) => !v && setReminding(null)} projectId={id} milestone={reminding} />

      <Dialog open={!!confirmDelete} onOpenChange={(v) => !v && setConfirmDelete(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete milestone?</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            “{confirmDelete?.label}” ({confirmDelete ? money(milestoneAmount(confirmDelete), confirmDelete.currency) : ""}) will be
            removed from the schedule.
          </p>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmDelete(null)} disabled={del.isPending}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={del.isPending}
              onClick={() =>
                confirmDelete &&
                del.mutate(confirmDelete.id, { onSuccess: () => setConfirmDelete(null) })
              }
            >
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
