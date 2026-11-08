"use client";

import { use, useState } from "react";
import Link from "next/link";
import { PlusIcon, Trash2Icon } from "lucide-react";
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
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
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
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  useAddContact,
  useClient,
  useDeleteContact,
  useUpdateClient,
  type BusinessClient,
  type ClientStatus,
  type ClientType,
} from "@/lib/clients";
import { useMe } from "@/lib/session";

export function ClientDetail({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const { data: me } = useMe();
  const detail = useClient(id);
  const d = detail.data;
  const canEdit = !!me && (me.is_admin || me.role === "sales");

  if (detail.isPending) return <Skeleton className="h-64" />;
  if (detail.isError || !d) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>Client not found</EmptyTitle>
          <EmptyDescription>It may have been removed, or you don&apos;t have access to it.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }
  const c = d.client;

  return (
    <div className="space-y-6">
      <PageHeader
        title={`${c.client_code} ${c.company_name}`}
        description={`${c.type} · ${c.project_count} ${c.project_count === 1 ? "project" : "projects"}`}
      />

      <div className="grid gap-6 lg:grid-cols-2">
        {/* Mounted only once the client has loaded, so the selects seed from it. */}
        <ClientEditForm key={c.id} client={c} canEdit={canEdit} />
        <ClientContactsAndProjects clientId={c.id} client={c} canEdit={canEdit} />
      </div>
    </div>
  );
}

function ClientEditForm({ client: c, canEdit }: { client: BusinessClient; canEdit: boolean }) {
  const update = useUpdateClient(c.id);
  const [type, setType] = useState<ClientType>(c.type);
  const [status, setStatus] = useState<ClientStatus>(c.status);

  async function onSave(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    try {
      await update.mutateAsync({
        company_name: String(fd.get("company_name")),
        contact_person: String(fd.get("contact_person") ?? ""),
        email: String(fd.get("email") ?? ""),
        phone: String(fd.get("phone") ?? ""),
        address: String(fd.get("address") ?? ""),
        type,
        status,
      });
      toast.success("Client saved.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Couldn't save the client.");
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Details</CardTitle>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSave} className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="company_name">Company / client name</Label>
            <Input id="company_name" name="company_name" required maxLength={200} defaultValue={c.company_name} disabled={!canEdit} />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div className="grid gap-2">
              <Label htmlFor="contact_person">Contact person</Label>
              <Input id="contact_person" name="contact_person" maxLength={120} defaultValue={c.contact_person ?? ""} disabled={!canEdit} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="type">Type</Label>
              <Select value={type} onValueChange={(v) => setType(v as ClientType)} disabled={!canEdit}>
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
              <Input id="email" name="email" type="email" maxLength={200} defaultValue={c.email ?? ""} disabled={!canEdit} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="phone">Phone</Label>
              <Input id="phone" name="phone" maxLength={50} defaultValue={c.phone ?? ""} disabled={!canEdit} />
            </div>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="address">Address</Label>
            <Textarea id="address" name="address" rows={2} maxLength={500} defaultValue={c.address ?? ""} disabled={!canEdit} />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="status">Status</Label>
            <Select value={status} onValueChange={(v) => setStatus(v as ClientStatus)} disabled={!canEdit}>
              <SelectTrigger id="status">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="active">Active</SelectItem>
                <SelectItem value="inactive">Inactive</SelectItem>
              </SelectContent>
            </Select>
          </div>
          {canEdit && (
            <div className="flex justify-end">
              <Button type="submit" disabled={update.isPending}>
                {update.isPending && <Spinner />} Save changes
              </Button>
            </div>
          )}
        </form>
      </CardContent>
    </Card>
  );
}

function ClientContactsAndProjects({ clientId, client: c, canEdit }: { clientId: string; client: BusinessClient; canEdit: boolean }) {
  const detail = useClient(clientId);
  const addContact = useAddContact(clientId);
  const deleteContact = useDeleteContact(clientId);
  const [addingContact, setAddingContact] = useState(false);
  const [primary, setPrimary] = useState(false);
  const d = detail.data;

  async function onAddContact(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    try {
      await addContact.mutateAsync({
        name: String(fd.get("name")),
        email: String(fd.get("email") ?? ""),
        phone: String(fd.get("phone") ?? ""),
        role: String(fd.get("role") ?? ""),
        is_primary: primary,
      });
      toast.success("Contact added.");
      setAddingContact(false);
      setPrimary(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Couldn't add the contact.");
    }
  }

  const contacts = d?.contacts ?? [];
  const projects = d?.projects ?? [];

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="text-base">Contacts</CardTitle>
          {canEdit && (
            <Button size="sm" variant="outline" onClick={() => setAddingContact(true)}>
              <PlusIcon /> Add contact
            </Button>
          )}
        </CardHeader>
        <CardContent>
          {contacts.length === 0 ? (
            <p className="text-sm text-muted-foreground">No contacts yet.</p>
          ) : (
            <ul className="divide-y">
              {contacts.map((ct) => (
                <li key={ct.id} className="flex items-start justify-between gap-3 py-3 first:pt-0 last:pb-0">
                  <div className="min-w-0">
                    <p className="font-medium">
                      {ct.name}
                      {ct.is_primary && (
                        <Badge className="ml-2 bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200">Primary</Badge>
                      )}
                    </p>
                    <p className="text-sm text-muted-foreground">
                      {[ct.role, ct.email, ct.phone].filter(Boolean).join(" · ") || "—"}
                    </p>
                  </div>
                  {canEdit && (
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={`Remove ${ct.name}`}
                      disabled={deleteContact.isPending}
                      onClick={() =>
                        deleteContact.mutate(ct.id, {
                          onSuccess: () => toast.success("Contact removed."),
                          onError: (err) => toast.error(err.message),
                        })
                      }
                    >
                      <Trash2Icon />
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Linked projects</CardTitle>
        </CardHeader>
        <CardContent>
          {projects.length === 0 ? (
            <p className="text-sm text-muted-foreground">Not linked to any project yet. Link them from a project&apos;s Client tab.</p>
          ) : (
            <ul className="divide-y">
              {projects.map((p) => (
                <li key={p.id} className="py-3 first:pt-0 last:pb-0">
                  <Link href={`/projects/${p.project_id}`} className="font-medium hover:underline">
                    #{p.number} {p.name ?? "Untitled"}
                  </Link>
                  <p className="text-xs text-muted-foreground capitalize">{p.status}</p>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Dialog open={addingContact} onOpenChange={setAddingContact}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Add contact</DialogTitle>
            <DialogDescription>Another person at {c.company_name} we can reach about their work.</DialogDescription>
          </DialogHeader>
          <form onSubmit={onAddContact} className="grid gap-4">
            <div className="grid gap-2">
              <Label htmlFor="name">Name</Label>
              <Input id="name" name="name" required maxLength={120} />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="grid gap-2">
                <Label htmlFor="role">Role</Label>
                <Input id="role" name="role" maxLength={120} placeholder="Decision Maker, Finance, Technical…" />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="email">Email</Label>
                <Input id="email" name="email" type="email" maxLength={200} />
              </div>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="phone">Phone</Label>
              <Input id="phone" name="phone" maxLength={50} />
            </div>
            <div className="flex items-center gap-2">
              <Switch id="is_primary" checked={primary} onCheckedChange={setPrimary} />
              <Label htmlFor="is_primary">Primary contact</Label>
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setAddingContact(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={addContact.isPending}>
                {addContact.isPending && <Spinner />} Add contact
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
