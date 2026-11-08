"use client";

import { useState } from "react";
import Link from "next/link";
import { InboxIcon, MessageSquareIcon, PaperclipIcon, PlusIcon, SearchIcon } from "lucide-react";
import { relativeTime } from "@/lib/format";
import { useRequests } from "@/lib/requests";
import { useMe } from "@/lib/session";
import type { ClientRequest, RequestStatus } from "@/lib/types";
import { PageHeader } from "@/components/page-header";
import { NewRequestDialog } from "@/components/requests/new-request-dialog";
import { RequestStatusBadge } from "@/components/requests/request-badges";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

const TABS: { key: string; label: string; status?: RequestStatus }[] = [
  { key: "open", label: "Open", status: "open" },
  { key: "done", label: "Done", status: "done" },
  { key: "cancelled", label: "Cancelled", status: "cancelled" },
  { key: "all", label: "All" },
];

/**
 * Client requests: work for clients with no project in the portal. Sales raise them and
 * send the updates; the team they went to works them.
 */
export default function RequestsPage() {
  const { data: me } = useMe();
  const [tab, setTab] = useState("open");
  const [mine, setMine] = useState(false);
  const [q, setQ] = useState("");
  const [adding, setAdding] = useState(false);
  const status = TABS.find((t) => t.key === tab)?.status;
  const list = useRequests({ status, mine, q: q.trim() || undefined });
  const counts = list.data?.counts ?? {};
  const isSales = me?.role === "sales";
  const isMember = me?.role === "member";

  return (
    <div className="space-y-6">
      <PageHeader
        title="Client requests"
        description={
          isSales
            ? "Changes asked for by clients who have no project here — monthly management, or an order delivered before the portal. Send one to a team and they work it like any other job."
            : isMember
              ? "What clients with no project here have asked your team for. Everything sent to your team is here; whoever holds a card works it and writes the client updates on the request."
              : "Work for clients with no project in the portal. Hand the cards to your team and write the client updates on the request."
        }
        actions={
          list.data?.can_raise && (
            <Button onClick={() => setAdding(true)}>
              <PlusIcon /> New client request
            </Button>
          )
        }
      />

      <div className="flex flex-col gap-3 md:flex-row md:items-center">
        <Tabs value={tab} onValueChange={setTab}>
          <TabsList>
            {TABS.map((t) => (
              <TabsTrigger key={t.key} value={t.key}>
                {t.label}
                {t.status && counts[t.status] ? <span className="ml-1 tabular-nums text-muted-foreground">{counts[t.status]}</span> : null}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <InputGroup className="md:max-w-xs">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search client, order or request" aria-label="Search" />
        </InputGroup>
        {isSales && (
          <Button variant={mine ? "default" : "outline"} aria-pressed={mine} onClick={() => setMine((m) => !m)}>
            Only mine
          </Button>
        )}
      </div>

      {list.isPending ? (
        <div className="space-y-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-24 w-full" />
          ))}
        </div>
      ) : list.data && list.data.requests.length === 0 ? (
        <Empty className="min-h-[40vh]">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <InboxIcon />
            </EmptyMedia>
            <EmptyTitle>Nothing here</EmptyTitle>
            <EmptyDescription>
              {list.data.can_raise
                ? "When a client with no project asks for a change, raise it here and send it to a team."
                : "Requests your team is asked for will show up here."}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <ul className="space-y-3">
          {(list.data?.requests ?? []).map((r) => (
            <li key={r.id}>
              <RequestRow request={r} />
            </li>
          ))}
        </ul>
      )}

      <NewRequestDialog open={adding} onOpenChange={setAdding} />
    </div>
  );
}

function RequestRow({ request: r }: { request: ClientRequest }) {
  return (
    <Link
      href={`/requests/${r.id}`}
      className="block rounded-lg border p-4 transition hover:border-primary/40 hover:bg-muted/30"
    >
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span className="font-mono text-muted-foreground">CR-{r.number}</span>
        <RequestStatusBadge status={r.status} />
        <Badge variant="outline">{r.team_name}</Badge>
        {r.unsent_update_count > 0 && <Badge variant="secondary">{r.unsent_update_count} to send</Badge>}
        <span className="ml-auto text-muted-foreground">{relativeTime(r.last_activity_at)}</span>
      </div>
      <p className="mt-1.5 font-medium">{r.title}</p>
      <p className="mt-0.5 text-sm text-muted-foreground">
        {r.client_name}
        {r.order_id ? ` · ${r.order_id}` : " · no order ID"} · {r.profile_name} · raised by {r.created_by_name}
      </p>
      <div className="mt-2 flex flex-wrap gap-3 text-xs text-muted-foreground">
        <span className="inline-flex items-center gap-1">
          <PaperclipIcon className="size-3" /> {r.done_card_count}/{r.card_count} cards done
        </span>
        <span className="inline-flex items-center gap-1">
          <MessageSquareIcon className="size-3" /> {r.update_count} update{r.update_count === 1 ? "" : "s"}
        </span>
        {r.member_names?.length ? <span>With {r.member_names.join(", ")}</span> : null}
      </div>
    </Link>
  );
}
