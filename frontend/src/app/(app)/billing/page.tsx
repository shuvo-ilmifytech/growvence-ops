"use client";

import { useEffect, useMemo, useState } from "react";
import {
  EyeIcon,
  PencilIcon,
  PlusIcon,
  PrinterIcon,
  ReceiptIcon,
  SendIcon,
  TrashIcon,
  CheckCircle2Icon,
} from "lucide-react";
import {
  useCreateInvoice,
  useDeleteInvoice,
  useInvoice,
  useInvoices,
  useMarkInvoicePaid,
  useSendInvoice,
  useUpdateInvoice,
  type Invoice,
  type InvoiceInput,
  type InvoiceItem,
} from "@/lib/invoices";
import { useMe } from "@/lib/session";
import { useProjects } from "@/lib/projects";
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
import { cn } from "@/lib/utils";

const STATUS_TONE: Record<string, string> = {
  draft: "bg-muted text-muted-foreground",
  sent: "bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-300",
  paid: "bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300",
  overdue: "bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-300",
};

function money(n: number, currency: string) {
  try {
    return new Intl.NumberFormat("en", { style: "currency", currency }).format(n);
  } catch {
    return `${n.toFixed(2)} ${currency}`;
  }
}

function emptyItem(): InvoiceItem {
  return { description: "", quantity: 1, unit_price: 0 };
}

function InvoiceDialog({
  open,
  onOpenChange,
  initial,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  initial?: Invoice | null;
}) {
  const create = useCreateInvoice();
  const update = useUpdateInvoice();
  const { data: projects } = useProjects({});
  const [form, setForm] = useState<InvoiceInput>({
    client_name: "",
    client_email: "",
    items: [emptyItem()],
    tax_rate: 0,
    currency: "USD",
    notes: "",
  });

  useEffect(() => {
    if (open) {
      setForm({
        project_id: initial?.project_id ?? undefined,
        client_name: initial?.client_name ?? "",
        client_email: initial?.client_email ?? "",
        items: initial?.items?.length ? initial.items.map((i) => ({ ...i })) : [emptyItem()],
        tax_rate: initial?.tax_rate ?? 0,
        currency: initial?.currency ?? "USD",
        due_date: initial?.due_date ?? undefined,
        notes: initial?.notes ?? "",
      });
    }
  }, [open, initial]);

  const totals = useMemo(() => {
    const subtotal = form.items.reduce((s, i) => s + (Number(i.quantity) || 0) * (Number(i.unit_price) || 0), 0);
    const tax = (subtotal * (Number(form.tax_rate) || 0)) / 100;
    return { subtotal, tax, total: subtotal + tax };
  }, [form.items, form.tax_rate]);

  function setItem(idx: number, patch: Partial<InvoiceItem>) {
    setForm((f) => ({ ...f, items: f.items.map((it, i) => (i === idx ? { ...it, ...patch } : it)) }));
  }

  function submit() {
    const items = form.items.filter((i) => i.description.trim());
    if (!items.length || !form.client_name.trim()) return;
    const payload = { ...form, items };
    if (initial) {
      update.mutate(
        { id: initial.id, input: payload },
        { onSuccess: () => onOpenChange(false) },
      );
    } else {
      create.mutate(payload, { onSuccess: () => onOpenChange(false) });
    }
  }

  const pending = create.isPending || update.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{initial ? `Edit ${initial.invoice_number}` : "New invoice"}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label>Client name *</Label>
              <Input value={form.client_name} onChange={(e) => setForm({ ...form, client_name: e.target.value })} placeholder="Acme Inc" required />
            </div>
            <div className="space-y-2">
              <Label>Client email</Label>
              <Input value={form.client_email} onChange={(e) => setForm({ ...form, client_email: e.target.value })} placeholder="billing@acme.com" />
            </div>
          </div>
          <div className="grid gap-4 sm:grid-cols-3">
            <div className="space-y-2">
              <Label>Project (optional)</Label>
              <Select
                value={form.project_id ?? "none"}
                onValueChange={(v) => setForm({ ...form, project_id: v === "none" ? undefined : v })}
              >
                <SelectTrigger>
                  <SelectValue placeholder="No project" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">No project</SelectItem>
                  {(projects?.projects ?? []).map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>Currency</Label>
              <Select value={form.currency} onValueChange={(v) => setForm({ ...form, currency: v })}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {["USD", "BDT", "EUR", "GBP"].map((c) => (
                    <SelectItem key={c} value={c}>
                      {c}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>Due date</Label>
              <Input
                type="date"
                value={form.due_date ?? ""}
                onChange={(e) => setForm({ ...form, due_date: e.target.value || undefined })}
              />
            </div>
          </div>

          <div className="space-y-2">
            <Label>Line items</Label>
            {form.items.map((it, idx) => (
              <div key={idx} className="grid grid-cols-[1fr_4.5rem_6rem_2.5rem] items-center gap-2">
                <Input
                  value={it.description}
                  onChange={(e) => setItem(idx, { description: e.target.value })}
                  placeholder="Service description"
                />
                <Input
                  type="number"
                  min={0}
                  value={it.quantity}
                  onChange={(e) => setItem(idx, { quantity: Number(e.target.value) })}
                  placeholder="Qty"
                  aria-label="Quantity"
                />
                <Input
                  type="number"
                  min={0}
                  step="0.01"
                  value={it.unit_price}
                  onChange={(e) => setItem(idx, { unit_price: Number(e.target.value) })}
                  placeholder="Price"
                  aria-label="Unit price"
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  onClick={() => setForm((f) => ({ ...f, items: f.items.filter((_, i) => i !== idx) }))}
                  disabled={form.items.length === 1}
                  aria-label="Remove item"
                >
                  <TrashIcon className="size-4" />
                </Button>
              </div>
            ))}
            <Button type="button" variant="outline" size="sm" onClick={() => setForm((f) => ({ ...f, items: [...f.items, emptyItem()] }))}>
              <PlusIcon className="size-4" /> Add item
            </Button>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label>Tax rate (%)</Label>
              <Input
                type="number"
                min={0}
                step="0.01"
                value={form.tax_rate}
                onChange={(e) => setForm({ ...form, tax_rate: Number(e.target.value) })}
              />
            </div>
            <div className="space-y-2">
              <Label>Notes</Label>
              <Textarea value={form.notes} onChange={(e) => setForm({ ...form, notes: e.target.value })} rows={2} placeholder="Payment terms…" />
            </div>
          </div>

          <div className="rounded-lg bg-muted/50 p-3 text-sm">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Subtotal</span>
              <span className="tabular-nums">{money(totals.subtotal, form.currency)}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Tax ({form.tax_rate || 0}%)</span>
              <span className="tabular-nums">{money(totals.tax, form.currency)}</span>
            </div>
            <div className="mt-1 flex justify-between border-t pt-1 font-semibold">
              <span>Total</span>
              <span className="tabular-nums">{money(totals.total, form.currency)}</span>
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={pending}>
            {pending ? "Saving…" : initial ? "Save changes" : "Create invoice"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function InvoiceView({ id, onClose }: { id: string; onClose: () => void }) {
  const { data: inv, isPending } = useInvoice(id);
  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ReceiptIcon className="size-5" /> {inv?.invoice_number ?? "Invoice"}
          </DialogTitle>
        </DialogHeader>
        {isPending || !inv ? (
          <Skeleton className="h-64 w-full" />
        ) : (
          <div className="print-area space-y-4">
            <div className="flex items-start justify-between">
              <div>
                <p className="text-lg font-bold">Growvence Tech</p>
                <p className="text-sm text-muted-foreground">Invoice</p>
              </div>
              <Badge className={STATUS_TONE[inv.status]}>{inv.status}</Badge>
            </div>
            <div className="grid grid-cols-2 gap-4 text-sm">
              <div>
                <p className="text-muted-foreground">Bill to</p>
                <p className="font-medium">{inv.client_name || "—"}</p>
                <p className="text-muted-foreground">{inv.client_email || ""}</p>
              </div>
              <div className="text-right">
                <p>
                  <span className="text-muted-foreground">Number: </span>
                  <span className="font-medium">{inv.invoice_number}</span>
                </p>
                <p>
                  <span className="text-muted-foreground">Issued: </span>
                  {new Date(inv.created_at).toLocaleDateString()}
                </p>
                <p>
                  <span className="text-muted-foreground">Due: </span>
                  {inv.due_date ? new Date(inv.due_date).toLocaleDateString() : "—"}
                </p>
              </div>
            </div>
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b text-left text-muted-foreground">
                  <th className="py-2 font-medium">Description</th>
                  <th className="py-2 text-right font-medium">Qty</th>
                  <th className="py-2 text-right font-medium">Price</th>
                  <th className="py-2 text-right font-medium">Amount</th>
                </tr>
              </thead>
              <tbody>
                {inv.items.map((it, i) => (
                  <tr key={i} className="border-b last:border-0">
                    <td className="py-2">{it.description}</td>
                    <td className="py-2 text-right tabular-nums">{it.quantity}</td>
                    <td className="py-2 text-right tabular-nums">{money(it.unit_price, inv.currency)}</td>
                    <td className="py-2 text-right tabular-nums">{money(it.quantity * it.unit_price, inv.currency)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            <div className="ml-auto w-56 space-y-1 text-sm">
              <div className="flex justify-between">
                <span className="text-muted-foreground">Subtotal</span>
                <span className="tabular-nums">{money(inv.subtotal, inv.currency)}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-muted-foreground">Tax ({inv.tax_rate}%)</span>
                <span className="tabular-nums">{money(inv.tax_amount, inv.currency)}</span>
              </div>
              <div className="flex justify-between border-t pt-1 text-base font-bold">
                <span>Total</span>
                <span className="tabular-nums">{money(inv.total, inv.currency)}</span>
              </div>
            </div>
            {inv.notes && (
              <p className="text-sm text-muted-foreground">
                <span className="font-medium text-foreground">Notes: </span>
                {inv.notes}
              </p>
            )}
          </div>
        )}
        <DialogFooter className="no-print">
          <Button variant="outline" onClick={() => window.print()}>
            <PrinterIcon className="size-4" /> Print
          </Button>
          <Button variant="outline" onClick={onClose}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function InvoicesPage() {
  const { data: me } = useMe();
  const [status, setStatus] = useState("");
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<Invoice | null>(null);
  const [viewing, setViewing] = useState<string | null>(null);
  const list = useInvoices(status || undefined);
  const del = useDeleteInvoice();
  const send = useSendInvoice();
  const markPaid = useMarkInvoicePaid();

  const role = me?.role;
  const canManage = !!me && (me.is_admin || ["sales", "hr", "ceo"].includes(role ?? ""));

  const invoices = list.data?.invoices ?? [];

  return (
    <div className="space-y-6">
      <style>{`@media print { body * { visibility: hidden; } .print-area, .print-area * { visibility: visible; } .print-area { position: absolute; inset: 0; padding: 2rem; } .no-print { display: none !important; } }`}</style>
      <PageHeader
        title="Invoices"
        description="Create and track client invoices with line items, tax and payment status."
        actions={
          canManage && (
            <Button
              onClick={() => {
                setEditing(null);
                setDialogOpen(true);
              }}
            >
              <PlusIcon /> New invoice
            </Button>
          )
        }
      />

      <div className="flex gap-2">
        {["", "draft", "sent", "paid", "overdue"].map((s) => (
          <Button
            key={s}
            variant={status === s ? "default" : "outline"}
            size="sm"
            onClick={() => setStatus(s)}
            className="capitalize"
          >
            {s || "All"}
          </Button>
        ))}
      </div>

      {list.isPending ? (
        <div className="space-y-2">
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} className="h-16 w-full" />
          ))}
        </div>
      ) : invoices.length === 0 ? (
        <Card>
          <CardContent className="py-12 text-center text-muted-foreground">
            <ReceiptIcon className="mx-auto mb-3 size-8 opacity-40" />
            <p>No invoices yet. Create your first invoice to bill a client.</p>
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Invoices ({invoices.length})</CardTitle>
          </CardHeader>
          <CardContent className="p-0">
            <ul className="divide-y">
              {invoices.map((inv) => (
                <li key={inv.id} className="flex items-center gap-3 px-4 py-3">
                  <div className="min-w-0 flex-1">
                    <p className="font-medium">
                      {inv.invoice_number}
                      <span className="ml-2 text-sm font-normal text-muted-foreground">{inv.client_name || "No client"}</span>
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {money(inv.total, inv.currency)}
                      {inv.due_date ? ` · due ${new Date(inv.due_date).toLocaleDateString()}` : ""}
                      {inv.project_name ? ` · ${inv.project_name}` : ""}
                    </p>
                  </div>
                  <Badge className={cn(STATUS_TONE[inv.status], "capitalize")}>{inv.status}</Badge>
                  <div className="flex items-center gap-1">
                    <Button variant="ghost" size="icon" onClick={() => setViewing(inv.id)} aria-label="View invoice">
                      <EyeIcon className="size-4" />
                    </Button>
                    {canManage && inv.status === "draft" && (
                      <>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => {
                            setEditing(inv);
                            setDialogOpen(true);
                          }}
                          aria-label="Edit invoice"
                        >
                          <PencilIcon className="size-4" />
                        </Button>
                        <Button variant="ghost" size="icon" disabled={send.isPending} onClick={() => send.mutate(inv.id)} aria-label="Mark as sent">
                          <SendIcon className="size-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          disabled={del.isPending}
                          onClick={() => del.mutate(inv.id)}
                          aria-label="Delete invoice"
                        >
                          <TrashIcon className="size-4" />
                        </Button>
                      </>
                    )}
                    {canManage && (inv.status === "sent" || inv.status === "overdue") && (
                      <Button variant="ghost" size="icon" disabled={markPaid.isPending} onClick={() => markPaid.mutate(inv.id)} aria-label="Mark as paid">
                        <CheckCircle2Icon className="size-4" />
                      </Button>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

      <InvoiceDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        initial={editing}
      />
      {viewing && <InvoiceView id={viewing} onClose={() => setViewing(null)} />}
    </div>
  );
}
