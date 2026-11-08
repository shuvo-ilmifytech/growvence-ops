"use client";

import { useState } from "react";
import Link from "next/link";
import { HandCoinsIcon, LandmarkIcon, PlusIcon, ScaleIcon, WalletIcon } from "lucide-react";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { money, parseAmount } from "@/lib/payments";
import { formatDate } from "@/lib/format";
import { useMe } from "@/lib/session";
import { cn } from "@/lib/utils";
import {
  investorAvailable,
  investorBalance,
  useApproveWithdrawal,
  useCompanyAccount,
  useInvestors,
  useMarkWithdrawalPaid,
  useRejectWithdrawal,
  useWithdrawals,
  type Withdrawal,
} from "@/lib/investors";
import {
  DistributeDialog,
  ExpenseDialog,
  InvestorDialog,
  RateDialog,
  ReserveDialog,
} from "./investor-dialogs";

const WITHDRAWAL_TONE: Record<Withdrawal["status"], string> = {
  pending: "bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300",
  approved: "bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-300",
  paid: "bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300",
  rejected: "bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-300",
};

function Stat({ label, value, tone }: { label: string; value: string; tone?: string }) {
  return (
    <Card>
      <CardContent className="space-y-1 py-4">
        <p className="text-xs text-muted-foreground">{label}</p>
        <p className={cn("text-lg font-semibold", tone)}>{value}</p>
      </CardContent>
    </Card>
  );
}

function WithdrawalRow({ w, isCeo }: { w: Withdrawal; isCeo: boolean }) {
  const approve = useApproveWithdrawal(w.investor_id);
  const reject = useRejectWithdrawal(w.investor_id);
  const markPaid = useMarkWithdrawalPaid(w.investor_id);
  const busy = approve.isPending || reject.isPending || markPaid.isPending;
  return (
    <li className="flex flex-wrap items-center gap-3 px-4 py-3">
      <div className="min-w-0 flex-1">
        <p className="font-medium">
          {w.investor_name}
          <span className="ml-2 text-sm font-normal text-muted-foreground">
            {money(parseAmount(w.amount), w.currency)}
            {w.currency !== "BDT" ? ` (≈ ${money(parseAmount(w.amount_bdt), "BDT")})` : ""} via {w.method}
          </span>
        </p>
        <p className="text-xs text-muted-foreground">
          Requested {formatDate(w.requested_at)}
          {w.note ? ` · ${w.note}` : ""}
          {w.paid_at ? ` · paid ${formatDate(w.paid_at)}` : ""}
        </p>
      </div>
      <Badge className={cn(WITHDRAWAL_TONE[w.status], "capitalize")}>{w.status}</Badge>
      {isCeo && w.status === "pending" && (
        <>
          <Button size="sm" disabled={busy} onClick={() => approve.mutate(w.id)}>
            Approve
          </Button>
          <Button size="sm" variant="outline" disabled={busy} onClick={() => reject.mutate(w.id)}>
            Reject
          </Button>
        </>
      )}
      {w.status === "approved" && (
        <Button size="sm" disabled={busy} onClick={() => markPaid.mutate(w.id)}>
          Mark paid
        </Button>
      )}
    </li>
  );
}

export function InvestorsDashboard() {
  const { data: me } = useMe();
  const investors = useInvestors();
  const account = useCompanyAccount();
  const pending = useWithdrawals("pending");
  const isCeo = me?.role === "ceo";
  const [dialog, setDialog] = useState<"add" | "distribute" | "reserve" | "expense" | "rate" | null>(null);

  if (investors.isPending || account.isPending) {
    return <Skeleton className="h-64" />;
  }
  if (investors.isError || account.isError || !investors.data || !account.data) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>Investors could not be loaded</EmptyTitle>
          <EmptyDescription>{investors.error?.message ?? account.error?.message ?? "Try again."}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  const { totals, expense_totals } = account.data;
  const reservePct = investors.data.reserve_percent;
  const totalShares = investors.data.total_shares;
  const unallocated = Math.max(0, 100 - totalShares - reservePct);
  const list = investors.data.investors;

  return (
    <div className="space-y-6">
      <PageHeader
        title="Investors"
        description="Capital partners, their shares, profit distributions and withdrawals."
        actions={
          <>
            <Button variant="outline" onClick={() => setDialog("rate")}>
              <LandmarkIcon className="size-4" /> Set rate
            </Button>
            <Button variant="outline" onClick={() => setDialog("expense")}>
              <WalletIcon className="size-4" /> Add expense
            </Button>
            {isCeo && (
              <Button variant="outline" onClick={() => setDialog("reserve")}>
                <ScaleIcon className="size-4" /> Reserve {reservePct}%
              </Button>
            )}
            <Button variant="outline" disabled={list.length === 0} onClick={() => setDialog("distribute")}>
              <HandCoinsIcon className="size-4" /> Distribute income
            </Button>
            <Button onClick={() => setDialog("add")}>
              <PlusIcon className="size-4" /> Add investor
            </Button>
          </>
        }
      />

      <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
        <Stat label="Total income (ledger)" value={money(parseAmount(totals.total_income_bdt), "BDT")} />
        <Stat label="Investor-tracked expenses" value={money(parseAmount(totals.total_expenses_bdt), "BDT")} />
        <Stat label="Net profit" value={money(account.data.net_profit_bdt, "BDT")} tone={account.data.net_profit_bdt < 0 ? "text-red-600" : undefined} />
        <Stat label="Reserve fund" value={money(parseAmount(totals.reserve_bdt), "BDT")} />
        <Stat label="Distributed to investors" value={money(parseAmount(totals.total_distributed_bdt), "BDT")} />
        <Stat label="Company balance" value={money(account.data.balance_bdt, "BDT")} tone={account.data.balance_bdt < 0 ? "text-red-600" : "text-green-700 dark:text-green-400"} />
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Profit split</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          Investor shares {totalShares}% + company reserve {reservePct}%
          {unallocated > 0 ? ` — ${unallocated}% unallocated stays with the company on each distribution.` : " = 100% of every distribution."}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Investors ({list.length})</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {list.length === 0 ? (
            <Empty className="py-8">
              <EmptyHeader>
                <EmptyTitle>No investors yet</EmptyTitle>
                <EmptyDescription>Add the first investor, record their capital, then set their share.</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <ul className="divide-y">
              {list.map((inv) => (
                <li key={inv.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                  <div className="min-w-0 flex-1">
                    <p className="font-medium">
                      <Link href={`/investors/${inv.id}`} className="hover:underline">
                        {inv.name}
                      </Link>
                      <span className="ml-2 text-sm font-normal text-muted-foreground">
                        {parseAmount(inv.share_percent)}% share
                      </span>
                    </p>
                    <p className="text-xs text-muted-foreground">
                      Invested {money(parseAmount(inv.invested_bdt), "BDT")} · Earned {money(parseAmount(inv.earned_bdt), "BDT")} · Withdrawn{" "}
                      {money(parseAmount(inv.withdrawn_bdt), "BDT")}
                      {parseAmount(inv.pending_withdrawals_bdt) > 0 &&
                        ` · ${money(parseAmount(inv.pending_withdrawals_bdt), "BDT")} requested`}
                    </p>
                  </div>
                  <div className="text-right">
                    <p className="text-sm font-semibold">{money(investorBalance(inv), "BDT")}</p>
                    <p className="text-xs text-muted-foreground">balance · {money(investorAvailable(inv), "BDT")} available</p>
                  </div>
                  <Badge className={inv.status === "active" ? "bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300" : "bg-muted text-muted-foreground"}>
                    {inv.status}
                  </Badge>
                  <Link href={`/investors/${inv.id}`}>
                    <Button variant="ghost" size="sm">
                      Open
                    </Button>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Pending withdrawals ({pending.data?.withdrawals.length ?? 0})</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {!pending.data || pending.data.withdrawals.length === 0 ? (
            <Empty className="py-8">
              <EmptyHeader>
                <EmptyTitle>No pending withdrawals</EmptyTitle>
              </EmptyHeader>
            </Empty>
          ) : (
            <ul className="divide-y">
              {pending.data.withdrawals.map((w) => (
                <WithdrawalRow key={w.id} w={w} isCeo={isCeo} />
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Expenses by category</CardTitle>
        </CardHeader>
        <CardContent>
          {expense_totals.length === 0 ? (
            <p className="text-sm text-muted-foreground">No investor-tracked expenses yet.</p>
          ) : (
            <ul className="space-y-1 text-sm">
              {expense_totals.map((t) => (
                <li key={t.category} className="flex justify-between">
                  <span>
                    {t.category} <span className="text-muted-foreground">({t.count})</span>
                  </span>
                  <span className="font-medium">{money(parseAmount(t.total_bdt), "BDT")}</span>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      {dialog === "add" && <InvestorDialog onClose={() => setDialog(null)} />}
      {dialog === "distribute" && list.length > 0 && <DistributeDialog investor={list[0]!} onClose={() => setDialog(null)} />}
      {dialog === "reserve" && <ReserveDialog current={reservePct} onClose={() => setDialog(null)} />}
      {dialog === "expense" && <ExpenseDialog onClose={() => setDialog(null)} />}
      {dialog === "rate" && <RateDialog onClose={() => setDialog(null)} />}
    </div>
  );
}
