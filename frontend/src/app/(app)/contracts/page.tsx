"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { FileTextIcon, PlusIcon } from "lucide-react";
import { toast } from "sonner";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import {
  contractValue,
  useContracts,
  useCreateContract,
  useRenewContract,
  useUpdateContract,
  type Contract,
  type ContractInput,
  type ContractStatus,
} from "@/lib/b2b";
import { useClients } from "@/lib/clients";
import { money } from "@/lib/payments";
import { useProjects } from "@/lib/projects";
import { useMe } from "@/lib/session";
import { cn } from "@/lib/utils";

const STATUS_TONE: Record<ContractStatus, string> = {
  draft: "bg-muted text-muted-foreground",
  active: "bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300",
  completed: "bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-300",
  terminated: "bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-300",
  expired: "bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300",
};

const STATUS_LABEL: Record<ContractStatus, string> = {
  draft: "Draft",
  active: "Active",
  completed: "Completed",
  terminated: "Terminated",
  expired: "Expired",
};

const NEXT_STATUS: Partial<Record<ContractStatus, ContractStatus[]>> = {
  draft: ["active", "terminated"],
  active: ["completed", "terminated", "expired"],
};

function fmtDate(d: string | null) {
  if (!d) return "—";
  return new Date(d.length === 10 ? d + "T00:00:00" : d).toLocaleDateString();
}

function emptyForm(): ContractInput {
  return {
    client_id: "",
    project_id: "",
    title: "",
    value: 0,
    currency: "BDT",
    start_date: "",
    end_date: "",
    terms: "",
    document_url: "",
    status: "draft",
  };
}

function ContractDialog({
  open,
  onOpenChange,
  initial,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  initial?: Contract | null;
}) {
  const create = useCreateContract();
  const update = useUpdateContract();
  const clients = useClients();
  const projects = useProjects({ page_size: 100 });
  const [form, setForm] = useState<ContractInput>(emptyForm());

  useEffect(() => {
    if (open) {
      setForm(
        initial
          ? {
              client_id: initial.client_id,
              project_id: initial.project_id ?? "",
              title: initial.title,
              value: contractValue(initial),
              currency: initial.currency,
              start_date: initial.start_date,
              end_date: initial.end_date ?? "",
              terms: initial.terms,
              document_url: initial.document_url ?? "",
              status: initial.status,
            }
          : { ...emptyForm(), start_date: new Date().toISOString().slice(0, 10) },
      );
    }
  }, [open, initial]);

  const pending = create.isPending || update.isPending;
  const valid =
    form.client_id !== "" &&
    form.title.trim() !== "" &&
    form.value >= 0 &&
    form.start_date !== "" &&
    (form.end_date === "" || form.end_date >= form.start_date);
  const save = () => {
    if (!valid) return;
    if (initial) {
      update.mutate({ id: initial.id, input: form }, { onSuccess: () => onOpenChange(false) });
    } else {
      create.mutate(form, { onSuccess: () => onOpenChange(false) });
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{initial ? `Edit ${initial.contract_number}` : "New contract"}</DialogTitle>
          <DialogDescription>
            A signed agreement with a client, optionally tied to a project. The contract
            number is generated automatically.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="ct-client">Client</Label>
            <Select
              value={form.client_id}
              onValueChange={(v) => setForm({ ...form, client_id: v })}
              disabled={!!initial}
            >
              <SelectTrigger id="ct-client">
                <SelectValue placeholder="Choose a client" />
              </SelectTrigger>
              <SelectContent>
                {(clients.data?.clients ?? []).map((c) => (
                  <SelectItem key={c.id} value={c.id}>
                    {c.company_name} ({c.client_code})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="ct-project">Project (optional)</Label>
            <Select
              value={form.project_id || "none"}
              onValueChange={(v) => setForm({ ...form, project_id: v === "none" ? "" : v })}
            >
              <SelectTrigger id="ct-project">
                <SelectValue placeholder="No project" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="none">No project</SelectItem>
                {(projects.data?.projects ?? []).map((p) => (
                  <SelectItem key={p.id} value={p.id}>
                    {p.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="ct-title">Title</Label>
            <Input
              id="ct-title"
              value={form.title}
              onChange={(e) => setForm({ ...form, title: e.target.value })}
              placeholder="e.g. Website redesign — 12 month support"
            />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="ct-value">Value</Label>
              <Input
                id="ct-value"
                type="number"
                min="0"
                step="0.01"
                value={form.value || ""}
                onChange={(e) => setForm({ ...form, value: Number(e.target.value) })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="ct-currency">Currency</Label>
              <Select value={form.currency} onValueChange={(v) => setForm({ ...form, currency: v })}>
                <SelectTrigger id="ct-currency">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {["BDT", "USD"].map((c) => (
                    <SelectItem key={c} value={c}>
                      {c}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="ct-start">Start date</Label>
              <Input
                id="ct-start"
                type="date"
                value={form.start_date}
                onChange={(e) => setForm({ ...form, start_date: e.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="ct-end">End date (optional)</Label>
              <Input
                id="ct-end"
                type="date"
                value={form.end_date}
                onChange={(e) => setForm({ ...form, end_date: e.target.value })}
              />
            </div>
          </div>
          {initial && (
            <div className="space-y-2">
              <Label htmlFor="ct-status">Status</Label>
              <Select
                value={form.status}
                onValueChange={(v) => setForm({ ...form, status: v as ContractStatus })}
              >
                <SelectTrigger id="ct-status">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {Array.from(new Set([initial.status, ...(NEXT_STATUS[initial.status] ?? [])])).map(
                    (s) => (
                      <SelectItem key={s} value={s}>
                        {STATUS_LABEL[s]}
                      </SelectItem>
                    ),
                  )}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                {initial.status === "draft" && "A draft can be activated or terminated."}
                {initial.status === "active" && "An active contract can be completed, terminated or marked expired."}
                {(initial.status === "completed" || initial.status === "terminated" || initial.status === "expired") &&
                  "This contract is closed — renew it instead of editing."}
              </p>
            </div>
          )}
          <div className="space-y-2">
            <Label htmlFor="ct-terms">Terms</Label>
            <Textarea
              id="ct-terms"
              value={form.terms}
              onChange={(e) => setForm({ ...form, terms: e.target.value })}
              placeholder="Payment terms, scope, cancellation…"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="ct-doc">Contract document URL (optional)</Label>
            <Input
              id="ct-doc"
              value={form.document_url}
              onChange={(e) => setForm({ ...form, document_url: e.target.value })}
              placeholder="Link to the signed PDF"
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <Button onClick={save} disabled={pending || !valid}>
            {initial ? "Save changes" : "Create contract"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function RenewDialog({
  open,
  onOpenChange,
  contract,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  contract: Contract | null;
}) {
  const renew = useRenewContract();
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  if (!contract) return null;
  const valid = start !== "" && (end === "" || end >= start);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Renew {contract.contract_number}</DialogTitle>
          <DialogDescription>
            Creates a new draft contract with the same client, project, value and terms.
            The current contract is marked completed.
          </DialogDescription>
        </DialogHeader>
        <div className="grid grid-cols-2 gap-4">
          <div className="space-y-2">
            <Label htmlFor="rn-start">New start date</Label>
            <Input id="rn-start" type="date" value={start} onChange={(e) => setStart(e.target.value)} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="rn-end">New end date (optional)</Label>
            <Input id="rn-end" type="date" value={end} onChange={(e) => setEnd(e.target.value)} />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={renew.isPending}>
            Cancel
          </Button>
          <Button
            disabled={renew.isPending || !valid}
            onClick={() =>
              renew.mutate(
                { id: contract.id, start_date: start, end_date: end },
                { onSuccess: () => onOpenChange(false) },
              )
            }
          >
            Renew contract
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function StatusTimeline({ status }: { status: ContractStatus }) {
  const steps: ContractStatus[] = ["draft", "active", "completed"];
  const reached =
    status === "completed" ? 3 : status === "active" ? 2 : status === "draft" ? 1 : 0;
  if (status === "terminated" || status === "expired") {
    return (
      <p className="text-sm">
        Status: <Badge className={cn(STATUS_TONE[status])}>{STATUS_LABEL[status]}</Badge>
      </p>
    );
  }
  return (
    <ol className="flex items-center gap-2 text-sm">
      {steps.map((s, i) => (
        <li key={s} className="flex items-center gap-2">
          <span
            className={cn(
              "flex size-6 items-center justify-center rounded-full text-xs font-medium",
              i < reached ? "bg-green-600 text-white" : "bg-muted text-muted-foreground",
            )}
          >
            {i + 1}
          </span>
          <span className={i < reached ? "font-medium" : "text-muted-foreground"}>{STATUS_LABEL[s]}</span>
          {i < steps.length - 1 && <span className="text-muted-foreground">→</span>}
        </li>
      ))}
    </ol>
  );
}

export default function ContractsPage() {
  const { data: me } = useMe();
  const [status, setStatus] = useState("");
  const list = useContracts(status);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<Contract | null>(null);
  const [detail, setDetail] = useState<Contract | null>(null);
  const [renewing, setRenewing] = useState<Contract | null>(null);
  const contracts = list.data?.contracts ?? [];
  const canManage = !!me && (me.is_admin || me.role === "sales");

  return (
    <div className="space-y-6">
      <PageHeader
        title="Contracts"
        description="Signed agreements with clients — value, dates, terms and renewals."
        actions={
          canManage ? (
            <Button onClick={() => setAdding(true)}>
              <PlusIcon /> New contract
            </Button>
          ) : undefined
        }
      />

      <div className="flex gap-2">
        <Select value={status || "all"} onValueChange={(v) => setStatus(v === "all" ? "" : v)}>
          <SelectTrigger className="w-44">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All statuses</SelectItem>
            {(Object.keys(STATUS_LABEL) as ContractStatus[]).map((s) => (
              <SelectItem key={s} value={s}>
                {STATUS_LABEL[s]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {list.isPending && (
        <div className="space-y-2">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-16" />
          ))}
        </div>
      )}

      {list.isError && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>Couldn’t load contracts</EmptyTitle>
            <EmptyDescription>Please try refreshing the page.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {!list.isPending && !list.isError && contracts.length === 0 && (
        <Empty className="min-h-[40vh]">
          <EmptyHeader>
            <EmptyMedia>
              <FileTextIcon className="size-8 opacity-40" />
            </EmptyMedia>
            <EmptyTitle>No contracts yet</EmptyTitle>
            <EmptyDescription>
              Create the first contract for a client — the number (CTR-0001…) is generated
              automatically.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {contracts.length > 0 && (
        <Card>
          <CardContent className="p-0">
            <ul className="divide-y">
              {contracts.map((c) => (
                <li key={c.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                  <button
                    type="button"
                    className="min-w-0 flex-1 text-left"
                    onClick={() => setDetail(c)}
                  >
                    <p className="font-medium">
                      {c.contract_number} · {c.title}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {c.client_name} ({c.client_code})
                      {c.project_name ? ` · ${c.project_name}` : ""}
                      {` · ${money(contractValue(c), c.currency)}`}
                      {` · ${fmtDate(c.start_date)} → ${fmtDate(c.end_date)}`}
                    </p>
                  </button>
                  <Badge className={cn(STATUS_TONE[c.status], "capitalize")}>{STATUS_LABEL[c.status]}</Badge>
                  {canManage && (c.status === "draft" || c.status === "active") && (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        setDetail(null);
                        setEditing(c);
                      }}
                    >
                      Edit
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

      <ContractDialog open={adding} onOpenChange={setAdding} />
      <ContractDialog
        open={!!editing}
        onOpenChange={(v) => !v && setEditing(null)}
        initial={editing}
      />

      <Dialog open={!!detail} onOpenChange={(v) => !v && setDetail(null)}>
        <DialogContent className="max-h-[90vh] overflow-y-auto">
          {detail && (
            <>
              <DialogHeader>
                <DialogTitle>
                  {detail.contract_number} · {detail.title}
                </DialogTitle>
                <DialogDescription>
                  {detail.client_name} ({detail.client_code})
                  {detail.project_name ? ` · ${detail.project_name}` : ""}
                </DialogDescription>
              </DialogHeader>
              <div className="space-y-4">
                <StatusTimeline status={detail.status} />
                <dl className="grid grid-cols-2 gap-3 text-sm">
                  <div>
                    <dt className="text-muted-foreground">Value</dt>
                    <dd className="font-medium">{money(contractValue(detail), detail.currency)}</dd>
                  </div>
                  <div>
                    <dt className="text-muted-foreground">Period</dt>
                    <dd className="font-medium">
                      {fmtDate(detail.start_date)} → {fmtDate(detail.end_date)}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-muted-foreground">Signed</dt>
                    <dd className="font-medium">{detail.signed_at ? fmtDate(detail.signed_at) : "Not signed yet"}</dd>
                  </div>
                  <div>
                    <dt className="text-muted-foreground">Created</dt>
                    <dd className="font-medium">{fmtDate(detail.created_at)}</dd>
                  </div>
                </dl>
                {detail.terms && (
                  <div>
                    <p className="text-sm font-medium">Terms</p>
                    <p className="whitespace-pre-wrap text-sm text-muted-foreground">{detail.terms}</p>
                  </div>
                )}
                {detail.document_url && (
                  <p className="text-sm">
                    <Link
                      href={detail.document_url}
                      target="_blank"
                      rel="noreferrer"
                      className="underline underline-offset-2"
                    >
                      Open contract document
                    </Link>
                  </p>
                )}
              </div>
              <DialogFooter>
                {canManage && (detail.status === "active" || detail.status === "completed") && (
                  <Button
                    variant="outline"
                    onClick={() => {
                      setDetail(null);
                      setRenewing(detail);
                    }}
                  >
                    Renew
                  </Button>
                )}
                {canManage && (detail.status === "draft" || detail.status === "active") && (
                  <Button
                    onClick={() => {
                      setDetail(null);
                      setEditing(detail);
                    }}
                  >
                    Edit
                  </Button>
                )}
              </DialogFooter>
            </>
          )}
        </DialogContent>
      </Dialog>

      <RenewDialog open={!!renewing} onOpenChange={(v) => !v && setRenewing(null)} contract={renewing} />
    </div>
  );
}
