"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { PlusIcon, Trash2Icon } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import { FINANCE_KEY, formatAmount, formatBdt, invoiceRef, itemQuantity, useRateOn, type FinanceCategory, type Invoice } from "@/lib/finance";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";

/** One line being typed. The amount is worked out, never entered. */
type Line = { key: number; category_id: string; description: string; quantity: string; unit_price: string };

let nextKey = 1;
const blankLine = (): Line => ({ key: nextKey++, category_id: "", description: "", quantity: "1", unit_price: "" });

const amountOf = (l: Line) => (Number(l.quantity.replace(/,/g, "")) || 0) * (Number(l.unit_price.replace(/,/g, "")) || 0);

/** Raise an invoice (invoice undefined) or correct one that is still waiting. */
export function InvoiceDialog({
  invoice,
  categories,
  today,
  onClose,
}: {
  invoice?: Invoice;
  categories: FinanceCategory[];
  today: string;
  onClose: () => void;
}) {
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-2xl">
        <InvoiceForm invoice={invoice} categories={categories} today={today} onDone={onClose} />
      </DialogContent>
    </Dialog>
  );
}

function InvoiceForm({
  invoice,
  categories,
  today,
  onDone,
}: {
  invoice?: Invoice;
  categories: FinanceCategory[];
  today: string;
  onDone: () => void;
}) {
  const qc = useQueryClient();
  const [description, setDescription] = useState(invoice?.description ?? "");
  const [currency, setCurrency] = useState(invoice?.currency ?? "BDT");
  const [date, setDate] = useState(invoice?.expense_date ?? today);
  // An edited foreign-currency invoice keeps its own rate unless someone changes it.
  const [rate, setRate] = useState(invoice && invoice.currency !== "BDT" ? String(Number(invoice.rate_to_bdt)) : "");
  const [lines, setLines] = useState<Line[]>(
    invoice?.items_list.length
      ? invoice.items_list.map((it) => ({
          key: nextKey++,
          category_id: it.category_id,
          description: it.description,
          quantity: itemQuantity(it.quantity),
          unit_price: String(Number(it.unit_price)),
        }))
      : [blankLine()],
  );
  const [fields, setFields] = useState<Record<string, string>>({});
  const [pending, setPending] = useState(false);

  const cur = currency.trim().toUpperCase();
  const foreign = cur !== "BDT";
  const setRateFor = useRateOn(cur, date);
  const effectiveRate = foreign ? rate.trim() || setRateFor.data || "" : "1";
  const total = lines.reduce((s, l) => s + amountOf(l), 0);
  const totalBdt = total * Number(effectiveRate);

  // A category switched off since the invoice was raised still shows, so it can be saved.
  const known = new Set(categories.map((c) => c.id));
  const options = [
    ...categories,
    ...(invoice?.items_list ?? [])
      .filter((it) => !known.has(it.category_id))
      .map((it) => ({ id: it.category_id, name: it.category_name }) as FinanceCategory),
  ];

  const setLine = (key: number, patch: Partial<Line>) => setLines((ls) => ls.map((l) => (l.key === key ? { ...l, ...patch } : l)));
  const complete = lines.length > 0 && lines.every((l) => l.category_id && l.description.trim() && amountOf(l) > 0);

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setFields({});
    setPending(true);
    try {
      const body = {
        currency: cur,
        rate_to_bdt: foreign ? rate.trim() : "",
        expense_date: date,
        description,
        items: lines.map((l) => ({
          category_id: l.category_id,
          description: l.description,
          quantity: l.quantity,
          unit_price: l.unit_price,
        })),
      };
      if (invoice) await api(`/finance/invoices/${invoice.id}`, { method: "PATCH", body });
      else await api("/finance/invoices", { method: "POST", body });
      toast.success(invoice ? `${invoiceRef(invoice.number)} updated` : "Invoice raised");
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
        <DialogTitle>{invoice ? `Edit ${invoiceRef(invoice.number)}` : "Raise an invoice"}</DialogTitle>
        <DialogDescription>
          {invoice
            ? "It still has to be approved, so what you leave here is what gets approved."
            : "One invoice is one purchase: add a line for each thing on it. HR approves it, then the CEO, and only then does it count as an expense."}
        </DialogDescription>
      </DialogHeader>
      <FieldGroup>
        <Field data-invalid={!!fields.description}>
          <FieldLabel htmlFor="inv-description">What is it for</FieldLabel>
          <Textarea
            id="inv-description"
            rows={2}
            maxLength={200}
            placeholder="e.g. Furniture and tea for the meeting room"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            required
            autoFocus
          />
          <FieldError>{fields.description && `Description ${fields.description}`}</FieldError>
        </Field>

        <div className="space-y-3">
          <div className="flex items-end justify-between gap-2">
            <FieldLabel className="mb-0">What is on it</FieldLabel>
            <span className="text-xs text-muted-foreground">Amounts in {cur || "BDT"}</span>
          </div>
          {/* Column headings on a wide screen; each line repeats them on a phone. */}
          <div className="hidden gap-2 px-1 text-xs text-muted-foreground sm:grid sm:grid-cols-[1fr_6rem_7rem_6rem_2.25rem]">
            <span>Item</span>
            <span>Qty</span>
            <span>Unit price</span>
            <span className="text-right">Amount</span>
            <span />
          </div>
          {lines.map((l, i) => (
            <div key={l.key} className="rounded-lg border p-3 sm:rounded-none sm:border-0 sm:p-0" data-invalid={!!fields[`items.${i}`]}>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_6rem_7rem_6rem_2.25rem] sm:items-center">
                <div className="grid gap-2">
                  <Input
                    aria-label={`Item ${i + 1}`}
                    placeholder="Ceiling fan"
                    maxLength={200}
                    value={l.description}
                    onChange={(e) => setLine(l.key, { description: e.target.value })}
                    aria-invalid={!!fields[`items.${i}`]}
                  />
                  <Select value={l.category_id} onValueChange={(v) => setLine(l.key, { category_id: v })}>
                    <SelectTrigger aria-label={`Category for item ${i + 1}`} className="w-full" aria-invalid={!!fields[`items.${i}`]}>
                      <SelectValue placeholder="Category" />
                    </SelectTrigger>
                    <SelectContent>
                      {options.map((c) => (
                        <SelectItem key={c.id} value={c.id}>
                          {c.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <Input
                  aria-label={`Quantity for item ${i + 1}`}
                  inputMode="decimal"
                  placeholder="1"
                  value={l.quantity}
                  onChange={(e) => setLine(l.key, { quantity: e.target.value })}
                  className="self-start"
                />
                <Input
                  aria-label={`Unit price for item ${i + 1}`}
                  inputMode="decimal"
                  placeholder="2000"
                  value={l.unit_price}
                  onChange={(e) => setLine(l.key, { unit_price: e.target.value })}
                  className="self-start"
                />
                <div className="self-start py-2 text-sm font-medium tabular-nums sm:text-right">
                  {amountOf(l) > 0 ? formatAmount(amountOf(l), cur || "BDT") : <span className="text-muted-foreground">—</span>}
                </div>
                <Button
                  type="button"
                  size="icon"
                  variant="ghost"
                  aria-label={`Remove item ${i + 1}`}
                  className="self-start"
                  disabled={lines.length === 1}
                  onClick={() => setLines((ls) => ls.filter((x) => x.key !== l.key))}
                >
                  <Trash2Icon />
                </Button>
              </div>
              {fields[`items.${i}`] && <p className="mt-1 text-sm text-destructive">Line {i + 1}: {fields[`items.${i}`]}</p>}
            </div>
          ))}
          {fields.items && <p className="text-sm text-destructive">{fields.items}</p>}
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Button type="button" variant="outline" size="sm" onClick={() => setLines((ls) => [...ls, blankLine()])}>
              <PlusIcon /> Add a line
            </Button>
            <div className="text-sm">
              <span className="text-muted-foreground">Total </span>
              <span className="font-semibold tabular-nums">{formatAmount(total, cur || "BDT")}</span>
              {foreign && Number(effectiveRate) > 0 && (
                <span className="text-muted-foreground tabular-nums"> · {formatBdt(totalBdt)}</span>
              )}
            </div>
          </div>
        </div>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <Field data-invalid={!!fields.expense_date}>
            <FieldLabel htmlFor="inv-date">Date</FieldLabel>
            <Input id="inv-date" type="date" value={date} onChange={(e) => setDate(e.target.value)} required />
            <FieldDescription>The day it was (or will be) bought.</FieldDescription>
            <FieldError>{fields.expense_date && `Date ${fields.expense_date}`}</FieldError>
          </Field>
          <Field data-invalid={!!fields.currency}>
            <FieldLabel htmlFor="inv-currency">Currency</FieldLabel>
            <Input id="inv-currency" value={currency} onChange={(e) => setCurrency(e.target.value)} maxLength={3} className="uppercase" required />
            <FieldError>{fields.currency}</FieldError>
          </Field>
          {foreign && (
            <Field data-invalid={!!fields.rate_to_bdt}>
              <FieldLabel htmlFor="inv-rate">Rate (BDT per 1 {cur || "unit"})</FieldLabel>
              <Input
                id="inv-rate"
                inputMode="decimal"
                placeholder={setRateFor.data ? String(Number(setRateFor.data)) : "e.g. 122.50"}
                value={rate}
                onChange={(e) => setRate(e.target.value)}
              />
              <FieldDescription>
                {setRateFor.data ? "Leave empty to use the rate set for this date." : `No ${cur} rate is set for this date. Enter the rate you paid.`}
              </FieldDescription>
              <FieldError>{fields.rate_to_bdt}</FieldError>
            </Field>
          )}
        </div>
      </FieldGroup>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            Cancel
          </Button>
        </DialogClose>
        <Button type="submit" disabled={pending || !complete}>
          {invoice ? "Save changes" : "Raise invoice"}
        </Button>
      </DialogFooter>
    </form>
  );
}

export type ReviewTarget = { invoice: Invoice; approve: boolean; final: boolean };

/** Approve or reject at this person's step. The final approval is what posts it. */
export function InvoiceReviewDialog({ target, onOpenChange }: { target: ReviewTarget | null; onOpenChange: (o: boolean) => void }) {
  return (
    <Dialog open={!!target} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        {target && <ReviewForm key={target.invoice.id + String(target.approve)} target={target} onDone={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  );
}

function ReviewForm({ target, onDone }: { target: ReviewTarget; onDone: () => void }) {
  const { invoice, approve, final } = target;
  const qc = useQueryClient();
  const [fields, setFields] = useState<Record<string, string>>({});
  const [pending, setPending] = useState(false);
  const categories = [...new Set(invoice.items_list.map((it) => it.category_name))];

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const note = String(new FormData(e.currentTarget).get("note") ?? "");
    setFields({});
    setPending(true);
    try {
      await api(`/finance/invoices/${invoice.id}/${approve ? "approve" : "reject"}`, { method: "POST", body: { note } });
      toast.success(approve ? (final ? `${invoiceRef(invoice.number)} approved and added to the ledger` : "Approved, sent to the CEO") : "Rejected");
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
          {approve ? "Approve" : "Reject"} {invoiceRef(invoice.number)}
        </DialogTitle>
        <DialogDescription>
          {invoice.description} · <span className="tabular-nums">{formatBdt(invoice.amount_bdt)}</span> over {invoice.items}{" "}
          {invoice.items === 1 ? "line" : "lines"} · raised by {invoice.requested_by_name}
          {approve &&
            (final
              ? `. This adds it to the ledger: ${categories.length === 1 ? "one line" : `${categories.length} lines`} under ${categories.join(" and ")}.`
              : ". The CEO gives the final approval.")}
        </DialogDescription>
      </DialogHeader>
      <Field data-invalid={!!fields.note}>
        <FieldLabel htmlFor="inv-note">{approve ? "Note (optional)" : "Reason"}</FieldLabel>
        <Textarea id="inv-note" name="note" maxLength={1000} required={!approve} autoFocus />
        <FieldDescription>They&apos;ll see this in their notification.</FieldDescription>
        <FieldError>{fields.note && `Note ${fields.note}`}</FieldError>
      </Field>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            Cancel
          </Button>
        </DialogClose>
        <Button type="submit" disabled={pending} variant={approve ? "default" : "destructive"}>
          {approve ? "Approve" : "Reject"}
        </Button>
      </DialogFooter>
    </form>
  );
}
