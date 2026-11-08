"use client";

import { useState } from "react";
import Link from "next/link";
import {
  CheckIcon,
  ClockIcon,
  FileTextIcon,
  LockIcon,
  PencilIcon,
  PlusIcon,
  PrinterIcon,
  ReceiptTextIcon,
  SearchIcon,
  Undo2Icon,
  XIcon,
} from "lucide-react";
import { api } from "@/lib/api";
import { formatDate, formatMoney, relativeTime } from "@/lib/format";
import { formatAmount, formatBdt, invoiceRef, INVOICE_STATUS, itemQuantity, useFinanceAction, useInvoices, type Invoice, type InvoiceStatus } from "@/lib/finance";
import { useMe } from "@/lib/session";
import { cn } from "@/lib/utils";
import { InvoiceDialog, InvoiceReviewDialog, type ReviewTarget } from "@/components/finance/invoice-dialog";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

const TONES: Record<InvoiceStatus, string> = {
  pending: "border-amber-500/50 text-amber-700 dark:text-amber-400",
  awaiting_ceo: "border-orange-500/50 text-orange-700 dark:text-orange-400",
  approved: "border-emerald-500/50 text-emerald-700 dark:text-emerald-400",
  rejected: "border-destructive/50 text-destructive",
  cancelled: "text-muted-foreground",
};

type Tab = "waiting" | "mine" | "all";

export default function InvoicesPage() {
  const { data: me } = useMe();
  const allowed = !!me && me.permissions.includes("invoice.raise");
  const [tab, setTab] = useState<Tab>("waiting");
  const [search, setSearch] = useState("");
  const [raising, setRaising] = useState(false);
  const [editing, setEditing] = useState<Invoice | null>(null);
  const [review, setReview] = useState<ReviewTarget | null>(null);

  const list = useInvoices(
    { status: tab === "waiting" ? "open" : "", mine: tab === "mine" ? "true" : "", q: search.trim() },
    allowed,
  );
  const data = list.data;
  const canReview = data?.can_review ?? false;
  const withdraw = useFinanceAction((id: string) => api(`/finance/invoices/${id}/cancel`, { method: "POST" }), "Invoice withdrawn");

  if (me && !allowed) {
    return (
      <Empty className="min-h-[60vh]">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <LockIcon />
          </EmptyMedia>
          <EmptyTitle>Not your part of the app</EmptyTitle>
          <EmptyDescription>Invoices are raised by the Office Admin and approved by HR and the CEO.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  const waiting = data?.waiting;
  const waitingOnMe = canReview ? (me?.role === "ceo" ? waiting?.awaiting_ceo : waiting?.pending_hr) : undefined;

  return (
    <div className="space-y-6">
      <PageHeader
        title="Invoices"
        description={
          canReview
            ? "Every expense is raised as an invoice: HR approves it, then the CEO, and the CEO's approval adds it to the ledger."
            : "Raise an invoice for what the office needs. HR approves it first, then the CEO."
        }
        actions={
          <Button onClick={() => setRaising(true)} disabled={!data?.today}>
            <PlusIcon /> Raise invoice
          </Button>
        }
      />

      {canReview && waiting && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Stat label="Waiting for HR" value={waiting.pending_hr} tone="text-amber-600" />
          <Stat label="Waiting for the CEO" value={waiting.awaiting_ceo} tone="text-orange-600" />
          <Stat label="Raised by you, still open" value={waiting.mine_open} />
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
          <TabsList>
            <TabsTrigger value="waiting">
              {canReview ? "Waiting" : "Still open"}
              {!!waitingOnMe && <span className="ml-1.5 text-xs text-muted-foreground tabular-nums">{waitingOnMe}</span>}
            </TabsTrigger>
            {canReview && <TabsTrigger value="mine">Raised by me</TabsTrigger>}
            <TabsTrigger value="all">All</TabsTrigger>
          </TabsList>
        </Tabs>
        <InputGroup className="w-full sm:w-64 sm:ml-auto">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput placeholder="Search what it was for" value={search} onChange={(e) => setSearch(e.target.value)} />
        </InputGroup>
      </div>

      {list.isPending && <Skeleton className="h-64 w-full" />}
      {list.isError && <p className="text-sm text-destructive">{list.error.message}</p>}

      {data && data.invoices.length === 0 && (
        <Card>
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <ReceiptTextIcon />
              </EmptyMedia>
              <EmptyTitle>{search ? "Nothing matches" : tab === "waiting" ? "Nothing is waiting" : "No invoices yet"}</EmptyTitle>
              <EmptyDescription>
                {canReview
                  ? "Office purchases come through here before they reach the ledger."
                  : "Raise one for anything the office needs — a fan, chairs, tea, repairs."}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        </Card>
      )}

      <div className="space-y-3">
        {(data?.invoices ?? []).map((inv) => {
          const mine = inv.requested_by === me?.id;
          const open = inv.status === "pending" || inv.status === "awaiting_ceo";
          // Only the person whose step it is may decide, and never on their own invoice.
          const myStep = canReview && !mine && (me?.role === "ceo" ? open : inv.status === "pending");
          const canEdit = open && (inv.status === "pending" ? mine || canReview : canReview);
          return (
            <Card key={inv.id} className="py-4">
              <CardContent className="flex flex-wrap items-start justify-between gap-4 px-4">
                <div className="min-w-0 space-y-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-mono text-xs text-muted-foreground">{invoiceRef(inv.number)}</span>
                    <span className="font-medium">{inv.description}</span>
                    <Badge variant="outline" className={cn(TONES[inv.status])}>
                      {INVOICE_STATUS[inv.status].label}
                    </Badge>
                  </div>
                  <div className="text-sm">
                    <span className="font-medium tabular-nums">{formatBdt(inv.amount_bdt)}</span>
                    {inv.currency !== "BDT" && (
                      <span className="text-muted-foreground tabular-nums">
                        {" "}
                        ({formatMoney(inv.amount, inv.currency)} × {Number(inv.rate_to_bdt)})
                      </span>
                    )}
                    <span className="text-muted-foreground">
                      {" · "}
                      {inv.items} {inv.items === 1 ? "line" : "lines"} · {formatDate(inv.expense_date)}
                    </span>
                  </div>
                  <Lines invoice={inv} />
                  <p className="text-xs text-muted-foreground">
                    Raised by {mine ? "you" : inv.requested_by_name} {relativeTime(inv.created_at)}
                  </p>
                  <Trail invoice={inv} />
                </div>
                <div className="flex flex-wrap gap-2">
                  {myStep && (
                    <>
                      <Button variant="outline" size="sm" onClick={() => setReview({ invoice: inv, approve: false, final: true })}>
                        <XIcon /> Reject
                      </Button>
                      <Button size="sm" onClick={() => setReview({ invoice: inv, approve: true, final: me?.role === "ceo" })}>
                        <CheckIcon /> Approve
                      </Button>
                    </>
                  )}
                  {canEdit && (
                    <Button variant="ghost" size="sm" onClick={() => setEditing(inv)}>
                      <PencilIcon /> Edit
                    </Button>
                  )}
                  <Button variant="ghost" size="sm" asChild>
                    <Link href={`/invoices/${inv.id}`}>
                      <PrinterIcon /> Open
                    </Link>
                  </Button>
                  {open && mine && (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => confirm(`Withdraw ${invoiceRef(inv.number)}? It won't be approved.`) && withdraw.mutate(inv.id)}
                    >
                      <Undo2Icon /> Withdraw
                    </Button>
                  )}
                </div>
              </CardContent>
            </Card>
          );
        })}
      </div>

      {raising && data && <InvoiceDialog categories={data.categories} today={data.today} onClose={() => setRaising(false)} />}
      {editing && data && (
        <InvoiceDialog invoice={editing} categories={data.categories} today={data.today} onClose={() => setEditing(null)} />
      )}
      <InvoiceReviewDialog target={review} onOpenChange={(o) => !o && setReview(null)} />
    </div>
  );
}

function Stat({ label, value, tone }: { label: string; value: number; tone?: string }) {
  return (
    <Card className="gap-1 py-4">
      <CardContent className="px-4">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className={cn("text-2xl font-semibold tabular-nums", value > 0 && tone)}>{value}</div>
      </CardContent>
    </Card>
  );
}

/** What is on the invoice, grouped as typed. */
function Lines({ invoice }: { invoice: Invoice }) {
  if (!invoice.items_list.length) return null;
  return (
    <ul className="space-y-0.5 text-sm">
      {invoice.items_list.map((it) => (
        <li key={it.id} className="flex flex-wrap items-baseline gap-x-2">
          <span>{it.description}</span>
          <span className="text-xs text-muted-foreground tabular-nums">
            {itemQuantity(it.quantity)} × {formatAmount(it.unit_price, invoice.currency)}
          </span>
          <span className="text-xs text-muted-foreground">{it.category_name}</span>
          <span className="ml-auto tabular-nums">{formatAmount(it.amount, invoice.currency)}</span>
        </li>
      ))}
    </ul>
  );
}

/** Who has decided so far, and what they said. */
function Trail({ invoice: inv }: { invoice: Invoice }) {
  const lines: { who: string; what: string; note: string }[] = [];
  if (inv.hr_reviewed_by_name) {
    const rejected = inv.status === "rejected" && !inv.ceo_reviewed_by_name;
    lines.push({
      who: inv.hr_reviewed_by_name,
      what: inv.requested_by === inv.hr_reviewed_by && !rejected ? "raised it as HR" : rejected ? "rejected it" : "approved it",
      note: inv.hr_note,
    });
  }
  if (inv.ceo_reviewed_by_name) {
    lines.push({
      who: inv.ceo_reviewed_by_name,
      what: inv.status === "rejected" ? "rejected it" : "gave the final approval",
      note: inv.ceo_note,
    });
  }
  if (inv.status === "approved" && inv.ledger_lines === 0) {
    lines.push({ who: "", what: "Its ledger lines were since removed", note: "" });
  }
  if (!lines.length) {
    return (
      <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <ClockIcon className="size-3.5" /> Waiting for HR to approve it
      </p>
    );
  }
  return (
    <ul className="space-y-0.5 text-xs text-muted-foreground">
      {lines.map((l, i) => (
        <li key={i} className="flex items-start gap-1.5">
          <FileTextIcon className="mt-0.5 size-3.5 shrink-0" />
          <span>
            {l.who && <span className="font-medium text-foreground">{l.who}</span>} {l.what}
            {l.note && ` — “${l.note}”`}
          </span>
        </li>
      ))}
    </ul>
  );
}
