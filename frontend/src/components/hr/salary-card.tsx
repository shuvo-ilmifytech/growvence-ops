"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { LockIcon, PlusIcon, Trash2Icon, WalletIcon } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import { formatDate, formatMoney } from "@/lib/format";
import { HR_KEY, useHrAction, useSalaryHistory } from "@/lib/hr";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

/** Salary history on a profile. Only rendered for HR and the CEO, never on someone's own profile. */
export function SalaryCard({ userId, name, today }: { userId: string; name: string; today: string }) {
  const history = useSalaryHistory(userId, true);
  const [adding, setAdding] = useState(false);
  const remove = useHrAction((id: string) => api(`/hr/salaries/${id}`, { method: "DELETE" }), "Salary entry removed");
  const rows = history.data ?? [];
  const current = rows.find((r) => r.effective_from <= today);

  return (
    <Card className="pb-0">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <WalletIcon className="size-4 text-primary" /> Salary
        </CardTitle>
        <CardDescription className="flex items-center gap-1.5">
          <LockIcon className="size-3" /> Only HR and the CEO see this. {name} can&apos;t see their own salary.
        </CardDescription>
        <CardAction>
          <Button size="sm" onClick={() => setAdding(true)}>
            <PlusIcon /> Add change
          </Button>
        </CardAction>
      </CardHeader>
      {history.isPending ? (
        <Skeleton className="mx-6 mb-6 h-16" />
      ) : rows.length === 0 ? (
        <p className="border-t px-6 py-4 text-sm text-muted-foreground">No salary recorded yet.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-6">From</TableHead>
              <TableHead className="text-right">Monthly gross</TableHead>
              <TableHead className="hidden md:table-cell">Note</TableHead>
              <TableHead className="hidden lg:table-cell">Added by</TableHead>
              <TableHead className="w-12 pr-6" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r) => (
              <TableRow key={r.id}>
                <TableCell className="pl-6">
                  <span className="flex items-center gap-2">
                    {formatDate(r.effective_from)}
                    {r.id === current?.id && <Badge variant="outline">Current</Badge>}
                    {r.effective_from > today && <Badge variant="outline">Upcoming</Badge>}
                  </span>
                </TableCell>
                <TableCell className="text-right font-medium tabular-nums">{formatMoney(r.amount, r.currency)}</TableCell>
                <TableCell className="hidden max-w-64 truncate text-sm text-muted-foreground md:table-cell">{r.note || "—"}</TableCell>
                <TableCell className="hidden text-sm text-muted-foreground lg:table-cell">{r.created_by_name}</TableCell>
                <TableCell className="pr-6 text-right">
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Remove entry from ${r.effective_from}`}
                    disabled={remove.isPending}
                    onClick={() => confirm("Remove this salary entry? Use this only for mistakes.") && remove.mutate(r.id)}
                  >
                    <Trash2Icon />
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <Dialog open={adding} onOpenChange={setAdding}>
        <DialogContent className="sm:max-w-md">
          {adding && <SalaryForm userId={userId} name={name} today={today} currency={current?.currency ?? "BDT"} onDone={() => setAdding(false)} />}
        </DialogContent>
      </Dialog>
    </Card>
  );
}

function SalaryForm({ userId, name, today, currency, onDone }: { userId: string; name: string; today: string; currency: string; onDone: () => void }) {
  const qc = useQueryClient();
  const [fields, setFields] = useState<Record<string, string>>({});
  const [pending, setPending] = useState(false);

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const d = new FormData(e.currentTarget);
    setFields({});
    setPending(true);
    try {
      await api(`/hr/employees/${userId}/salary`, {
        method: "POST",
        body: { amount: d.get("amount"), currency: d.get("currency"), effective_from: d.get("effective_from"), note: d.get("note") },
      });
      toast.success("Salary saved");
      qc.invalidateQueries({ queryKey: HR_KEY });
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
        <DialogTitle>Salary change for {name}</DialogTitle>
        <DialogDescription>Add an entry for a new salary, a raise or a correction. Earlier entries are kept as history.</DialogDescription>
      </DialogHeader>
      <FieldGroup>
        <div className="grid grid-cols-[1fr_6rem] gap-3">
          <Field data-invalid={!!fields.amount}>
            <FieldLabel htmlFor="amount">Monthly gross</FieldLabel>
            <Input id="amount" name="amount" inputMode="decimal" placeholder="45000" required autoFocus />
            <FieldError>{fields.amount && `Amount ${fields.amount}`}</FieldError>
          </Field>
          <Field data-invalid={!!fields.currency}>
            <FieldLabel htmlFor="currency">Currency</FieldLabel>
            <Input id="currency" name="currency" defaultValue={currency} maxLength={3} className="uppercase" required />
            <FieldError>{fields.currency && fields.currency}</FieldError>
          </Field>
        </div>
        <Field data-invalid={!!fields.effective_from}>
          <FieldLabel htmlFor="effective_from">Effective from</FieldLabel>
          <Input id="effective_from" name="effective_from" type="date" defaultValue={today} required />
          <FieldDescription>A future date is fine; it becomes current on that day.</FieldDescription>
          <FieldError>{fields.effective_from && `Date ${fields.effective_from}`}</FieldError>
        </Field>
        <Field data-invalid={!!fields.note}>
          <FieldLabel htmlFor="salary-note">Note</FieldLabel>
          <Input id="salary-note" name="note" maxLength={500} placeholder="e.g. Annual raise" />
          <FieldError>{fields.note && `Note ${fields.note}`}</FieldError>
        </Field>
      </FieldGroup>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">Cancel</Button>
        </DialogClose>
        <Button type="submit" disabled={pending}>Save</Button>
      </DialogFooter>
    </form>
  );
}
