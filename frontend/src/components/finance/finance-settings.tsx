"use client";

import { useState } from "react";
import { CheckIcon, PencilIcon, PlusIcon, Trash2Icon, XIcon } from "lucide-react";
import { api } from "@/lib/api";
import { formatDate } from "@/lib/format";
import { useFinanceAction, useFinanceCategories, useFinanceRates, type FinanceCategory, type FinanceKind } from "@/lib/finance";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export function FinanceSettings({ today }: { today: string }) {
  return (
    <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
      <RatesCard today={today} />
      <div className="grid gap-6">
        <CategoriesCard kind="expense" />
        <CategoriesCard kind="income" />
      </div>
    </div>
  );
}

function RatesCard({ today }: { today: string }) {
  const rates = useFinanceRates();
  const add = useFinanceAction(
    (body: { currency: string; rate: string; effective_from: string }) => api("/finance/rates", { method: "POST", body }),
    "Rate saved",
  );
  const remove = useFinanceAction((id: string) => api(`/finance/rates/${id}`, { method: "DELETE" }), "Rate removed");
  const [currency, setCurrency] = useState("USD");
  const [rate, setRate] = useState("");
  const [from, setFrom] = useState(today);

  return (
    <Card className="self-start">
      <CardHeader>
        <CardTitle>Exchange rates</CardTitle>
        <CardDescription>
          BDT for one unit of another currency, from a date on. Project earnings and salaries in that currency use the rate on each
          month&apos;s last day. Entries keep the rate they were saved with.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <form
          className="grid grid-cols-[5rem_1fr] gap-2 sm:grid-cols-[5rem_1fr_10rem_auto]"
          onSubmit={(e) => {
            e.preventDefault();
            add.mutate({ currency: currency.trim().toUpperCase(), rate, effective_from: from }, { onSuccess: () => setRate("") });
          }}
        >
          <Input aria-label="Currency" value={currency} onChange={(e) => setCurrency(e.target.value)} maxLength={3} className="uppercase" required />
          <Input aria-label="Rate in BDT" inputMode="decimal" placeholder="BDT, e.g. 122.50" value={rate} onChange={(e) => setRate(e.target.value)} required />
          <Input aria-label="Effective from" type="date" value={from} onChange={(e) => setFrom(e.target.value)} required className="max-sm:col-span-2" />
          <Button type="submit" disabled={add.isPending} className="max-sm:col-span-2">
            <PlusIcon /> Add
          </Button>
        </form>
        {rates.isPending ? (
          <Skeleton className="h-24 w-full" />
        ) : (rates.data ?? []).length === 0 ? (
          <p className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">
            No rates yet. Add the USD rate so marketplace earnings can be counted in BDT.
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>Currency</TableHead>
                <TableHead className="text-right">BDT</TableHead>
                <TableHead>From</TableHead>
                <TableHead className="w-10" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {(rates.data ?? []).map((r, i, all) => (
                <TableRow key={r.id}>
                  <TableCell className="font-medium">
                    {r.currency}
                    {all.findIndex((x) => x.currency === r.currency && x.effective_from <= today) === i && (
                      <Badge variant="secondary" className="ml-2">
                        Current
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{Number(r.rate)}</TableCell>
                  <TableCell className="text-sm text-muted-foreground">{formatDate(r.effective_from)}</TableCell>
                  <TableCell>
                    <Button
                      size="icon"
                      variant="ghost"
                      aria-label={`Remove ${r.currency} rate from ${r.effective_from}`}
                      onClick={() => confirm("Remove this rate? Use this only for mistakes.") && remove.mutate(r.id)}
                    >
                      <Trash2Icon />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

function CategoriesCard({ kind }: { kind: FinanceKind }) {
  const categories = useFinanceCategories();
  const add = useFinanceAction((name: string) => api("/finance/categories", { method: "POST", body: { name, kind } }), "Category added");
  const [name, setName] = useState("");
  const list = (categories.data ?? []).filter((c) => c.kind === kind);

  return (
    <Card>
      <CardHeader>
        <CardTitle>{kind === "income" ? "Income categories" : "Expense categories"}</CardTitle>
        <CardDescription>Switch one off to hide it from new entries. Past entries keep it.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            add.mutate(name.trim(), { onSuccess: () => setName("") });
          }}
        >
          <Input aria-label="New category" placeholder="New category" value={name} onChange={(e) => setName(e.target.value)} maxLength={80} required />
          <Button type="submit" variant="outline" disabled={add.isPending}>
            <PlusIcon /> Add
          </Button>
        </form>
        {categories.isPending && <Skeleton className="h-24 w-full" />}
        <ul className="divide-y">
          {list.map((c) => (
            <CategoryRow key={c.id} c={c} />
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}

function CategoryRow({ c }: { c: FinanceCategory }) {
  const update = useFinanceAction(
    (body: { name?: string; is_active?: boolean }) => api(`/finance/categories/${c.id}`, { method: "PATCH", body }),
    "Category saved",
  );
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(c.name);

  return (
    <li className="flex items-center gap-2 py-2">
      {editing ? (
        <form
          className="flex flex-1 gap-1"
          onSubmit={(e) => {
            e.preventDefault();
            update.mutate({ name: name.trim() }, { onSuccess: () => setEditing(false) });
          }}
        >
          <Input aria-label="Category name" value={name} onChange={(e) => setName(e.target.value)} maxLength={80} autoFocus required className="h-8" />
          <Button size="icon" variant="ghost" type="submit" aria-label="Save name">
            <CheckIcon />
          </Button>
          <Button
            size="icon"
            variant="ghost"
            type="button"
            aria-label="Cancel"
            onClick={() => {
              setName(c.name);
              setEditing(false);
            }}
          >
            <XIcon />
          </Button>
        </form>
      ) : (
        <>
          <span className={c.is_active ? "flex-1 text-sm" : "flex-1 text-sm text-muted-foreground line-through"}>{c.name}</span>
          <span className="text-xs text-muted-foreground tabular-nums">{c.entries || ""}</span>
          <Button size="icon" variant="ghost" aria-label={`Rename ${c.name}`} onClick={() => setEditing(true)}>
            <PencilIcon />
          </Button>
        </>
      )}
      <Switch checked={c.is_active} onCheckedChange={(v) => update.mutate({ is_active: v })} aria-label={`${c.name} in use`} />
    </li>
  );
}
