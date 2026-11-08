"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import {
  AlertTriangleIcon,
  BanknoteIcon,
  DownloadIcon,
  LandmarkIcon,
  LockIcon,
  PencilIcon,
  PlusIcon,
  ReceiptTextIcon,
  SearchIcon,
  Trash2Icon,
} from "lucide-react";
import { api } from "@/lib/api";
import { formatDate, formatMoney } from "@/lib/format";
import {
  addMonths,
  entryQuery,
  formatBdt,
  formatMonth,
  monthDays,
  useFinanceAction,
  useFinanceEntries,
  useFinanceSummary,
  invoiceRef,
  type FinanceEntry,
  type FinanceMonth,
} from "@/lib/finance";
import { useHrMe } from "@/lib/hr";
import { useMe } from "@/lib/session";
import { cn } from "@/lib/utils";
import { CategoryBars, IncomeExpenseChart, ProfitChart } from "@/components/finance/charts";
import { EntryDialog } from "@/components/finance/entry-dialog";
import { FinanceSettings } from "@/components/finance/finance-settings";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableFooter, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

const RANGES = {
  "6m": "Last 6 months",
  "12m": "Last 12 months",
  ytd: "This year",
  last: "Last year",
} as const;
type Range = keyof typeof RANGES;

function rangeMonths(range: Range, current: string): [string, string] {
  const year = Number(current.slice(0, 4));
  switch (range) {
    case "6m":
      return [addMonths(current, -5), current];
    case "12m":
      return [addMonths(current, -11), current];
    case "ytd":
      return [`${year}-01`, current];
    case "last":
      return [`${year - 1}-01`, `${year - 1}-12`];
  }
}

export default function FinancePage() {
  const { data: me } = useMe();
  const allowed = me?.role === "hr" || me?.role === "ceo";
  const hrMe = useHrMe(allowed);
  const today = hrMe.data?.today ?? "";
  const [adding, setAdding] = useState(false);

  if (me && !allowed) {
    return (
      <Empty className="min-h-[60vh]">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <LockIcon />
          </EmptyMedia>
          <EmptyTitle>HR and the CEO only</EmptyTitle>
          <EmptyDescription>Company finances are private.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Finance"
        description="Income and expenses in BDT. Expenses come from approved invoices; project earnings (after the marketplace fee) are counted automatically; payroll is what you record under Payroll."
        actions={
          <>
            <Button variant="outline" asChild>
              <Link href="/invoices">
                <ReceiptTextIcon /> Invoices
              </Link>
            </Button>
            <Button variant="outline" asChild>
              <Link href="/payroll">
                <BanknoteIcon /> Payroll
              </Link>
            </Button>
            <Button onClick={() => setAdding(true)} disabled={!today}>
              <PlusIcon /> Add income
            </Button>
          </>
        }
      />
      {adding && today && <EntryDialog today={today} onClose={() => setAdding(false)} />}
      {!today ? (
        <Skeleton className="h-96 w-full" />
      ) : (
        <Tabs defaultValue="overview" className="gap-6">
          <TabsList>
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="ledger">Ledger</TabsTrigger>
            <TabsTrigger value="settings">Categories & rates</TabsTrigger>
          </TabsList>
          <TabsContent value="overview">
            <Overview current={today.slice(0, 7)} />
          </TabsContent>
          <TabsContent value="ledger">
            <Ledger current={today.slice(0, 7)} today={today} isCEO={me?.role === "ceo"} />
          </TabsContent>
          <TabsContent value="settings">
            <FinanceSettings today={today} />
          </TabsContent>
        </Tabs>
      )}
    </div>
  );
}

// ── Overview ─────────────────────────────────────────────────

function sum(months: FinanceMonth[], k: keyof FinanceMonth) {
  return months.reduce((s, m) => s + Number(m[k]), 0);
}

function Stat({ label, value, hint, tone }: { label: string; value: number; hint?: string; tone?: "loss" }) {
  return (
    <Card className="gap-1 py-4">
      <CardContent className="px-4">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className={cn("text-2xl font-semibold tabular-nums", tone === "loss" && value < 0 && "text-destructive")}>{formatBdt(value)}</div>
        {hint && <div className="mt-0.5 text-xs text-muted-foreground">{hint}</div>}
      </CardContent>
    </Card>
  );
}

function Overview({ current }: { current: string }) {
  const [range, setRange] = useState<Range>("12m");
  const [from, to] = rangeMonths(range, current);
  const summary = useFinanceSummary(from, to);
  const s = summary.data;
  const months = s?.months ?? [];
  const thisMonth = months.find((m) => m.month === current);
  const missing = [...new Set(months.flatMap((m) => m.earnings_missing_rates))];
  const profit = sum(months, "profit");

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Select value={range} onValueChange={(v) => setRange(v as Range)}>
          <SelectTrigger className="w-44" aria-label="Period">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {Object.entries(RANGES).map(([k, label]) => (
              <SelectItem key={k} value={k}>
                {label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <span className="text-sm text-muted-foreground">
          {formatMonth(from)} – {formatMonth(to)}
        </span>
      </div>

      {missing.length > 0 && (
        <div className="flex gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-sm">
          <AlertTriangleIcon className="mt-0.5 size-4 shrink-0 text-amber-600" />
          <span>
            No BDT rate set for {missing.join(", ")}, so some project earnings aren&apos;t counted yet. Add a rate under{" "}
            <strong>Categories &amp; rates</strong>.
          </span>
        </div>
      )}

      {!s ? (
        <Skeleton className="h-80 w-full" />
      ) : (
        <>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <Stat label={`Profit this month (${formatMonth(current, true)})`} value={Number(thisMonth?.profit ?? 0)} hint="So far; payroll once it's recorded" tone="loss" />
            <Stat label="Income in period" value={sum(months, "total_income")} hint={`${formatBdt(sum(months, "project_earnings"))} from ${sum(months, "projects")} projects`} />
            <Stat label="Expenses in period" value={sum(months, "total_expense")} hint={`${formatBdt(sum(months, "payroll"))} payroll`} />
            <Stat label={profit < 0 ? "Loss in period" : "Profit in period"} value={profit} tone="loss" />
          </div>

          <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Income and expenses</CardTitle>
                <CardDescription>Per month, in BDT</CardDescription>
              </CardHeader>
              <CardContent>
                <IncomeExpenseChart summary={s} />
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>Profit</CardTitle>
                <CardDescription>Income less expenses, per month</CardDescription>
              </CardHeader>
              <CardContent>
                <ProfitChart summary={s} />
              </CardContent>
            </Card>
          </div>

          <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Expenses by category</CardTitle>
                <CardDescription>Entered in the ledger this period (payroll not included)</CardDescription>
              </CardHeader>
              <CardContent>
                <CategoryBars rows={s.categories} kind="expense" />
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>Other income by category</CardTitle>
                <CardDescription>Entered in the ledger this period (project earnings not included)</CardDescription>
              </CardHeader>
              <CardContent>
                <CategoryBars rows={s.categories} kind="income" />
              </CardContent>
            </Card>
          </div>

          <MonthTable months={months} current={current} />
        </>
      )}
    </div>
  );
}

function MonthTable({ months, current }: { months: FinanceMonth[]; current: string }) {
  const rows = [...months].reverse();
  const cols: [keyof FinanceMonth, string, string][] = [
    ["project_earnings", "Project earnings", "hidden md:table-cell"],
    ["ledger_income", "Other income", "hidden md:table-cell"],
    ["payroll", "Payroll", "hidden lg:table-cell"],
    ["ledger_expense", "Other expenses", "hidden lg:table-cell"],
    ["total_income", "Income", "hidden sm:table-cell"],
    ["total_expense", "Expenses", "hidden sm:table-cell"],
    ["profit", "Profit", ""],
  ];
  return (
    <Card className="py-0">
      <CardHeader className="pt-6">
        <CardTitle>Month by month</CardTitle>
        <CardDescription>All amounts in BDT. Payroll is the pay recorded for each month on the Payroll page, after bonuses and deductions.</CardDescription>
      </CardHeader>
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="pl-6">Month</TableHead>
            {cols.map(([k, label, cls]) => (
              <TableHead key={k} className={cn("text-right", cls, k === "profit" && "pr-6")}>
                {label}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((m) => {
            const missing = m.earnings_missing_rates;
            return (
              <TableRow key={m.month}>
                <TableCell className="pl-6">
                  {formatMonth(m.month)}
                  {m.month === current && (
                    <Badge variant="secondary" className="ml-2">
                      So far
                    </Badge>
                  )}
                  {missing.length > 0 && (
                    <Badge variant="outline" className="ml-2 border-amber-500/50 text-amber-700 dark:text-amber-400" title={`No rate for ${missing.join(", ")}`}>
                      No {[...new Set(missing)].join(", ")} rate
                    </Badge>
                  )}
                </TableCell>
                {cols.map(([k, , cls]) => (
                  <TableCell
                    key={k}
                    className={cn("text-right tabular-nums", cls, k === "profit" && "pr-6 font-medium", k === "profit" && Number(m.profit) < 0 && "text-destructive")}
                  >
                    {formatBdt(m[k] as string)}
                  </TableCell>
                ))}
              </TableRow>
            );
          })}
        </TableBody>
        <TableFooter>
          <TableRow>
            <TableCell className="pl-6 font-medium">Total</TableCell>
            {cols.map(([k, , cls]) => (
              <TableCell key={k} className={cn("text-right font-medium tabular-nums", cls, k === "profit" && "pr-6")}>
                {formatBdt(sum(months, k))}
              </TableCell>
            ))}
          </TableRow>
        </TableFooter>
      </Table>
    </Card>
  );
}

// ── Ledger ───────────────────────────────────────────────────

function Ledger({ current, today, isCEO }: { current: string; today: string; isCEO: boolean }) {
  const [month, setMonth] = useState(current);
  const [kind, setKind] = useState("all");
  const [search, setSearch] = useState("");
  const [editing, setEditing] = useState<FinanceEntry | null>(null);
  const [from, to] = monthDays(month || current);
  const filter = { from, to, kind: kind === "all" ? "" : kind, q: search.trim() };
  const entries = useFinanceEntries(filter);
  const remove = useFinanceAction((id: string) => api(`/finance/entries/${id}`, { method: "DELETE" }), "Entry deleted");

  const totals = useMemo(() => {
    let income = 0;
    let expense = 0;
    for (const e of entries.data ?? []) {
      if (e.kind === "income") income += Number(e.amount_bdt);
      else expense += Number(e.amount_bdt);
    }
    return { income, expense };
  }, [entries.data]);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Input type="month" aria-label="Month" value={month} onChange={(e) => setMonth(e.target.value)} className="w-44" />
        <Select value={kind} onValueChange={setKind}>
          <SelectTrigger className="w-36" aria-label="Type">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All</SelectItem>
            <SelectItem value="income">Income</SelectItem>
            <SelectItem value="expense">Expenses</SelectItem>
          </SelectContent>
        </Select>
        <InputGroup className="w-full sm:w-64">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput placeholder="Search descriptions" value={search} onChange={(e) => setSearch(e.target.value)} />
        </InputGroup>
        <Button variant="outline" asChild className="sm:ml-auto">
          <a href={`/api/finance/entries/export.csv?${entryQuery(filter)}`} download>
            <DownloadIcon /> CSV
          </a>
        </Button>
      </div>

      {(entries.data ?? []).length > 0 && (
        <p className="text-sm text-muted-foreground">
          {entries.data!.length} {entries.data!.length === 1 ? "entry" : "entries"}
          {(search || kind !== "all") && " matching"}:{" "}
          <span className="font-medium text-foreground tabular-nums">+{formatBdt(totals.income)}</span> income ·{" "}
          <span className="font-medium text-foreground tabular-nums">−{formatBdt(totals.expense)}</span> expenses
        </p>
      )}
      <Card className="py-0">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-4">Date</TableHead>
              <TableHead>Description</TableHead>
              <TableHead className="hidden md:table-cell">Category</TableHead>
              <TableHead className="text-right">Amount</TableHead>
              <TableHead className="w-20 pr-4" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {entries.isPending &&
              Array.from({ length: 4 }, (_, i) => (
                <TableRow key={i}>
                  <TableCell colSpan={5} className="pl-4">
                    <Skeleton className="h-8 w-full" />
                  </TableCell>
                </TableRow>
              ))}
            {(entries.data ?? []).map((e) => (
              <TableRow key={e.id}>
                <TableCell className="pl-4 text-sm whitespace-nowrap">{formatDate(e.entry_date)}</TableCell>
                <TableCell className="max-w-72">
                  <div className="truncate text-sm" title={e.description}>
                    {e.description}
                  </div>
                  <div className="text-xs text-muted-foreground md:hidden">{e.category_name}</div>
                  <div className="text-xs text-muted-foreground" title={`Added by ${e.created_by_name}`}>
                    {e.invoice_id ? (
                      <Link href="/invoices" className="font-mono hover:underline">
                        {invoiceRef(e.invoice_number)}
                      </Link>
                    ) : (
                      e.updated_by_name
                    )}
                  </div>
                </TableCell>
                <TableCell className="hidden text-sm md:table-cell">{e.category_name}</TableCell>
                <TableCell className="text-right whitespace-nowrap">
                  <div className="font-medium tabular-nums">
                    {e.kind === "income" ? "+" : "−"}
                    {formatBdt(e.amount_bdt)}
                  </div>
                  {e.currency !== "BDT" && (
                    <div className="text-xs text-muted-foreground tabular-nums">
                      {formatMoney(e.amount, e.currency)} × {Number(e.rate_to_bdt)}
                    </div>
                  )}
                </TableCell>
                <TableCell className="pr-4">
                  {/* A line the CEO approved as an invoice is the CEO's alone to correct. */}
                  <div className="flex justify-end">
                    {(!e.invoice_id || isCEO) && (
                      <>
                        <Button size="icon" variant="ghost" aria-label="Edit entry" onClick={() => setEditing(e)}>
                          <PencilIcon />
                        </Button>
                        <Button
                          size="icon"
                          variant="ghost"
                          aria-label="Delete entry"
                          onClick={() => confirm(`Delete "${e.description}"? This can't be undone.`) && remove.mutate(e.id)}
                        >
                          <Trash2Icon />
                        </Button>
                      </>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {entries.isSuccess && (entries.data ?? []).length === 0 && (
          <Empty className="border-t">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <LandmarkIcon />
              </EmptyMedia>
              <EmptyTitle>{search || kind !== "all" ? "Nothing matches" : `Nothing recorded for ${formatMonth(month || current)}`}</EmptyTitle>
              <EmptyDescription>
                Approved invoices land here as expenses. Income that didn&apos;t come through a marketplace project is added by hand.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
      </Card>
      {editing && <EntryDialog entry={editing} today={today} onClose={() => setEditing(null)} />}
    </div>
  );
}
