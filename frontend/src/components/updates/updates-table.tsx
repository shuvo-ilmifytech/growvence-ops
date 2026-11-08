"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import Link from "next/link";
import { ChevronLeftIcon, ChevronRightIcon, InboxIcon, MegaphoneIcon, PaperclipIcon, SearchIcon } from "lucide-react";
import { useMe } from "@/lib/session";
import { useUpdates } from "@/lib/updates";
import { formatDateTime } from "@/lib/format";
import { DESTINATION_LABELS, DESTINATIONS, type Role, type UpdateStatus } from "@/lib/types";
import { PageHeader } from "@/components/page-header";
import { DestinationLabel, UpdateStatusBadge } from "@/components/update-badges";
import { UpdateSheet } from "@/components/updates/update-sheet";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";

type View = { value: string; label: string; statuses: UpdateStatus[] };

const VIEWS: Record<Role, View[]> = {
  // Roles with no project access never reach this table; the entry keeps the map total.
  hr: [{ value: "all", label: "All", statuses: [] }],
  office_admin: [{ value: "all", label: "All", statuses: [] }],
  client: [{ value: "all", label: "All", statuses: [] }],
  investor: [{ value: "all", label: "All", statuses: [] }],
  tl: [
    { value: "approve", label: "To approve", statuses: ["pending_approval"] },
    { value: "changes", label: "Changes requested", statuses: ["changes_requested"] },
    { value: "unsent", label: "Approved, not sent", statuses: ["approved"] },
    { value: "sent", label: "Sent", statuses: ["sent"] },
    { value: "all", label: "All", statuses: [] },
  ],
  sales: [
    { value: "send", label: "Ready to send", statuses: ["approved"] },
    { value: "sent", label: "Sent", statuses: ["sent"] },
    { value: "all", label: "All", statuses: [] },
  ],
  member: [
    { value: "work", label: "Needs work", statuses: ["draft", "changes_requested"] },
    { value: "pending", label: "Waiting for approval", statuses: ["pending_approval"] },
    { value: "approved", label: "Approved", statuses: ["approved"] },
    { value: "sent", label: "Sent", statuses: ["sent"] },
    { value: "all", label: "All", statuses: [] },
  ],
  ceo: [
    { value: "all", label: "All", statuses: [] },
    { value: "approve", label: "Pending approval", statuses: ["pending_approval"] },
    { value: "unsent", label: "Approved, not sent", statuses: ["approved"] },
    { value: "sent", label: "Sent", statuses: ["sent"] },
    { value: "draft", label: "Drafts", statuses: ["draft"] },
  ],
};

const PAGE_SIZE = 50;

export function UpdatesTable() {
  const { data: me } = useMe();
  const router = useRouter();
  const pathname = usePathname();
  const sp = useSearchParams();
  const views = me ? VIEWS[me.role] : [];
  const view = sp.get("view") ?? views[0]?.value ?? "all";
  const destination = sp.get("destination") ?? "";
  const page = Number(sp.get("page") ?? 1);
  const urlQ = sp.get("q") ?? "";
  const [q, setQ] = useState(urlQ);
  const openId = sp.get("open");

  function setParams(patch: Record<string, string | null>) {
    const next = new URLSearchParams(sp);
    for (const [k, v] of Object.entries(patch)) {
      if (v === null || v === "") next.delete(k);
      else next.set(k, v);
    }
    if (!("page" in patch) && !("open" in patch)) next.delete("page");
    router.replace(`${pathname}?${next}`, { scroll: false });
  }

  useEffect(() => {
    if (q === urlQ) return;
    const t = setTimeout(() => setParams({ q }), 300);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q]);

  const statuses = views.find((v) => v.value === view)?.statuses ?? [];
  const list = useUpdates({ status: statuses.join(","), destination, q: urlQ, page, page_size: PAGE_SIZE }, !!me);
  const counts = list.data?.counts ?? {};
  const countFor = (st: UpdateStatus[]) =>
    (st.length ? st : (Object.keys(counts) as UpdateStatus[])).reduce((n, s) => n + (counts[s] ?? 0), 0);
  const pages = Math.max(1, Math.ceil((list.data?.total ?? 0) / PAGE_SIZE));

  return (
    <div className="space-y-6">
      <PageHeader
        title="Updates"
        description={
          me?.role === "sales"
            ? "Approved updates to post on the marketplace, then mark as sent."
            : me?.role === "tl"
              ? "Review what the team wants to tell clients."
              : me?.role === "member"
                ? "Updates you've written for your projects."
                : "Every client update across the team."
        }
        actions={
          /* Updates for clients with no project live on their request, which is next door. */
          <Button variant="outline" asChild>
            <Link href="/requests">
              <InboxIcon /> No-project requests
            </Link>
          </Button>
        }
      />

      <Tabs value={view} onValueChange={(v) => setParams({ view: v })}>
        <TabsList className="flex-wrap">
          {views.map((v) => (
            <TabsTrigger key={v.value} value={v.value}>
              {v.label}
              {list.data && <span className="ml-1.5 text-xs tabular-nums text-muted-foreground">{countFor(v.statuses)}</span>}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>

      <div className="flex flex-wrap gap-3">
        <InputGroup className="w-full sm:w-80">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput placeholder="Search message, order ID, client or update #" value={q} onChange={(e) => setQ(e.target.value)} />
        </InputGroup>
        <Select value={destination || "any"} onValueChange={(v) => setParams({ destination: v === "any" ? null : v })}>
          <SelectTrigger className="w-44">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="any">Any destination</SelectItem>
            {DESTINATIONS.map((d) => (
              <SelectItem key={d} value={d}>
                {DESTINATION_LABELS[d]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <Card className="py-0">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="pl-4">Update</TableHead>
                <TableHead>Order ID</TableHead>
                <TableHead>Profile</TableHead>
                <TableHead>Client</TableHead>
                <TableHead>Update by</TableHead>
                <TableHead className="min-w-64">Message</TableHead>
                <TableHead>Update to</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Checked by</TableHead>
                <TableHead className="pr-4">Sent by</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody className={cn(list.isFetching && list.isPlaceholderData && "opacity-60")}>
              {list.isPending &&
                Array.from({ length: 6 }, (_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 10 }, (_, j) => (
                      <TableCell key={j} className={j === 0 ? "pl-4" : undefined}>
                        <Skeleton className="h-4 w-full" />
                      </TableCell>
                    ))}
                  </TableRow>
                ))}
              {list.data?.updates.map((u) => (
                <TableRow key={u.id} className="cursor-pointer" onClick={() => setParams({ open: u.id })}>
                  <TableCell className="pl-4 whitespace-nowrap">
                    <div className="font-mono text-xs">UPD-{u.number}</div>
                    <div className="text-xs text-muted-foreground">{formatDateTime(u.created_at)}</div>
                  </TableCell>
                  <TableCell className="font-mono text-xs">{u.order_id ?? "—"}</TableCell>
                  <TableCell className="text-sm">{u.profile_name ?? "—"}</TableCell>
                  <TableCell className="text-sm">
                    {u.client_name ?? "—"}
                    {/* A client with no project in the portal: the update hangs on the request. */}
                    {u.client_request_id && <span className="ml-1.5 text-xs text-muted-foreground">· CR-{u.project_number}</span>}
                  </TableCell>
                  <TableCell className="text-sm whitespace-nowrap">{u.author_name}</TableCell>
                  <TableCell className="max-w-80">
                    <p className="line-clamp-2 text-sm">{u.message || <span className="italic text-muted-foreground">Empty draft</span>}</p>
                    {u.attachment_count > 0 && (
                      <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                        <PaperclipIcon className="size-3" /> {u.attachment_count}
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="whitespace-nowrap">
                    <DestinationLabel destination={u.destination} />
                  </TableCell>
                  <TableCell>
                    <UpdateStatusBadge status={u.status} />
                  </TableCell>
                  <TableCell className="text-xs whitespace-nowrap">
                    {u.checked_by_name ? (
                      <>
                        <div>{u.checked_by_name}</div>
                        <div className="text-muted-foreground">{formatDateTime(u.checked_at!)}</div>
                      </>
                    ) : u.auto_approved ? (
                      <span className="text-muted-foreground">Auto</span>
                    ) : (
                      "—"
                    )}
                  </TableCell>
                  <TableCell className="pr-4 text-xs whitespace-nowrap">
                    {u.sent_by_name ? (
                      <>
                        <div>{u.sent_by_name}</div>
                        <div className="text-muted-foreground">{formatDateTime(u.sent_at!)}</div>
                      </>
                    ) : (
                      "—"
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        {list.isSuccess && list.data.updates.length === 0 && (
          <Empty className="border-t">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <MegaphoneIcon />
              </EmptyMedia>
              <EmptyTitle>Nothing here</EmptyTitle>
              <EmptyDescription>{urlQ || destination ? "Try clearing the filters." : "No updates in this view right now."}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        {list.data && list.data.total > PAGE_SIZE && (
          <div className="flex items-center justify-between border-t px-4 py-3 text-sm text-muted-foreground">
            <span>
              {(page - 1) * PAGE_SIZE + 1}–{Math.min(page * PAGE_SIZE, list.data.total)} of {list.data.total}
            </span>
            <div className="flex gap-2">
              <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setParams({ page: String(page - 1) })}>
                <ChevronLeftIcon /> Previous
              </Button>
              <Button variant="outline" size="sm" disabled={page >= pages} onClick={() => setParams({ page: String(page + 1) })}>
                Next <ChevronRightIcon />
              </Button>
            </div>
          </div>
        )}
      </Card>

      <UpdateSheet updateId={openId} onClose={() => setParams({ open: null })} />
    </div>
  );
}
