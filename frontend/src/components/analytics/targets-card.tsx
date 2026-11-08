"use client";

import { useState } from "react";
import { AlertTriangleIcon, CheckCircle2Icon, ChevronLeftIcon, ChevronRightIcon, CopyIcon, PencilIcon, PlusIcon, TrendingUpIcon, XCircleIcon } from "lucide-react";
import { ApiError } from "@/lib/api";
import { formatMoney } from "@/lib/format";
import { addMonth, monthLabel, useSaveTarget, useTargets, type TeamTarget } from "@/lib/analytics";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";

// Each team's revenue target for a month (after the marketplace's fee) against what it has
// delivered. The marker on each bar is where the team should be by now if it delivered
// evenly through the month, so "behind" means behind that pace, not behind the total.

type Standing = { label: string; icon: typeof CheckCircle2Icon; className: string };

function standing(t: TeamTarget, elapsed: number): Standing | null {
  if (!t.amount) return null;
  const done = Number(t.achieved) / Number(t.amount);
  if (done >= 1) return { label: "Target hit", icon: CheckCircle2Icon, className: "text-emerald-700 dark:text-emerald-400" };
  if (elapsed >= 1) return { label: "Missed", icon: XCircleIcon, className: "text-red-600 dark:text-red-400" };
  if (done >= elapsed) return { label: "On track", icon: TrendingUpIcon, className: "text-emerald-700 dark:text-emerald-400" };
  return { label: "Behind pace", icon: AlertTriangleIcon, className: "text-amber-700 dark:text-amber-400" };
}

export function TargetsCard({ month, onMonth, isCEO, teamId }: { month: string; onMonth: (m: string) => void; isCEO: boolean; teamId: string | null }) {
  const q = useTargets(month);
  const prev = useTargets(addMonth(month, -1), isCEO);
  const save = useSaveTarget();
  const [editing, setEditing] = useState<TeamTarget | null>(null);
  const d = q.data;
  const teams = d?.teams.filter((t) => !teamId || t.team_id === teamId);
  const canCopy = isCEO && d && d.teams.every((t) => !t.amount) && prev.data?.teams.some((t) => t.amount);

  async function copyLast() {
    for (const t of prev.data!.teams) {
      if (t.amount && d!.teams.some((x) => x.team_id === t.team_id)) {
        await save.mutateAsync({ teamId: t.team_id, month, amount: t.amount, currency: t.currency });
      }
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Revenue targets · {monthLabel(month, true)}</CardTitle>
        <CardDescription>
          What each team should deliver this month, after the marketplace&apos;s fee. The line on each bar is where the team should be by now
          {d && d.elapsed > 0 && d.elapsed < 1 ? ` (${Math.round(d.elapsed * 100)}% of the month gone)` : ""}.
          {isCEO ? " Only you can set targets." : " Set by the CEO."}
        </CardDescription>
        <CardAction className="flex items-center gap-1">
          <Button variant="ghost" size="icon" aria-label="Previous month" onClick={() => onMonth(addMonth(month, -1))}>
            <ChevronLeftIcon />
          </Button>
          <Button variant="ghost" size="icon" aria-label="Next month" onClick={() => onMonth(addMonth(month, 1))}>
            <ChevronRightIcon />
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-5">
        {canCopy && (
          <Button variant="outline" size="sm" onClick={copyLast} disabled={save.isPending}>
            <CopyIcon /> Use {monthLabel(addMonth(month, -1))}&apos;s targets
          </Button>
        )}
        {!teams ? (
          <Skeleton className="h-32" />
        ) : teams.length === 0 ? (
          <p className="text-sm text-muted-foreground">No teams yet.</p>
        ) : (
          <ul className="space-y-5">
            {teams.map((t) => (
              <TargetRow key={t.team_id} t={t} elapsed={d!.elapsed} isCEO={isCEO} onEdit={() => setEditing(t)} />
            ))}
          </ul>
        )}
      </CardContent>
      {editing && <TargetDialog t={editing} month={month} onClose={() => setEditing(null)} />}
    </Card>
  );
}

function TargetRow({ t, elapsed, isCEO, onEdit }: { t: TeamTarget; elapsed: number; isCEO: boolean; onEdit: () => void }) {
  const target = Number(t.amount);
  const achieved = Number(t.achieved);
  const s = standing(t, elapsed);
  const pct = target ? Math.min(100, (achieved / target) * 100) : 0;
  const expected = target * Math.min(1, elapsed);
  return (
    <li className="space-y-2">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <div className="min-w-0">
          <span className="font-medium">{t.team_name}</span>
          {t.lead_name && <span className="ml-2 text-xs text-muted-foreground">Lead: {t.lead_name}</span>}
        </div>
        <div className="flex items-center gap-3 text-sm">
          {s && (
            <span className={cn("inline-flex items-center gap-1 text-xs font-medium", s.className)}>
              <s.icon className="size-3.5" /> {s.label}
            </span>
          )}
          {t.amount ? (
            <span className="tabular-nums">
              <span className="font-semibold">{formatMoney(t.achieved, t.currency)}</span>
              <span className="text-muted-foreground"> of {formatMoney(t.amount, t.currency)}</span>
            </span>
          ) : (
            <span className="text-muted-foreground">
              No target · {formatMoney(t.achieved, t.currency)} delivered
            </span>
          )}
          {isCEO && (
            <Button variant="ghost" size="sm" className="h-7" onClick={onEdit}>
              {t.amount ? <PencilIcon /> : <PlusIcon />} {t.amount ? "Edit" : "Set target"}
            </Button>
          )}
        </div>
      </div>
      {t.amount && (
        <>
          <div
            className="relative h-2.5 rounded-full bg-muted"
            role="meter"
            aria-valuemin={0}
            aria-valuemax={target}
            aria-valuenow={achieved}
            aria-label={`${t.team_name}: ${formatMoney(t.achieved, t.currency)} of ${formatMoney(t.amount, t.currency)}`}
          >
            <div className="h-2.5 rounded-full bg-flow-out" style={{ width: `${pct}%`, minWidth: achieved ? 4 : 0 }} />
            {elapsed > 0 && elapsed < 1 && (
              <div className="absolute -top-1 h-4.5 w-0.5 rounded-full bg-foreground/70" style={{ left: `calc(${elapsed * 100}% - 1px)` }} aria-hidden />
            )}
          </div>
          <div className="flex flex-wrap justify-between gap-x-4 text-xs text-muted-foreground">
            <span>
              {Math.round((achieved / target) * 100)}% · {t.projects} project{t.projects === 1 ? "" : "s"} delivered
              {t.other_currency > 0 && ` · ${t.other_currency} in other currencies not counted`}
            </span>
            {elapsed > 0 && elapsed < 1 && achieved < target && (
              <span>
                {achieved >= expected ? "Ahead of" : `${formatMoney(String(expected - achieved), t.currency)} behind`} pace · {formatMoney(String(target - achieved), t.currency)} to go
              </span>
            )}
          </div>
        </>
      )}
    </li>
  );
}

function TargetDialog({ t, month, onClose }: { t: TeamTarget; month: string; onClose: () => void }) {
  const save = useSaveTarget();
  const [amount, setAmount] = useState(t.amount ? String(Number(t.amount)) : "");
  const [currency, setCurrency] = useState(t.currency || "USD");
  const [fields, setFields] = useState<Record<string, string>>({});

  async function submit(value: string | null) {
    setFields({});
    try {
      await save.mutateAsync({ teamId: t.team_id, month, amount: value, currency });
      onClose();
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length) setFields(err.fields);
      else setFields({ amount: err instanceof Error ? err.message : "could not be saved" });
    }
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <form
          className="grid gap-6"
          onSubmit={(e) => {
            e.preventDefault();
            submit(amount);
          }}
        >
          <DialogHeader>
            <DialogTitle>
              {t.team_name} · {monthLabel(month, true)}
            </DialogTitle>
            <DialogDescription>Revenue the team should deliver in the month, after the marketplace&apos;s fee.</DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <div className="grid grid-cols-[1fr_6rem] gap-3">
              <Field data-invalid={!!fields.amount}>
                <FieldLabel htmlFor="target-amount">Target</FieldLabel>
                <Input id="target-amount" inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} required autoFocus placeholder="3000" />
                <FieldError>{fields.amount && `Target ${fields.amount}`}</FieldError>
              </Field>
              <Field data-invalid={!!fields.currency}>
                <FieldLabel htmlFor="target-currency">Currency</FieldLabel>
                <Input id="target-currency" value={currency} onChange={(e) => setCurrency(e.target.value.toUpperCase())} maxLength={3} required />
                <FieldError>{fields.currency}</FieldError>
              </Field>
            </div>
            <FieldDescription>
              Only projects in this currency count towards it. So far this month: {formatMoney(t.achieved, t.currency)}.
            </FieldDescription>
          </FieldGroup>
          <DialogFooter className="sm:justify-between">
            {t.amount ? (
              <Button type="button" variant="ghost" className="text-destructive" onClick={() => submit(null)} disabled={save.isPending}>
                Remove target
              </Button>
            ) : (
              <span />
            )}
            <div className="flex gap-2">
              <DialogClose asChild>
                <Button type="button" variant="outline">
                  Cancel
                </Button>
              </DialogClose>
              <Button type="submit" disabled={save.isPending}>
                Save
              </Button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
