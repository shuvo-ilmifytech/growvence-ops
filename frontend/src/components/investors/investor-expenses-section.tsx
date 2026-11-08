"use client";

import { useState } from "react";
import { Trash2Icon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { money, parseAmount } from "@/lib/payments";
import { formatDate } from "@/lib/format";
import { useMe } from "@/lib/session";
import { useCurrencyRates, useDeleteExpense, useInvestorExpenses } from "@/lib/investors";

/** Full expense list (CEO can delete) + the currency-rate history. */
export function InvestorExpensesSection() {
  const { data: me } = useMe();
  const expenses = useInvestorExpenses();
  const rates = useCurrencyRates();
  const del = useDeleteExpense();
  const isCeo = me?.role === "ceo";
  const [confirmId, setConfirmId] = useState<string | null>(null);

  return (
    <div className="grid gap-6 md:grid-cols-2">
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Expenses ({expenses.data?.expenses.length ?? 0})</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {expenses.isPending ? (
            <Skeleton className="m-4 h-24" />
          ) : !expenses.data || expenses.data.expenses.length === 0 ? (
            <Empty className="py-8">
              <EmptyHeader>
                <EmptyTitle>No expenses yet</EmptyTitle>
              </EmptyHeader>
            </Empty>
          ) : (
            <ul className="divide-y">
              {expenses.data.expenses.map((e) => (
                <li key={e.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                  <div className="min-w-0 flex-1">
                    <p className="font-medium">
                      {e.description}
                      <span className="ml-2 text-sm font-normal text-muted-foreground">{e.category}</span>
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {formatDate(e.expense_date)}
                      {e.added_by_name ? ` · ${e.added_by_name}` : ""}
                      {e.receipt_url ? (
                        <>
                          {" · "}
                          <a href={e.receipt_url} target="_blank" rel="noreferrer" className="underline">
                            Receipt
                          </a>
                        </>
                      ) : (
                        ""
                      )}
                    </p>
                  </div>
                  <p className="text-sm font-semibold">
                    {money(parseAmount(e.amount), e.currency)}
                    {e.currency !== "BDT" ? ` (≈ ${money(parseAmount(e.amount_bdt), "BDT")})` : ""}
                  </p>
                  {isCeo &&
                    (confirmId === e.id ? (
                      <Button
                        size="sm"
                        variant="destructive"
                        disabled={del.isPending}
                        onClick={() => {
                          del.mutate(e.id);
                          setConfirmId(null);
                        }}
                      >
                        Confirm delete
                      </Button>
                    ) : (
                      <Button size="sm" variant="ghost" onClick={() => setConfirmId(e.id)} aria-label="Delete expense">
                        <Trash2Icon className="size-4" />
                      </Button>
                    ))}
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Currency rates</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {rates.isPending ? (
            <Skeleton className="m-4 h-24" />
          ) : !rates.data || rates.data.rates.length === 0 ? (
            <Empty className="py-8">
              <EmptyHeader>
                <EmptyTitle>No rates set</EmptyTitle>
              </EmptyHeader>
            </Empty>
          ) : (
            <ul className="divide-y">
              {rates.data.rates.map((r) => (
                <li key={r.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                  <p className="font-medium">
                    1 {r.currency} = {money(parseAmount(r.rate), "BDT")}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    from {formatDate(r.effective_from)} · set by {r.created_by_name}
                  </p>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
