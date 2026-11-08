"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { BanknoteIcon, LockIcon, PencilIcon, PlusIcon, SearchIcon, Trash2Icon } from "lucide-react";
import { api } from "@/lib/api";
import { formatDate, formatMoney } from "@/lib/format";
import { adjustmentTotal, formatBdt, formatMonth, useFinanceAction, usePayroll, type PayrollRow } from "@/lib/finance";
import { useHrMe } from "@/lib/hr";
import { useMe } from "@/lib/session";
import { PaymentDialog } from "@/components/finance/payment-dialog";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export default function PayrollPage() {
  const { data: me } = useMe();
  const allowed = me?.role === "hr" || me?.role === "ceo";
  const hrMe = useHrMe(allowed);
  const today = hrMe.data?.today ?? "";
  const [picked, setPicked] = useState("");
  const month = picked || today.slice(0, 7);
  const [search, setSearch] = useState("");
  const [editing, setEditing] = useState<PayrollRow | null>(null);
  const sheet = usePayroll(month, allowed);
  const remove = useFinanceAction((id: string) => api(`/finance/payroll/${id}`, { method: "DELETE" }), "Pay record deleted");

  const rows = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (sheet.data?.rows ?? []).filter((r) => !q || [r.name, r.department_name, r.designation].some((v) => v.toLowerCase().includes(q)));
  }, [sheet.data, search]);

  const totals = useMemo(() => {
    let paid = 0;
    let recorded = 0;
    for (const r of sheet.data?.rows ?? []) {
      if (!r.payment) continue;
      recorded++;
      paid += Number(r.payment.net_bdt);
    }
    return { paid, recorded, people: sheet.data?.rows.length ?? 0 };
  }, [sheet.data]);

  if (me && !allowed) {
    return (
      <Empty className="min-h-[60vh]">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <LockIcon />
          </EmptyMedia>
          <EmptyTitle>HR and the CEO only</EmptyTitle>
          <EmptyDescription>Payroll is private.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Payroll"
        description="Record what each person is paid for the month: their fixed salary plus bonuses, less deductions such as unpaid absence. Only recorded pay counts as an expense in Finance. Your own pay is never shown to you."
        actions={
          <Button variant="outline" asChild>
            <Link href="/finance">Finance</Link>
          </Button>
        }
      />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-[1fr_auto] sm:items-end">
        <div className="flex flex-wrap items-center gap-2">
          <Input type="month" aria-label="Month" value={month} onChange={(e) => setPicked(e.target.value)} className="w-44" disabled={!today} />
          <InputGroup className="w-full sm:w-64">
            <InputGroupAddon>
              <SearchIcon />
            </InputGroupAddon>
            <InputGroupInput placeholder="Search name, department" value={search} onChange={(e) => setSearch(e.target.value)} />
          </InputGroup>
        </div>
        {sheet.data && (
          <Card className="gap-0 py-3">
            <CardContent className="px-4">
              <div className="text-xs text-muted-foreground">
                Paid for {formatMonth(month)}: {totals.recorded} of {totals.people} people
              </div>
              <div className="text-lg font-semibold tabular-nums">{formatBdt(totals.paid)}</div>
            </CardContent>
          </Card>
        )}
      </div>

      <Card className="py-0">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-4">Person</TableHead>
              <TableHead className="hidden text-right md:table-cell">Fixed salary</TableHead>
              <TableHead className="hidden lg:table-cell">Attendance</TableHead>
              <TableHead className="hidden text-right sm:table-cell">Bonuses / deductions</TableHead>
              <TableHead className="text-right">Net pay</TableHead>
              <TableHead className="w-28 pr-4" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {(sheet.isPending || !today) &&
              Array.from({ length: 4 }, (_, i) => (
                <TableRow key={i}>
                  <TableCell colSpan={6} className="pl-4">
                    <Skeleton className="h-8 w-full" />
                  </TableCell>
                </TableRow>
              ))}
            {rows.map((r) => {
              const p = r.payment;
              const att = r.attendance;
              return (
                <TableRow key={r.user_id}>
                  <TableCell className="pl-4">
                    <Link href={`/hr/people/${r.user_id}`} className="font-medium hover:underline">
                      {r.name}
                    </Link>
                    {!r.is_active && (
                      <Badge variant="outline" className="ml-2">
                        Left
                      </Badge>
                    )}
                    <div className="text-xs text-muted-foreground">{[r.designation, r.department_name].filter(Boolean).join(" · ") || "—"}</div>
                  </TableCell>
                  <TableCell className="hidden text-right tabular-nums md:table-cell">
                    {r.salary_amount ? formatMoney(r.salary_amount, r.salary_currency ?? "BDT") : "—"}
                  </TableCell>
                  <TableCell className="hidden text-sm text-muted-foreground lg:table-cell">
                    {att
                      ? [
                          `${att.working_days} working days`,
                          att.absent > 0 && `${att.absent} absent`,
                          att.unpaid_leave_days > 0 && `${att.unpaid_leave_days} unpaid leave`,
                          att.late > 0 && `${att.late} late`,
                        ]
                          .filter(Boolean)
                          .join(" · ")
                      : "—"}
                  </TableCell>
                  <TableCell className="hidden text-right text-sm tabular-nums sm:table-cell">
                    {p && p.adjustments.length > 0 ? (
                      <span title={p.adjustments.map((a) => `${a.kind === "bonus" ? "+" : "−"}${a.amount} ${a.reason}`).join("\n")}>
                        +{formatMoney(String(adjustmentTotal(p.adjustments, "bonus")), p.currency)}
                        <span className="text-muted-foreground"> / </span>
                        <span className="text-destructive">−{formatMoney(String(adjustmentTotal(p.adjustments, "deduction")), p.currency)}</span>
                      </span>
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </TableCell>
                  <TableCell className="text-right whitespace-nowrap">
                    {p ? (
                      <>
                        <div className="font-medium tabular-nums">{formatMoney(p.net_amount, p.currency)}</div>
                        <div className="text-xs text-muted-foreground" title={`Last saved by ${p.updated_by_name}`}>
                          Paid {formatDate(p.paid_on)}
                          {p.currency !== "BDT" && ` · ${formatBdt(p.net_bdt)}`}
                        </div>
                      </>
                    ) : (
                      <Badge variant="secondary">Not recorded</Badge>
                    )}
                  </TableCell>
                  <TableCell className="pr-4">
                    <div className="flex justify-end">
                      {p ? (
                        <>
                          <Button size="icon" variant="ghost" aria-label={`Edit ${r.name}'s pay`} onClick={() => setEditing(r)}>
                            <PencilIcon />
                          </Button>
                          <Button
                            size="icon"
                            variant="ghost"
                            aria-label={`Delete ${r.name}'s pay`}
                            onClick={() => confirm(`Delete ${r.name}'s pay for ${formatMonth(month)}? This can't be undone.`) && remove.mutate(p.id)}
                          >
                            <Trash2Icon />
                          </Button>
                        </>
                      ) : (
                        <Button size="sm" variant="outline" onClick={() => setEditing(r)}>
                          <PlusIcon /> Record
                        </Button>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
        {sheet.isSuccess && rows.length === 0 && (
          <Empty className="border-t">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <BanknoteIcon />
              </EmptyMedia>
              <EmptyTitle>{search ? "No one found" : `No one to pay for ${formatMonth(month)}`}</EmptyTitle>
              <EmptyDescription>{search ? "Try a different search." : "People show here once they have a salary. Add one from People."}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
      </Card>
      {editing && <PaymentDialog row={editing} month={month} today={today} onClose={() => setEditing(null)} />}
    </div>
  );
}
