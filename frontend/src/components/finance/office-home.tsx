"use client";

import Link from "next/link";
import { ArrowRightIcon, CalendarCheck2Icon, ClockIcon, PlusIcon, ReceiptTextIcon, type LucideIcon } from "lucide-react";
import { formatDate } from "@/lib/format";
import { formatBdt, invoiceRef, INVOICE_STATUS, useInvoices } from "@/lib/finance";
import { useHrMe } from "@/lib/hr";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

/** Home for the Office Admin: the invoices they raised, and their own attendance. */
export function OfficeHome({ name }: { name: string }) {
  const me = useHrMe();
  const list = useInvoices();
  const invoices = list.data?.invoices ?? [];
  const open = invoices.filter((i) => i.status === "pending" || i.status === "awaiting_ceo");
  const approved = invoices.filter((i) => i.status === "approved");

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight">Hello, {name.split(" ")[0]}</h1>
          <p className="text-sm text-muted-foreground">Raise an invoice for what the office needs; HR and the CEO approve it.</p>
        </div>
        <Button asChild>
          <Link href="/invoices">
            <PlusIcon /> Raise invoice
          </Link>
        </Button>
      </div>

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-3">
        <Stat href="/invoices" label="Waiting for approval" icon={ClockIcon} value={list.data ? open.length : undefined} tone="text-amber-600" />
        <Stat href="/invoices" label="Approved" icon={ReceiptTextIcon} value={list.data ? approved.length : undefined} />
        <Stat href="/hr" label="My HR" icon={CalendarCheck2Icon} value={me.data?.today_record ? "In" : me.data ? "—" : undefined} />
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <ReceiptTextIcon className="size-4 text-primary" /> Your invoices
          </CardTitle>
          <CardDescription>Newest first</CardDescription>
          <CardAction>
            <Link href="/invoices" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
              All invoices <ArrowRightIcon className="size-4" />
            </Link>
          </CardAction>
        </CardHeader>
        <CardContent>
          {!list.data ? (
            <Skeleton className="h-10 w-full" />
          ) : invoices.length === 0 ? (
            <p className="text-sm text-muted-foreground">Nothing raised yet.</p>
          ) : (
            <ul className="space-y-2 text-sm">
              {invoices.slice(0, 6).map((i) => (
                <li key={i.id} className="flex flex-wrap items-center justify-between gap-2">
                  <span className="min-w-0">
                    <span className="mr-1.5 font-mono text-xs text-muted-foreground">{invoiceRef(i.number)}</span>
                    {i.description}
                    <span className="text-muted-foreground"> · {formatDate(i.expense_date)}</span>
                  </span>
                  <span className="flex items-center gap-2">
                    <span className="font-medium tabular-nums">{formatBdt(i.amount_bdt)}</span>
                    <Badge variant="outline">{INVOICE_STATUS[i.status].label}</Badge>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function Stat({ href, label, icon: Icon, value, tone }: { href: string; label: string; icon: LucideIcon; value: number | string | undefined; tone?: string }) {
  return (
    <Link href={href} className="group">
      <Card className="h-full gap-2 transition group-hover:border-primary/40 group-hover:shadow-sm">
        <CardHeader>
          <CardDescription>{label}</CardDescription>
          <CardAction>
            <Icon className={`size-4 text-muted-foreground ${tone ?? ""}`} />
          </CardAction>
        </CardHeader>
        <CardContent>
          {value === undefined ? <Skeleton className="h-9 w-10" /> : <div className="text-3xl font-semibold tabular-nums">{value}</div>}
        </CardContent>
      </Card>
    </Link>
  );
}
