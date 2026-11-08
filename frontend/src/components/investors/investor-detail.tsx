"use client";

import { use, useState } from "react";
import { HandCoinsIcon, PencilIcon, PercentIcon, PlusIcon } from "lucide-react";
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
  useInvestor,
  useMarkWithdrawalPaid,
  useRejectWithdrawal,
  type Withdrawal,
} from "@/lib/investors";
import {
  DistributeDialog,
  InvestmentDialog,
  InvestorDialog,
  ShareDialog,
  WithdrawalDialog,
} from "./investor-dialogs";

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

export function InvestorDetail({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const { data: me } = useMe();
  const detail = useInvestor(id);
  const isCeo = me?.role === "ceo";
  const [dialog, setDialog] = useState<"edit" | "invest" | "share" | "withdraw" | "distribute" | null>(null);

  if (detail.isPending) return <Skeleton className="h-64" />;
  if (detail.isError || !detail.data) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>Investor could not be loaded</EmptyTitle>
          <EmptyDescription>{detail.error?.message ?? "Try again."}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  const { investor, investments, distributions, withdrawals, shares } = detail.data;
  const balance = investorBalance(investor);

  return (
    <div className="space-y-6">
      <PageHeader
        title={investor.name}
        description={`${parseAmount(investor.share_percent)}% share · joined ${formatDate(investor.join_date)}${investor.email ? ` · ${investor.email}` : ""}${investor.phone ? ` · ${investor.phone}` : ""}`}
        actions={
          <>
            <Button variant="outline" onClick={() => setDialog("edit")}>
              <PencilIcon className="size-4" /> Edit
            </Button>
            <Button variant="outline" onClick={() => setDialog("invest")}>
              <PlusIcon className="size-4" /> Record investment
            </Button>
            {isCeo && (
              <Button variant="outline" onClick={() => setDialog("share")}>
                <PercentIcon className="size-4" /> Set share
              </Button>
            )}
            <Button variant="outline" onClick={() => setDialog("distribute")}>
              <HandCoinsIcon className="size-4" /> Distribute income
            </Button>
            <Button onClick={() => setDialog("withdraw")}>Request withdrawal</Button>
          </>
        }
      />

      <div className="flex items-center gap-2">
        <Badge className={investor.status === "active" ? "bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300" : "bg-muted text-muted-foreground"}>
          {investor.status}
        </Badge>
        {investor.notes && <p className="text-sm text-muted-foreground">{investor.notes}</p>}
      </div>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
        <Stat label="Invested" value={money(parseAmount(investor.invested_bdt), "BDT")} />
        <Stat label="Earned" value={money(parseAmount(investor.earned_bdt), "BDT")} />
        <Stat label="Withdrawn" value={money(parseAmount(investor.withdrawn_bdt), "BDT")} />
        <Stat label="Balance" value={money(balance, "BDT")} />
        <Stat label="Available to withdraw" value={money(investorAvailable(investor), "BDT")} />
        <Stat label="Share" value={`${parseAmount(investor.share_percent)}%`} />
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Investments ({investments.length})</CardTitle>
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

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Distributions ({distributions.length})</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {distributions.length === 0 ? (
            <Empty className="py-8">
              <EmptyHeader>
                <EmptyTitle>No distributions yet</EmptyTitle>
                <EmptyDescription>Milestone payments distribute automatically once recorded.</EmptyDescription>
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
          <CardTitle className="text-base">Withdrawals ({withdrawals.length})</CardTitle>
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
                <WithdrawalRow key={w.id} w={w} isCeo={isCeo} />
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Share history ({shares.length})</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {shares.length === 0 ? (
            <Empty className="py-8">
              <EmptyHeader>
                <EmptyTitle>No share set yet</EmptyTitle>
              </EmptyHeader>
            </Empty>
          ) : (
            <ul className="divide-y">
              {shares.map((s) => (
                <li key={s.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                  <p className="font-medium">{parseAmount(s.share_percent)}%</p>
                  <p className="text-xs text-muted-foreground">
                    from {formatDate(s.effective_from)}
                    {s.created_by_name ? ` · set by ${s.created_by_name}` : ""}
                  </p>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      {dialog === "edit" && <InvestorDialog investor={investor} onClose={() => setDialog(null)} />}
      {dialog === "invest" && <InvestmentDialog investor={investor} onClose={() => setDialog(null)} />}
      {dialog === "share" && <ShareDialog investor={investor} onClose={() => setDialog(null)} />}
      {dialog === "withdraw" && <WithdrawalDialog investor={investor} onClose={() => setDialog(null)} />}
      {dialog === "distribute" && <DistributeDialog investor={investor} onClose={() => setDialog(null)} />}
    </div>
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
          {money(parseAmount(w.amount), w.currency)}
          {w.currency !== "BDT" ? ` (≈ ${money(parseAmount(w.amount_bdt), "BDT")})` : ""}
          <span className="ml-2 text-sm font-normal text-muted-foreground">via {w.method}</span>
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
