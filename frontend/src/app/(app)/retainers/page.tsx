"use client";

import { useEffect, useState } from "react";
import { CalendarClockIcon, PlusIcon, RepeatIcon } from "lucide-react";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
  retainerFee,
  useB2BReport,
  useCreateRetainer,
  useRetainers,
  useUpdateRetainer,
  type Retainer,
  type RetainerInput,
  type RetainerStatus,
} from "@/lib/b2b";
import { useClients } from "@/lib/clients";
import { money } from "@/lib/payments";
import { useMe } from "@/lib/session";
import { cn } from "@/lib/utils";

const STATUS_TONE: Record<RetainerStatus, string> = {
  active: "bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300",
  paused: "bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300",
  cancelled: "bg-muted text-muted-foreground",
};

const STATUS_LABEL: Record<RetainerStatus, string> = {
  active: "Active",
  paused: "Paused",
  cancelled: "Cancelled",
};

const NEXT_STATUS: Partial<Record<RetainerStatus, RetainerStatus[]>> = {
  active: ["paused", "cancelled"],
  paused: ["active", "cancelled"],
};

function fmtDate(d: string | null) {
  if (!d) return "—";
  return new Date(d.length === 10 ? d + "T00:00:00" : d).toLocaleDateString();
}

function emptyForm(): RetainerInput {
  return {
    client_id: "",
    title: "",
    monthly_fee: 0,
    currency: "BDT",
    start_date: "",
    billing_day: 1,
    auto_renew: true,
    status: "active",
  };
}

function RetainerDialog({
  open,
  onOpenChange,
  initial,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  initial?: Retainer | null;
}) {
  const create = useCreateRetainer();
  const update = useUpdateRetainer();
  const clients = useClients();
  const [form, setForm] = useState<RetainerInput>(emptyForm());

  useEffect(() => {
    if (open) {
      setForm(
        initial
          ? {
              client_id: initial.client_id,
              title: initial.title,
              monthly_fee: retainerFee(initial),
              currency: initial.currency,
              start_date: initial.start_date,
              billing_day: initial.billing_day,
              auto_renew: initial.auto_renew,
              status: initial.status,
            }
          : { ...emptyForm(), start_date: new Date().toISOString().slice(0, 10) },
      );
    }
  }, [open, initial]);

  const pending = create.isPending || update.isPending;
  const valid =
    form.client_id !== "" &&
    form.title.trim() !== "" &&
    form.monthly_fee > 0 &&
    form.start_date !== "" &&
    form.billing_day >= 1 &&
    form.billing_day <= 28;
  const save = () => {
    if (!valid) return;
    if (initial) {
      update.mutate({ id: initial.id, input: form }, { onSuccess: () => onOpenChange(false) });
    } else {
      create.mutate(form, { onSuccess: () => onOpenChange(false) });
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{initial ? "Edit retainer" : "New retainer"}</DialogTitle>
          <DialogDescription>
            A monthly subscription for a client. On the billing day each month an invoice
            is generated automatically.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="rt-client">Client</Label>
            <Select
              value={form.client_id}
              onValueChange={(v) => setForm({ ...form, client_id: v })}
              disabled={!!initial}
            >
              <SelectTrigger id="rt-client">
                <SelectValue placeholder="Choose a client" />
              </SelectTrigger>
              <SelectContent>
                {(clients.data?.clients ?? []).map((c) => (
                  <SelectItem key={c.id} value={c.id}>
                    {c.company_name} ({c.client_code})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="rt-title">Title</Label>
            <Input
              id="rt-title"
              value={form.title}
              onChange={(e) => setForm({ ...form, title: e.target.value })}
              placeholder="e.g. Monthly maintenance"
            />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="rt-fee">Monthly fee</Label>
              <Input
                id="rt-fee"
                type="number"
                min="0"
                step="0.01"
                value={form.monthly_fee || ""}
                onChange={(e) => setForm({ ...form, monthly_fee: Number(e.target.value) })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="rt-currency">Currency</Label>
              <Select value={form.currency} onValueChange={(v) => setForm({ ...form, currency: v })}>
                <SelectTrigger id="rt-currency">
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
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="rt-start">Start date</Label>
              <Input
                id="rt-start"
                type="date"
                value={form.start_date}
                onChange={(e) => setForm({ ...form, start_date: e.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="rt-day">Billing day (1–28)</Label>
              <Input
                id="rt-day"
                type="number"
                min="1"
                max="28"
                step="1"
                value={form.billing_day || ""}
                onChange={(e) => setForm({ ...form, billing_day: Number(e.target.value) })}
              />
            </div>
          </div>
          <div className="flex items-center justify-between gap-4">
            <div>
              <Label htmlFor="rt-renew">Auto-renew</Label>
              <p className="text-xs text-muted-foreground">
                {form.auto_renew
                  ? "Billing continues every month."
                  : "Bills once more, then pauses automatically."}
              </p>
            </div>
            <Switch
              id="rt-renew"
              checked={form.auto_renew}
              onCheckedChange={(v) => setForm({ ...form, auto_renew: v })}
            />
          </div>
          {initial && (
            <div className="space-y-2">
              <Label htmlFor="rt-status">Status</Label>
              <Select
                value={form.status}
                onValueChange={(v) => setForm({ ...form, status: v as RetainerStatus })}
              >
                <SelectTrigger id="rt-status">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {Array.from(new Set([initial.status, ...(NEXT_STATUS[initial.status] ?? [])])).map(
                    (s) => (
                      <SelectItem key={s} value={s}>
                        {STATUS_LABEL[s]}
                      </SelectItem>
                    ),
                  )}
                </SelectContent>
              </Select>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <Button onClick={save} disabled={pending || !valid}>
            {initial ? "Save changes" : "Create retainer"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function RetainersPage() {
  const { data: me } = useMe();
  const [status, setStatus] = useState("");
  const list = useRetainers(status);
  const report = useB2BReport();
  const update = useUpdateRetainer();
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<Retainer | null>(null);
  const retainers = list.data?.retainers ?? [];
  const dueSoon = report.data?.retainers_due ?? [];
  const canManage = !!me && (me.is_admin || me.role === "sales");

  const setStatusFor = (r: Retainer, next: RetainerStatus) => {
    update.mutate({
      id: r.id,
      input: {
        client_id: r.client_id,
        title: r.title,
        monthly_fee: retainerFee(r),
        currency: r.currency,
        start_date: r.start_date,
        billing_day: r.billing_day,
        auto_renew: r.auto_renew,
        status: next,
      },
    });
  };

  return (
    <div className="space-y-6">
      <PageHeader
        title="Retainers"
        description="Monthly subscriptions — billed automatically on each retainer's billing day."
        actions={
          canManage ? (
            <Button onClick={() => setAdding(true)}>
              <PlusIcon /> New retainer
            </Button>
          ) : undefined
        }
      />

      {dueSoon.length > 0 && (
        <Card className="border-amber-300 bg-amber-50 dark:border-amber-800 dark:bg-amber-950/30">
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-2 text-base">
              <CalendarClockIcon className="size-4" /> Billing due soon ({dueSoon.length})
            </CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="space-y-1 text-sm">
              {dueSoon.map((r) => (
                <li key={r.id}>
                  {r.client_name} — {r.title}: {money(retainerFee(r), r.currency)} on{" "}
                  {fmtDate(r.next_billing_date)}
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

      <div className="flex gap-2">
        <Select value={status || "all"} onValueChange={(v) => setStatus(v === "all" ? "" : v)}>
          <SelectTrigger className="w-44">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All statuses</SelectItem>
            {(Object.keys(STATUS_LABEL) as RetainerStatus[]).map((s) => (
              <SelectItem key={s} value={s}>
                {STATUS_LABEL[s]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {list.isPending && (
        <div className="space-y-2">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-16" />
          ))}
        </div>
      )}

      {list.isError && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>Couldn’t load retainers</EmptyTitle>
            <EmptyDescription>Please try refreshing the page.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {!list.isPending && !list.isError && retainers.length === 0 && (
        <Empty className="min-h-[40vh]">
          <EmptyHeader>
            <EmptyMedia>
              <RepeatIcon className="size-8 opacity-40" />
            </EmptyMedia>
            <EmptyTitle>No retainers yet</EmptyTitle>
            <EmptyDescription>
              Set up a monthly retainer for a client — the first billing date is worked out
              from the start date and billing day.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {retainers.length > 0 && (
        <Card>
          <CardContent className="p-0">
            <ul className="divide-y">
              {retainers.map((r) => (
                <li key={r.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                  <div className="min-w-0 flex-1">
                    <p className="font-medium">
                      {r.title} · {r.client_name}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {money(retainerFee(r), r.currency)}/month · billing day {r.billing_day}
                      {r.status === "active" && ` · next billing ${fmtDate(r.next_billing_date)}`}
                      {r.auto_renew ? " · auto-renew" : " · no auto-renew"}
                    </p>
                  </div>
                  <Badge className={cn(STATUS_TONE[r.status], "capitalize")}>{STATUS_LABEL[r.status]}</Badge>
                  {canManage && (
                    <div className="flex items-center gap-1">
                      {r.status === "active" && (
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={update.isPending}
                          onClick={() => setStatusFor(r, "paused")}
                        >
                          Pause
                        </Button>
                      )}
                      {r.status === "paused" && (
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={update.isPending}
                          onClick={() => setStatusFor(r, "active")}
                        >
                          Resume
                        </Button>
                      )}
                      {r.status !== "cancelled" && (
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={update.isPending}
                          onClick={() => setStatusFor(r, "cancelled")}
                        >
                          Cancel
                        </Button>
                      )}
                      {r.status !== "cancelled" && (
                        <Button variant="ghost" size="sm" onClick={() => setEditing(r)}>
                          Edit
                        </Button>
                      )}
                    </div>
                  )}
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

      <RetainerDialog open={adding} onOpenChange={setAdding} />
      <RetainerDialog
        open={!!editing}
        onOpenChange={(v) => !v && setEditing(null)}
        initial={editing}
      />
    </div>
  );
}
