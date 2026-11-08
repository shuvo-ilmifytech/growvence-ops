"use client";

import { useState } from "react";
import Link from "next/link";
import { Building2Icon, PlusIcon } from "lucide-react";
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
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { useClients, useCreateClient, type ClientType } from "@/lib/clients";
import { useMe } from "@/lib/session";
import { cn } from "@/lib/utils";

/**
 * Clients: the businesses (and individuals) we work for. Each gets a
 * CL-0001… code; link one to a project to collaborate with them on it.
 */
export default function ClientsPage() {
  const { data: me } = useMe();
  const list = useClients();
  const [adding, setAdding] = useState(false);
  const clients = list.data?.clients ?? [];
  const canAdd = !!me && (me.is_admin || me.role === "sales");

  return (
    <div className="space-y-6">
      <PageHeader
        title="Clients"
        description="Businesses and individuals we work for. Link a client to a project to share tasks, messages and files with them."
        actions={
          canAdd && (
            <Button onClick={() => setAdding(true)}>
              <PlusIcon /> Add client
            </Button>
          )
        }
      />

      {list.isPending && (
        <div className="grid gap-4 md:grid-cols-2">
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-36" />
          ))}
        </div>
      )}

      {list.isError && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>Couldn't load clients</EmptyTitle>
            <EmptyDescription>Please try refreshing the page.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {!list.isPending && !list.isError && clients.length === 0 && (
        <Empty className="min-h-[40vh]">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Building2Icon />
            </EmptyMedia>
            <EmptyTitle>No clients yet</EmptyTitle>
            <EmptyDescription>Add your first client to start collaborating on their projects.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      <div className="grid gap-4 md:grid-cols-2">
        {clients.map((c) => (
          <Link key={c.id} href={`/clients/${c.id}`}>
            <Card className="transition hover:shadow-md">
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between gap-2">
                  <CardTitle className="text-base">
                    <span className="mr-2 font-mono text-sm text-muted-foreground">{c.client_code}</span>
                    {c.company_name}
                  </CardTitle>
                  <Badge
                    className={cn(
                      c.status === "active"
                        ? "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200"
                        : "bg-muted text-muted-foreground",
                    )}
                  >
                    {c.status}
                  </Badge>
                </div>
              </CardHeader>
              <CardContent className="space-y-1 text-sm text-muted-foreground">
                {c.contact_person && <p>{c.contact_person}</p>}
                {c.email && <p className="break-all">{c.email}</p>}
                <p>
                  {c.type} · {c.project_count} {c.project_count === 1 ? "project" : "projects"}
                </p>
              </CardContent>
            </Card>
          </Link>
        ))}
      </div>

      <AddClientDialog open={adding} onOpenChange={setAdding} />
    </div>
  );
}

function AddClientDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const create = useCreateClient();
  const [type, setType] = useState<ClientType>("B2B");

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const d = new FormData(e.currentTarget);
    try {
      const res = await create.mutateAsync({
        company_name: String(d.get("company_name")),
        contact_person: String(d.get("contact_person") ?? ""),
        email: String(d.get("email") ?? ""),
        phone: String(d.get("phone") ?? ""),
        address: String(d.get("address") ?? ""),
        type,
      });
      toast.success(`Client ${res.client_code} added.`);
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Couldn't add the client.");
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => (onOpenChange(o), o || create.reset())}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Add client</DialogTitle>
          <DialogDescription>The client gets a CL-0001… code automatically.</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="company_name">Company / client name</Label>
            <Input id="company_name" name="company_name" required maxLength={200} placeholder="Demo Client Ltd" />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div className="grid gap-2">
              <Label htmlFor="contact_person">Contact person</Label>
              <Input id="contact_person" name="contact_person" maxLength={120} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="type">Type</Label>
              <Select value={type} onValueChange={(v) => setType(v as ClientType)}>
                <SelectTrigger id="type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="B2B">B2B (company)</SelectItem>
                  <SelectItem value="Individual">Individual</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div className="grid gap-2">
              <Label htmlFor="email">Email</Label>
              <Input id="email" name="email" type="email" maxLength={200} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="phone">Phone</Label>
              <Input id="phone" name="phone" maxLength={50} />
            </div>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="address">Address</Label>
            <Textarea id="address" name="address" rows={2} maxLength={500} />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending && <Spinner />} Add client
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
