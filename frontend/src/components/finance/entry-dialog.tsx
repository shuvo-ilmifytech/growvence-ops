"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { InfoIcon } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import { FINANCE_KEY, formatBdt, useFinanceCategories, useRateOn, type FinanceEntry, type FinanceKind } from "@/lib/finance";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";

/**
 * Add an income line (entry undefined) or edit one. Expenses never come through here: they
 * are raised as invoices and posted by the CEO's approval, so the kind of an entry is fixed.
 * Rendered only while open, so state starts fresh.
 */
export function EntryDialog({ entry, today, onClose }: { entry?: FinanceEntry; today: string; onClose: () => void }) {
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <EntryForm entry={entry} today={today} onDone={onClose} />
      </DialogContent>
    </Dialog>
  );
}

function EntryForm({ entry, today, onDone }: { entry?: FinanceEntry; today: string; onDone: () => void }) {
  const qc = useQueryClient();
  const categories = useFinanceCategories();
  const kind: FinanceKind = entry?.kind ?? "income";
  const [categoryId, setCategoryId] = useState(entry?.category_id ?? "");
  const [amount, setAmount] = useState(entry?.amount ?? "");
  const [currency, setCurrency] = useState(entry?.currency ?? "BDT");
  const [date, setDate] = useState(entry?.entry_date ?? today);
  // An edited foreign-currency entry keeps its own rate unless someone changes it.
  const [rate, setRate] = useState(entry && entry.currency !== "BDT" ? String(Number(entry.rate_to_bdt)) : "");
  const [description, setDescription] = useState(entry?.description ?? "");
  const [fields, setFields] = useState<Record<string, string>>({});
  const [pending, setPending] = useState(false);

  const cur = currency.trim().toUpperCase();
  const foreign = cur !== "BDT";
  const setRateFor = useRateOn(cur, date);
  const effectiveRate = foreign ? rate.trim() || setRateFor.data || "" : "1";
  const converted = Number(amount.replace(/,/g, "")) * Number(effectiveRate);

  const options = (categories.data ?? []).filter((c) => c.kind === kind && (c.is_active || c.id === entry?.category_id));

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setFields({});
    setPending(true);
    try {
      const body = { category_id: categoryId, amount, currency: cur, rate_to_bdt: foreign ? rate.trim() : "", entry_date: date, description };
      if (entry) await api(`/finance/entries/${entry.id}`, { method: "PATCH", body });
      else await api("/finance/entries", { method: "POST", body });
      toast.success(entry ? "Entry updated" : "Entry added");
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
        <DialogTitle>{entry ? `Edit ${kind === "income" ? "income" : "expense"}` : "Add income"}</DialogTitle>
        <DialogDescription>Converted to BDT at the rate kept on the entry.</DialogDescription>
      </DialogHeader>
      <FieldGroup>
        <Field data-invalid={!!fields.category_id}>
          <FieldLabel htmlFor="fin-category">Category</FieldLabel>
          <Select value={categoryId} onValueChange={setCategoryId}>
            <SelectTrigger id="fin-category" className="w-full" aria-invalid={!!fields.category_id}>
              <SelectValue placeholder="Pick a category" />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectLabel>{kind === "income" ? "Income" : "Expense"}</SelectLabel>
                {options.map((c) => (
                  <SelectItem key={c.id} value={c.id}>
                    {c.name}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <FieldDescription className="flex gap-1.5">
            <InfoIcon className="mt-0.5 size-3.5 shrink-0" />
            {kind === "income"
              ? "Expenses are raised on the Invoices page; salaries go under Payroll, and marketplace project earnings are counted automatically."
              : "This expense came through an invoice. Its category stays an expense one."}
          </FieldDescription>
          <FieldError>{fields.category_id && `Category: ${fields.category_id}`}</FieldError>
        </Field>
        <div className="grid grid-cols-[1fr_6rem] gap-3">
          <Field data-invalid={!!fields.amount}>
            <FieldLabel htmlFor="fin-amount">Amount</FieldLabel>
            <Input id="fin-amount" inputMode="decimal" placeholder="15000" value={amount} onChange={(e) => setAmount(e.target.value)} required autoFocus />
            <FieldError>{fields.amount && `Amount ${fields.amount}`}</FieldError>
          </Field>
          <Field data-invalid={!!fields.currency}>
            <FieldLabel htmlFor="fin-currency">Currency</FieldLabel>
            <Input id="fin-currency" value={currency} onChange={(e) => setCurrency(e.target.value)} maxLength={3} className="uppercase" required />
            <FieldError>{fields.currency}</FieldError>
          </Field>
        </div>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field data-invalid={!!fields.entry_date}>
            <FieldLabel htmlFor="fin-date">Date</FieldLabel>
            <Input id="fin-date" type="date" value={date} onChange={(e) => setDate(e.target.value)} required />
            <FieldError>{fields.entry_date && `Date ${fields.entry_date}`}</FieldError>
          </Field>
          {foreign && (
            <Field data-invalid={!!fields.rate_to_bdt}>
              <FieldLabel htmlFor="fin-rate">Rate (BDT per 1 {cur || "unit"})</FieldLabel>
              <Input
                id="fin-rate"
                inputMode="decimal"
                placeholder={setRateFor.data ? String(Number(setRateFor.data)) : "e.g. 122.50"}
                value={rate}
                onChange={(e) => setRate(e.target.value)}
              />
              <FieldDescription>
                {setRateFor.data ? "Leave empty to use the rate set for this date." : `No ${cur} rate is set for this date. Enter the rate you got.`}
              </FieldDescription>
              <FieldError>{fields.rate_to_bdt}</FieldError>
            </Field>
          )}
        </div>
        <Field data-invalid={!!fields.description}>
          <FieldLabel htmlFor="fin-description">Description</FieldLabel>
          <Textarea
            id="fin-description"
            rows={2}
            maxLength={500}
            placeholder={kind === "income" ? "e.g. Website for Acme, paid by bank transfer" : "e.g. November office rent"}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            required
          />
          <FieldError>{fields.description && `Description ${fields.description}`}</FieldError>
        </Field>
        {foreign && Number.isFinite(converted) && converted > 0 && (
          <p className="text-sm text-muted-foreground">
            Counts as <span className="font-medium text-foreground tabular-nums">{formatBdt(converted)}</span>
          </p>
        )}
      </FieldGroup>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            Cancel
          </Button>
        </DialogClose>
        <Button type="submit" disabled={pending || !categoryId}>
          {entry ? "Save changes" : "Add income"}
        </Button>
      </DialogFooter>
    </form>
  );
}
