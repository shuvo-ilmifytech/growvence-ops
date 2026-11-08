"use client";

import Link from "next/link";
import { Building2Icon, CalendarClockIcon, FileTextIcon, RepeatIcon } from "lucide-react";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { useB2BReport } from "@/lib/b2b";
import { money } from "@/lib/payments";
import { cn } from "@/lib/utils";

function fmtDate(d: string | null) {
  if (!d) return "—";
  return new Date(d.length === 10 ? d + "T00:00:00" : d).toLocaleDateString();
}

function healthTone(score: number) {
  if (score >= 80) return "bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300";
  if (score >= 50) return "bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300";
  return "bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-300";
}

/**
 * B2B dashboard: revenue by client, outstanding payments, profitability and
 * client health — all in BDT (foreign-currency milestones are converted at
 * the rate on their payment date).
 */
export default function B2BPage() {
  const report = useB2BReport();
  const data = report.data;
  const revenue = data?.revenue_by_client ?? [];
  const outstanding = data?.outstanding ?? [];
  const profit = data?.profitability ?? [];
  const health = data?.health ?? [];
  const dueSoon = data?.retainers_due ?? [];
  const expiring = data?.contracts_expiring ?? [];

  const totalRevenue = revenue.reduce((s, r) => s + r.revenue_bdt, 0);
  const totalOutstanding = outstanding.reduce((s, r) => s + r.outstanding_bdt, 0);

  return (
    <div className="space-y-6">
      <PageHeader
        title="B2B"
        description="Revenue, outstanding payments, profitability and client health across all business clients."
      />

      {report.isPending && (
        <div className="grid gap-4 md:grid-cols-2">
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-32" />
          ))}
        </div>
      )}

      {report.isError && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>Couldn’t load the B2B report</EmptyTitle>
            <EmptyDescription>Please try refreshing the page.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {data && (
        <>
          <div className="grid gap-4 md:grid-cols-2">
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">
                  Total received (top 10 clients)
                </CardTitle>
              </CardHeader>
              <CardContent className="text-2xl font-bold text-green-700 dark:text-green-400">
                {money(totalRevenue, "BDT")}
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">
                  Outstanding across clients
                </CardTitle>
              </CardHeader>
              <CardContent className="text-2xl font-bold text-red-600 dark:text-red-400">
                {totalOutstanding > 0 ? money(totalOutstanding, "BDT") : "—"}
              </CardContent>
            </Card>
          </div>

          <div className="grid gap-4 lg:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle className="text-base">Revenue by client (top 10)</CardTitle>
              </CardHeader>
              <CardContent className="p-0">
                {revenue.length === 0 ? (
                  <p className="px-4 py-6 text-sm text-muted-foreground">
                    No payments received yet — revenue appears here once a milestone is paid.
                  </p>
                ) : (
                  <ul className="divide-y">
                    {revenue.map((r) => (
                      <li key={r.client_id} className="flex items-center justify-between gap-3 px-4 py-2.5">
                        <span className="min-w-0">
                          <span className="block truncate font-medium">{r.company_name}</span>
                          <span className="text-xs text-muted-foreground">
                            {r.client_code} · {r.paid_count} payment{r.paid_count === 1 ? "" : "s"}
                          </span>
                        </span>
                        <span className="font-medium">{money(r.revenue_bdt, "BDT")}</span>
                      </li>
                    ))}
                  </ul>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="text-base">Outstanding payments</CardTitle>
              </CardHeader>
              <CardContent className="p-0">
                {outstanding.length === 0 ? (
                  <p className="px-4 py-6 text-sm text-muted-foreground">
                    Nothing outstanding — every scheduled milestone is paid.
                  </p>
                ) : (
                  <ul className="divide-y">
                    {outstanding.map((r) => (
                      <li key={r.client_id} className="flex items-center justify-between gap-3 px-4 py-2.5">
                        <span className="min-w-0">
                          <span className="block truncate font-medium">{r.company_name}</span>
                          <span className="text-xs text-muted-foreground">
                            {r.client_code} · {r.open_count} open milestone{r.open_count === 1 ? "" : "s"}
                          </span>
                        </span>
                        <span className="font-medium text-red-600 dark:text-red-400">
                          {money(r.outstanding_bdt, "BDT")}
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Profitability by client</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {profit.length === 0 ? (
                <p className="px-4 py-6 text-sm text-muted-foreground">
                  No agreed prices yet — set a project value to see agreed vs received here.
                </p>
              ) : (
                <ul className="divide-y">
                  {profit.map((r) => (
                    <li key={r.client_id} className="flex flex-wrap items-center gap-3 px-4 py-2.5">
                      <span className="min-w-0 flex-1">
                        <span className="block truncate font-medium">{r.company_name}</span>
                        <span className="text-xs text-muted-foreground">{r.client_code}</span>
                      </span>
                      <span className="text-sm">
                        Agreed <strong>{money(r.agreed_bdt, "BDT")}</strong>
                      </span>
                      <span className="text-sm">
                        Received <strong className="text-green-700 dark:text-green-400">{money(r.received_bdt, "BDT")}</strong>
                      </span>
                      <span className="text-sm">
                        Outstanding{" "}
                        <strong className={r.outstanding_bdt > 0 ? "text-red-600 dark:text-red-400" : ""}>
                          {money(r.outstanding_bdt, "BDT")}
                        </strong>
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Client health</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {health.length === 0 ? (
                <p className="px-4 py-6 text-sm text-muted-foreground">No clients yet.</p>
              ) : (
                <ul className="divide-y">
                  {health.map((h) => (
                    <li key={h.client_id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                      <span className="min-w-0 flex-1">
                        <span className="flex items-center gap-2 font-medium">
                          <Building2Icon className="size-4 text-muted-foreground" />
                          {h.company_name}
                        </span>
                        <span className="text-xs text-muted-foreground">
                          On-time {h.on_time}% · Response {h.response_rate}% · Active {h.active_score}% ·{" "}
                          {h.active_projects} active project{h.active_projects === 1 ? "" : "s"}
                          {h.overdue_count > 0 ? ` · ${h.overdue_count} overdue` : ""}
                        </span>
                      </span>
                      <Badge className={cn(healthTone(h.score))}>{h.score}/100</Badge>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>

          <div className="grid gap-4 lg:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-base">
                  <RepeatIcon className="size-4" /> Retainers due soon
                </CardTitle>
              </CardHeader>
              <CardContent className="p-0">
                {dueSoon.length === 0 ? (
                  <p className="px-4 py-6 text-sm text-muted-foreground">
                    No retainer bills in the next 7 days.
                  </p>
                ) : (
                  <ul className="divide-y">
                    {dueSoon.map((r) => (
                      <li key={r.id} className="flex items-center justify-between gap-3 px-4 py-2.5">
                        <span className="min-w-0">
                          <span className="block truncate font-medium">
                            {r.client_name} — {r.title}
                          </span>
                          <span className="text-xs text-muted-foreground">
                            Billing day {r.billing_day}
                          </span>
                        </span>
                        <span className="text-sm font-medium">{fmtDate(r.next_billing_date)}</span>
                      </li>
                    ))}
                  </ul>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-base">
                  <FileTextIcon className="size-4" /> Contracts expiring soon
                </CardTitle>
              </CardHeader>
              <CardContent className="p-0">
                {expiring.length === 0 ? (
                  <p className="px-4 py-6 text-sm text-muted-foreground">
                    No contracts ending in the next 30 days.
                  </p>
                ) : (
                  <ul className="divide-y">
                    {expiring.map((c) => (
                      <li key={c.id} className="flex items-center justify-between gap-3 px-4 py-2.5">
                        <span className="min-w-0">
                          <span className="block truncate font-medium">
                            {c.contract_number} · {c.title}
                          </span>
                          <span className="text-xs text-muted-foreground">{c.client_name}</span>
                        </span>
                        <span className="flex items-center gap-1 text-sm font-medium">
                          <CalendarClockIcon className="size-3.5 text-muted-foreground" />
                          {fmtDate(c.end_date)}
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </CardContent>
            </Card>
          </div>

          <p className="text-sm text-muted-foreground">
            Health = on-time payments (50%) + client responses (25%) + active projects (25%).
            Revenue counts milestone payments received; the finance ledger has no project link,
            so it is not the source here.{" "}
            <Link href="/contracts" className="underline underline-offset-2">
              Contracts
            </Link>
            {" · "}
            <Link href="/retainers" className="underline underline-offset-2">
              Retainers
            </Link>
          </p>
        </>
      )}
    </div>
  );
}
