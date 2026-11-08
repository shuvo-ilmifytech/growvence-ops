"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { LockIcon, SearchIcon, WalletIcon } from "lucide-react";
import { formatDate, formatMoney } from "@/lib/format";
import { useSalaries } from "@/lib/hr";
import { useMe } from "@/lib/session";
import { PageHeader } from "@/components/page-header";
import { Card, CardContent } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export default function SalariesPage() {
  const { data: me } = useMe();
  const allowed = me?.role === "hr" || me?.role === "ceo";
  const salaries = useSalaries(allowed);
  const [search, setSearch] = useState("");

  const rows = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (salaries.data ?? [])
      .filter((r) => !q || [r.user_name, r.department_name, r.designation].some((v) => v.toLowerCase().includes(q)))
      .sort((a, b) => a.user_name.localeCompare(b.user_name));
  }, [salaries.data, search]);

  const totals = useMemo(() => {
    const t: Record<string, number> = {};
    for (const r of rows) t[r.currency] = (t[r.currency] ?? 0) + Number(r.amount);
    return Object.entries(t);
  }, [rows]);

  if (me && !allowed) {
    return (
      <Empty className="min-h-[60vh]">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <LockIcon />
          </EmptyMedia>
          <EmptyTitle>HR and the CEO only</EmptyTitle>
          <EmptyDescription>Salaries are private.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Salaries"
        description="Current monthly gross for everyone. Your own salary is never shown to you. Open a person to add a change."
      />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-[1fr_auto] sm:items-end">
        <InputGroup className="sm:w-72">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput placeholder="Search name, department, designation" value={search} onChange={(e) => setSearch(e.target.value)} />
        </InputGroup>
        {totals.length > 0 && (
          <Card className="gap-0 py-3">
            <CardContent className="px-4">
              <div className="text-xs text-muted-foreground">Monthly total{search && " (filtered)"}</div>
              <div className="flex flex-wrap gap-x-4 text-lg font-semibold tabular-nums">
                {totals.map(([cur, sum]) => (
                  <span key={cur}>{formatMoney(String(sum), cur)}</span>
                ))}
              </div>
            </CardContent>
          </Card>
        )}
      </div>

      <Card className="py-0">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-4">Person</TableHead>
              <TableHead className="hidden md:table-cell">Department</TableHead>
              <TableHead className="text-right">Monthly gross</TableHead>
              <TableHead className="hidden pr-4 sm:table-cell">Since</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {salaries.isPending &&
              Array.from({ length: 4 }, (_, i) => (
                <TableRow key={i}>
                  <TableCell colSpan={4} className="pl-4">
                    <Skeleton className="h-8 w-full" />
                  </TableCell>
                </TableRow>
              ))}
            {rows.map((r) => (
              <TableRow key={r.user_id}>
                <TableCell className="pl-4">
                  <Link href={`/hr/people/${r.user_id}`} className="font-medium hover:underline">
                    {r.user_name}
                  </Link>
                  <div className="text-xs text-muted-foreground">{r.designation || "—"}</div>
                </TableCell>
                <TableCell className="hidden text-sm md:table-cell">{r.department_name || "—"}</TableCell>
                <TableCell className="text-right font-medium tabular-nums">{formatMoney(r.amount, r.currency)}</TableCell>
                <TableCell className="hidden pr-4 text-sm text-muted-foreground sm:table-cell">{formatDate(r.effective_from)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {salaries.isSuccess && rows.length === 0 && (
          <Empty className="border-t">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <WalletIcon />
              </EmptyMedia>
              <EmptyTitle>{search ? "No one found" : "No salaries yet"}</EmptyTitle>
              <EmptyDescription>{search ? "Try a different search." : "Open someone in People and add their salary."}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
      </Card>
    </div>
  );
}
