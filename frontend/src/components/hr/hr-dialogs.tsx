"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import { days, HR_KEY, remaining, useLeaveTypes, type Balance } from "@/lib/hr";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldContent, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";

type Fields = Record<string, string>;

/** Runs a form submit: API field errors go under their inputs, anything else is a toast. */
function useSubmit(onDone: () => void) {
  const qc = useQueryClient();
  const [fields, setFields] = useState<Fields>({});
  const [pending, setPending] = useState(false);
  async function run(fn: () => Promise<unknown>, success: string) {
    setFields({});
    setPending(true);
    try {
      await fn();
      toast.success(success);
      qc.invalidateQueries({ queryKey: HR_KEY });
      onDone();
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length) setFields(err.fields);
      else toast.error(err instanceof Error ? err.message : "Could not save");
    } finally {
      setPending(false);
    }
  }
  return { fields, pending, run };
}

// ── Apply for leave ────────────────────────────────────────

export function LeaveDialog({
  open,
  onOpenChange,
  balances,
  today,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  balances: Balance[];
  today: string;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        {open && <LeaveForm balances={balances} today={today} onDone={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  );
}

function LeaveForm({ balances, today, onDone }: { balances: Balance[]; today: string; onDone: () => void }) {
  const types = useLeaveTypes();
  const [typeId, setTypeId] = useState("");
  const [start, setStart] = useState(today);
  const [end, setEnd] = useState(today);
  const [halfDay, setHalfDay] = useState(false);
  const { fields, pending, run } = useSubmit(onDone);
  const balance = balances.find((b) => b.leave_type_id === typeId);
  const singleDay = start === end;

  return (
    <form
      className="grid gap-6"
      onSubmit={(e) => {
        e.preventDefault();
        const reason = String(new FormData(e.currentTarget).get("reason") ?? "");
        run(
          () =>
            api("/hr/me/leave", {
              method: "POST",
              body: { leave_type_id: typeId, start_date: start, end_date: end, half_day: singleDay && halfDay, reason },
            }),
          "Leave requested. Your Team Lead approves it first, then HR.",
        );
      }}
    >
      <DialogHeader>
        <DialogTitle>Request leave</DialogTitle>
        <DialogDescription>Weekly off days and holidays inside the dates aren&apos;t counted.</DialogDescription>
      </DialogHeader>
      <FieldGroup>
        <Field data-invalid={!!fields.leave_type_id}>
          <FieldLabel>Leave type</FieldLabel>
          <Select value={typeId} onValueChange={setTypeId} required>
            <SelectTrigger className="w-full" aria-invalid={!!fields.leave_type_id}>
              <SelectValue placeholder="Choose a type" />
            </SelectTrigger>
            <SelectContent>
              {(types.data ?? []).map((t) => {
                const b = balances.find((x) => x.leave_type_id === t.id);
                return (
                  <SelectItem key={t.id} value={t.id}>
                    {t.name}
                    {b?.is_limited && <span className="text-muted-foreground"> · {days(remaining(b))} left</span>}
                    {!t.is_paid && <span className="text-muted-foreground"> · unpaid</span>}
                  </SelectItem>
                );
              })}
            </SelectContent>
          </Select>
          {balance?.is_limited && (
            <FieldDescription>
              {days(remaining(balance))} of {days(balance.entitled)} days left this year
              {Number(balance.pending) > 0 && ` (${days(balance.pending)} pending)`}.
            </FieldDescription>
          )}
          <FieldError>{fields.leave_type_id && `Leave type: ${fields.leave_type_id}`}</FieldError>
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field data-invalid={!!fields.start_date}>
            <FieldLabel htmlFor="start">From</FieldLabel>
            <Input
              id="start"
              type="date"
              value={start}
              required
              onChange={(e) => {
                setStart(e.target.value);
                if (e.target.value > end) setEnd(e.target.value);
              }}
            />
            <FieldError>{fields.start_date && `Start ${fields.start_date}`}</FieldError>
          </Field>
          <Field data-invalid={!!fields.end_date}>
            <FieldLabel htmlFor="end">To</FieldLabel>
            <Input id="end" type="date" value={end} min={start} required onChange={(e) => setEnd(e.target.value)} />
            <FieldError>{fields.end_date && `End ${fields.end_date}`}</FieldError>
          </Field>
        </div>
        {singleDay && (
          <Field orientation="horizontal" className="rounded-lg border p-3">
            <FieldContent>
              <FieldLabel htmlFor="half">Half day</FieldLabel>
              <FieldDescription>Counts as 0.5 days.</FieldDescription>
            </FieldContent>
            <Switch id="half" checked={halfDay} onCheckedChange={setHalfDay} />
          </Field>
        )}
        <Field data-invalid={!!fields.reason}>
          <FieldLabel htmlFor="reason">Reason</FieldLabel>
          <Textarea id="reason" name="reason" maxLength={1000} placeholder="Optional, but it helps your manager decide" />
          <FieldError>{fields.reason && `Reason ${fields.reason}`}</FieldError>
        </Field>
      </FieldGroup>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">Cancel</Button>
        </DialogClose>
        <Button type="submit" disabled={pending || !typeId}>Send request</Button>
      </DialogFooter>
    </form>
  );
}

// ── Attendance correction ──────────────────────────────────

export function CorrectionDialog({ open, onOpenChange, today }: { open: boolean; onOpenChange: (o: boolean) => void; today: string }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        {open && <CorrectionForm today={today} onDone={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  );
}

function CorrectionForm({ today, onDone }: { today: string; onDone: () => void }) {
  const { fields, pending, run } = useSubmit(onDone);
  return (
    <form
      className="grid gap-6"
      onSubmit={(e) => {
        e.preventDefault();
        const d = new FormData(e.currentTarget);
        const out = String(d.get("check_out") ?? "");
        run(
          () =>
            api("/hr/me/corrections", {
              method: "POST",
              body: { work_date: d.get("work_date"), check_in: d.get("check_in"), check_out: out || null, reason: d.get("reason") },
            }),
          "Correction requested. Your manager will review it.",
        );
      }}
    >
      <DialogHeader>
        <DialogTitle>Fix a day&apos;s attendance</DialogTitle>
        <DialogDescription>Forgot to check in or out? Ask for a correction. It applies once your manager approves.</DialogDescription>
      </DialogHeader>
      <FieldGroup>
        <Field data-invalid={!!fields.work_date}>
          <FieldLabel htmlFor="work_date">Day</FieldLabel>
          <Input id="work_date" name="work_date" type="date" max={today} defaultValue={today} required />
          <FieldError>{fields.work_date && `Day ${fields.work_date}`}</FieldError>
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field data-invalid={!!fields.check_in}>
            <FieldLabel htmlFor="check_in">Check-in</FieldLabel>
            <Input id="check_in" name="check_in" type="time" defaultValue="09:00" required />
            <FieldError>{fields.check_in && `Check-in ${fields.check_in}`}</FieldError>
          </Field>
          <Field data-invalid={!!fields.check_out}>
            <FieldLabel htmlFor="check_out">Check-out</FieldLabel>
            <Input id="check_out" name="check_out" type="time" />
            <FieldError>{fields.check_out && `Check-out ${fields.check_out}`}</FieldError>
          </Field>
        </div>
        <Field data-invalid={!!fields.reason}>
          <FieldLabel htmlFor="c-reason">Reason</FieldLabel>
          <Textarea id="c-reason" name="reason" maxLength={1000} required placeholder="e.g. Forgot to check in, was at a client meeting" />
          <FieldError>{fields.reason && `Reason ${fields.reason}`}</FieldError>
        </Field>
      </FieldGroup>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">Cancel</Button>
        </DialogClose>
        <Button type="submit" disabled={pending}>Send request</Button>
      </DialogFooter>
    </form>
  );
}

// ── Approve / reject ───────────────────────────────────────

export type ReviewTarget = { kind: "leave" | "corrections"; id: string; approve: boolean; title: string; detail: string };

export function ReviewDialog({ target, onOpenChange }: { target: ReviewTarget | null; onOpenChange: (o: boolean) => void }) {
  return (
    <Dialog open={!!target} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        {target && <ReviewForm key={target.id + target.approve} target={target} onDone={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  );
}

function ReviewForm({ target, onDone }: { target: ReviewTarget; onDone: () => void }) {
  const { fields, pending, run } = useSubmit(onDone);
  const verb = target.approve ? "Approve" : "Reject";
  return (
    <form
      className="grid gap-6"
      onSubmit={(e) => {
        e.preventDefault();
        const note = String(new FormData(e.currentTarget).get("note") ?? "");
        run(
          () => api(`/hr/${target.kind}/${target.id}/${target.approve ? "approve" : "reject"}`, { method: "POST", body: { note } }),
          target.approve ? "Approved" : "Rejected",
        );
      }}
    >
      <DialogHeader>
        <DialogTitle>
          {verb} {target.title}
        </DialogTitle>
        <DialogDescription>{target.detail}</DialogDescription>
      </DialogHeader>
      <Field data-invalid={!!fields.note}>
        <FieldLabel htmlFor="note">{target.approve ? "Note (optional)" : "Reason"}</FieldLabel>
        <Textarea id="note" name="note" maxLength={1000} required={!target.approve} autoFocus />
        <FieldDescription>They&apos;ll see this in their notification.</FieldDescription>
        <FieldError>{fields.note && `Note ${fields.note}`}</FieldError>
      </Field>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">Cancel</Button>
        </DialogClose>
        <Button type="submit" disabled={pending} variant={target.approve ? "default" : "destructive"}>
          {verb}
        </Button>
      </DialogFooter>
    </form>
  );
}
