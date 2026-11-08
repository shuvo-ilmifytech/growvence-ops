"use client";

import { useState } from "react";
import { PlusIcon } from "lucide-react";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { money, parseAmount } from "@/lib/payments";
import { formatDate } from "@/lib/format";
import { cn } from "@/lib/utils";
import { investorAvailable, investorBalance, useInvestorMe, type Withdrawal } from "@/lib/investors";
import { WithdrawalDialog } from "./investor-dialogs";

const WITHDRAWAL_TONE: Record<Withdrawal["status"], string> = {
  pending: "bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300",
  approved: "bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-300",
  paid: "bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300",
  rejected: "bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-300",
};

const SOURCE_LABEL: Record<string, string> = {
  milestone: "Milestone payment",
  income: "Income",
  manual: "Manual income",
};

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <Card>
      <CardContent className="space-y-1 py-4">
        <p className="text-xs text-muted-foreground">{label}</p>
        <p className="text-lg font-semibold">{value}</p>
      </CardContent>
    </Card>
  );
}

/** The investor's own dashboard: their share, earnings, withdrawals — and
 * the company reserve total, read-only. Nothing about other investors. */
export function InvestorHome() {
  const me = useInvestorMe();
  const [withdrawOpen, setWithdrawOpen] = useState(false);

  if (me.isPending) return <Skeleton className="h-64" />;
  if (me.isError || !me.data) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>No investor profile linked</EmptyTitle>
          <EmptyDescription>
            {me.error?.message ?? "Ask the CEO or HR to link your login to an investor record."}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  const { investor, investments, distributions, by_month, withdrawals, reserve_bdt } = me.data;
  const balance = investorBalance(investor);

  return (
    <div className="space-y-6">
      <PageHeader
        title={`My Share — ${investor.name}`}
        description={`${parseAmount(investor.share_percent)}% of every distribution · joined ${formatDate(investor.join_date)}`}
        actions={
          <Button onClick={() => setWithdrawOpen(true)}>
            <PlusIcon className="size-4" /> Request withdrawal
          </Button>
        }
      />

      <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
        <Stat label="My share" value={`${parseAmount(investor.share_percent)}%`} />
        <Stat label="Total earned" value={money(parseAmount(investor.earned_bdt), "BDT")} />
        <Stat label="Balance" value={money(balance, "BDT")} />
        <Stat label="Available to withdraw" value={money(investorAvailable(investor), "BDT")} />
        <Stat label="Total withdrawn" value={money(parseAmount(investor.withdrawn_bdt), "BDT")} />
        <Stat label="Company reserve fund" value={money(parseAmount(reserve_bdt), "BDT")} />
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Earnings by month</CardTitle>
        </CardHeader>
        <CardContent>
          {by_month.length === 0 ? (
            <p className="text-sm text-muted-foreground">No distributions yet.</p>
          ) : (
            <ul className="space-y-1 text-sm">
              {by_month.map((m) => (
                <li key={m.month} className="flex justify-between">
                  <span>{m.month}</span>
                  <span className="font-medium">{money(parseAmount(m.earned_bdt), "BDT")}</span>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">My distributions ({distributions.length})</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {distributions.length === 0 ? (
            <Empty className="py-8">
              <EmptyHeader>
                <EmptyTitle>No distributions yet</EmptyTitle>
              </EmptyHeader>
            </Empty>
          ) : (
            <ul className="divide-y">
              {distributions.map((d) => (
                <li key={d.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                  <div className="min-w-0 flex-1">
                    <p className="font-medium">
                      {money(parseAmount(d.amount), d.currency)}
                      {d.currency !== "BDT" ? ` (≈ ${money(parseAmount(d.amount_bdt), "BDT")})` : ""}
                      <span className="ml-2 text-sm font-normal text-muted-foreground">
                        {parseAmount(d.share_percent)}% of {SOURCE_LABEL[d.source_type] ?? d.source_type}
                      </span>
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {formatDate(d.distributed_at)}
                      {d.note ? ` · ${d.note}` : ""}
                    </p>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">My withdrawals ({withdrawals.length})</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {withdrawals.length === 0 ? (
            <Empty className="py-8">
              <EmptyHeader>
                <EmptyTitle>No withdrawals yet</EmptyTitle>
              </EmptyHeader>
            </Empty>
          ) : (
            <ul className="divide-y">
              {withdrawals.map((w) => (
                <li key={w.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                  <div className="min-w-0 flex-1">
                    <p className="font-medium">
                      {money(parseAmount(w.amount), w.currency)}
                      {w.currency !== "BDT" ? ` (≈ ${money(parseAmount(w.amount_bdt), "BDT")})` : ""}
                      <span className="ml-2 text-sm font-normal text-muted-foreground">via {w.method}</span>
                    </p>
                    <p className="text-xs text-muted-foreground">
                      Requested {formatDate(w.requested_at)}
                      {w.paid_at ? ` · paid ${formatDate(w.paid_at)}` : ""}
                      {w.note ? ` · ${w.note}` : ""}
                    </p>
                  </div>
                  <Badge className={cn(WITHDRAWAL_TONE[w.status], "capitalize")}>{w.status}</Badge>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">My investments ({investments.length})</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {investments.length === 0 ? (
            <Empty className="py-8">
              <EmptyHeader>
                <EmptyTitle>No investments recorded</EmptyTitle>
              </EmptyHeader>
            </Empty>
          ) : (
            <ul className="divide-y">
              {investments.map((inv) => (
                <li key={inv.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                  <div className="min-w-0 flex-1">
                    <p className="font-medium">
                      {money(parseAmount(inv.amount), inv.currency)}
                      {inv.currency !== "BDT" ? ` (≈ ${money(parseAmount(inv.amount_bdt), "BDT")} @ ${inv.rate_to_bdt})` : ""}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {formatDate(inv.invested_at)}
                      {inv.method ? ` · ${inv.method}` : ""}
                      {inv.note ? ` · ${inv.note}` : ""}
                    </p>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      {withdrawOpen && <WithdrawalDialog investor={investor} onClose={() => setWithdrawOpen(false)} />}
    </div>
  );
}
