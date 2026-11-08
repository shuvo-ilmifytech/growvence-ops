"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { MinusIcon, PlusIcon, XIcon } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import { formatMoney } from "@/lib/format";
import { adjustmentTotal, FINANCE_KEY, formatBdt, formatMonth, useRateOn, type PayrollAdjustment, type PayrollRow } from "@/lib/finance";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";

const REASONS: Record<PayrollAdjustment["kind"], string[]> = {
  bonus: ["Performance bonus", "Festival bonus", "Overtime", "Incentive", "Allowance"],
  deduction: ["Unpaid absence", "Late attendance", "Salary advance", "Loan repayment", "Tax"],
};

/** "1234.5" for amounts: at most 2 decimals, no trailing zeros. */
function money(n: number) {
  return String(Math.round(n * 100) / 100);
}

/** Record or edit one person's pay for a month. Rendered only while open, so state starts fresh. */
export function PaymentDialog({ row, month, today, onClose }: { row: PayrollRow; month: string; today: string; onClose: () => void }) {
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <PaymentForm row={row} month={month} today={today} onDone={onClose} />
      </DialogContent>
    </Dialog>
  );
}

function PaymentForm({ row, month, today, onDone }: { row: PayrollRow; month: string; today: string; onDone: () => void }) {
  const qc = useQueryClient();
  const p = row.payment;
  const [base, setBase] = useState(p ? money(Number(p.base_amount)) : row.salary_amount ? money(Number(row.salary_amount)) : "");
  const [currency, setCurrency] = useState(p?.currency ?? row.salary_currency ?? "BDT");
  const [paidOn, setPaidOn] = useState(p?.paid_on ?? today);
  // An edited foreign-currency payment keeps its own rate unless someone changes it.
  const [rate, setRate] = useState(p && p.currency !== "BDT" ? String(Number(p.rate_to_bdt)) : "");
  const [note, setNote] = useState(p?.note ?? "");
  const [lines, setLines] = useState<PayrollAdjustment[]>(p?.adjustments.map((a) => ({ ...a, amount: money(Number(a.amount)) })) ?? []);
  const [fields, setFields] = useState<Record<string, string>>({});
  const [pending, setPending] = useState(false);

  const cur = currency.trim().toUpperCase();
  const foreign = cur !== "BDT";
  const setRateFor = useRateOn(cur, paidOn);
  const effectiveRate = foreign ? rate.trim() || setRateFor.data || "" : "1";
  const baseNum = Number(base.replace(/,/g, "")) || 0;
  const bonuses = adjustmentTotal(lines, "bonus");
  const deductions = adjustmentTotal(lines, "deduction");
  const net = baseNum + bonuses - deductions;

  // Unpaid absence: unpaid leave plus days absent without leave, at the salary ÷ working days.
  const att = row.attendance;
  const missedDays = att ? att.unpaid_leave_days + att.absent : 0;
  const perDay = att && att.working_days > 0 ? baseNum / att.working_days : 0;

  function update(i: number, patch: Partial<PayrollAdjustment>) {
    setLines((ls) => ls.map((l, j) => (j === i ? { ...l, ...patch } : l)));
  }
  function add(line: PayrollAdjustment) {
    setLines((ls) => [...ls, line]);
  }

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setFields({});
    setPending(true);
    try {
      await api(`/finance/payroll/${row.user_id}/${month}`, {
        method: "PUT",
        body: { base_amount: base, currency: cur, rate_to_bdt: foreign ? rate.trim() : "", paid_on: paidOn, note, adjustments: lines },
      });
      toast.success(p ? "Pay updated" : "Pay recorded");
      qc.invalidateQueries({ queryKey: FINANCE_KEY });
      onDone();
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length) setFields(err.fields);
      else toast.error(err instanceof Error ? err.message : "Could not save");
    } finally {
      setPending(false);
    }
  }

  return (
    <form onSubmit={onSubmit} className="grid gap-6">
      <DialogHeader>
        <DialogTitle>
          {row.name}: {formatMonth(month)}
        </DialogTitle>
        <DialogDescription>
          {row.salary_amount ? `Fixed salary ${formatMoney(row.salary_amount, row.salary_currency ?? "BDT")}. ` : "No fixed salary is set. "}
          Add bonuses and deductions below; the net is what counts as payroll.
        </DialogDescription>
      </DialogHeader>
      <FieldGroup>
        <div className="grid grid-cols-[1fr_6rem] gap-3">
          <Field data-invalid={!!fields.base_amount}>
            <FieldLabel htmlFor="pay-base">Salary for the month</FieldLabel>
            <Input id="pay-base" inputMode="decimal" value={base} onChange={(e) => setBase(e.target.value)} required autoFocus />
            <FieldError>{fields.base_amount && `Salary ${fields.base_amount}`}</FieldError>
          </Field>
          <Field data-invalid={!!fields.currency}>
            <FieldLabel htmlFor="pay-currency">Currency</FieldLabel>
            <Input id="pay-currency" value={currency} onChange={(e) => setCurrency(e.target.value)} maxLength={3} className="uppercase" required />
            <FieldError>{fields.currency}</FieldError>
          </Field>
        </div>

        <div className="space-y-2">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="text-sm font-medium">Bonuses and deductions</span>
            <div className="flex flex-wrap gap-2">
              <Button type="button" size="sm" variant="outline" onClick={() => add({ kind: "bonus", reason: "", amount: "" })}>
                <PlusIcon /> Bonus
              </Button>
              <Button type="button" size="sm" variant="outline" onClick={() => add({ kind: "deduction", reason: "", amount: "" })}>
                <MinusIcon /> Deduction
              </Button>
            </div>
          </div>
          {att && missedDays > 0 && perDay > 0 && (
            <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border bg-muted/40 p-2 text-sm">
              <span className="text-muted-foreground">
                {[att.unpaid_leave_days > 0 && `${att.unpaid_leave_days} day${att.unpaid_leave_days === 1 ? "" : "s"} unpaid leave`, att.absent > 0 && `${att.absent} absent`]
                  .filter(Boolean)
                  .join(", ")}{" "}
                of {att.working_days} working days
              </span>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                onClick={() => add({ kind: "deduction", reason: `Unpaid absence (${missedDays} day${missedDays === 1 ? "" : "s"})`, amount: money(perDay * missedDays) })}
              >
                Deduct {formatMoney(money(perDay * missedDays), cur || "BDT")}
              </Button>
            </div>
          )}
          {lines.length === 0 && <p className="text-sm text-muted-foreground">None. The salary is paid as it is.</p>}
          {lines.map((l, i) => (
            <Field key={i} data-invalid={!!fields[`adjustments.${i}`]}>
              <div className="grid grid-cols-[7.5rem_1fr_6.5rem_auto] items-center gap-2">
                <Select value={l.kind} onValueChange={(v) => update(i, { kind: v as PayrollAdjustment["kind"] })}>
                  <SelectTrigger aria-label="Type" className={cn("w-full", l.kind === "deduction" && "text-destructive")}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="bonus">+ Bonus</SelectItem>
                    <SelectItem value="deduction">− Deduction</SelectItem>
                  </SelectContent>
                </Select>
                <Input aria-label="Reason" placeholder="Reason" list={`pay-reasons-${l.kind}`} maxLength={120} value={l.reason} onChange={(e) => update(i, { reason: e.target.value })} />
                <Input aria-label="Amount" inputMode="decimal" placeholder="0" value={l.amount} onChange={(e) => update(i, { amount: e.target.value })} className="text-right" />
                <Button type="button" size="icon" variant="ghost" aria-label="Remove line" onClick={() => setLines((ls) => ls.filter((_, j) => j !== i))}>
                  <XIcon />
                </Button>
              </div>
              <FieldError>{fields[`adjustments.${i}`]}</FieldError>
            </Field>
          ))}
          {(["bonus", "deduction"] as const).map((k) => (
            <datalist key={k} id={`pay-reasons-${k}`}>
              {REASONS[k].map((r) => (
                <option key={r} value={r} />
              ))}
            </datalist>
          ))}
          {fields.adjustments && <p className="text-sm text-destructive">{fields.adjustments}</p>}
        </div>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field data-invalid={!!fields.paid_on}>
            <FieldLabel htmlFor="pay-date">Paid on</FieldLabel>
            <Input id="pay-date" type="date" value={paidOn} onChange={(e) => setPaidOn(e.target.value)} required />
            <FieldError>{fields.paid_on && `Date ${fields.paid_on}`}</FieldError>
          </Field>
          {foreign && (
            <Field data-invalid={!!fields.rate_to_bdt}>
              <FieldLabel htmlFor="pay-rate">Rate (BDT per 1 {cur || "unit"})</FieldLabel>
              <Input
                id="pay-rate"
                inputMode="decimal"
                placeholder={setRateFor.data ? String(Number(setRateFor.data)) : "e.g. 122.50"}
                value={rate}
                onChange={(e) => setRate(e.target.value)}
              />
              <FieldDescription>
                {setRateFor.data ? "Leave empty to use the rate set for this date." : `No ${cur} rate is set for this date. Enter the rate you paid at.`}
              </FieldDescription>
              <FieldError>{fields.rate_to_bdt}</FieldError>
            </Field>
          )}
        </div>
        <Field data-invalid={!!fields.note}>
          <FieldLabel htmlFor="pay-note">Note</FieldLabel>
          <Textarea id="pay-note" rows={2} maxLength={500} placeholder="e.g. Paid by bank transfer" value={note} onChange={(e) => setNote(e.target.value)} />
          <FieldError>{fields.note}</FieldError>
        </Field>

        <dl className="grid grid-cols-[1fr_auto] gap-x-4 gap-y-1 rounded-md border p-3 text-sm tabular-nums">
          <dt className="text-muted-foreground">Salary</dt>
          <dd className="text-right">{formatMoney(money(baseNum), cur || "BDT")}</dd>
          <dt className="text-muted-foreground">Bonuses</dt>
          <dd className="text-right">+{formatMoney(money(bonuses), cur || "BDT")}</dd>
          <dt className="text-muted-foreground">Deductions</dt>
          <dd className="text-right">−{formatMoney(money(deductions), cur || "BDT")}</dd>
          <dt className="border-t pt-1 font-medium">Net pay</dt>
          <dd className={cn("border-t pt-1 text-right font-semibold", net < 0 && "text-destructive")}>
            {formatMoney(money(net), cur || "BDT")}
            {foreign && Number(effectiveRate) > 0 && <div className="text-xs font-normal text-muted-foreground">{formatBdt(net * Number(effectiveRate))}</div>}
          </dd>
        </dl>
      </FieldGroup>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            Cancel
          </Button>
        </DialogClose>
        <Button type="submit" disabled={pending || net < 0}>
          {p ? "Save changes" : "Record pay"}
        </Button>
      </DialogFooter>
    </form>
  );
}
