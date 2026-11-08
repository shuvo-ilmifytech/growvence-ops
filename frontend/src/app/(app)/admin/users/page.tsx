"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ChartNoAxesCombinedIcon,
  KeyRoundIcon,
  MoreHorizontalIcon,
  PencilIcon,
  PlusIcon,
  SearchIcon,
  ShieldCheckIcon,
  UserCheckIcon,
  UnplugIcon,
  UserXIcon,
  UsersIcon,
} from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import { initials, relativeTime } from "@/lib/format";
import { useMe } from "@/lib/session";
import { ROLE_LABELS, type Role, type User } from "@/lib/types";
import { PageHeader } from "@/components/page-header";
import { RoleBadge } from "@/components/role-badge";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Textarea } from "@/components/ui/textarea";
import { Field, FieldContent, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

const ROLES = Object.keys(ROLE_LABELS) as Role[];
const USERS_KEY = ["admin", "users"];

type Filter = "all" | Role | "inactive";
const FILTERS: { value: Filter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "sales", label: "Sales" },
  { value: "tl", label: "Team leaders" },
  { value: "member", label: "Members" },
  { value: "hr", label: "HR" },
  { value: "office_admin", label: "Office admins" },
  { value: "client", label: "Clients" },
  { value: "investor", label: "Investors" },
  { value: "ceo", label: "CEO" },
  { value: "inactive", label: "Inactive" },
];

type DialogState =
  | { kind: "create" }
  | { kind: "edit"; user: User }
  | { kind: "reset"; user: User }
  | { kind: "deactivate"; user: User }
  | null;

export default function UsersPage() {
  const { data: me } = useMe();
  const qc = useQueryClient();
  const [filter, setFilter] = useState<Filter>("all");
  const [search, setSearch] = useState("");
  const [dialog, setDialog] = useState<DialogState>(null);
  const [reason, setReason] = useState("");

  const isCEO = me?.role === "ceo";
  // Mirrors the API: the CEO account is only edited by the CEO; HR accounts only by the CEO.
  const canManage = (u: User) => (u.role === "ceo" ? u.id === me?.id : u.role !== "hr" || isCEO);

  const users = useQuery({
    queryKey: USERS_KEY,
    queryFn: () => api<{ users: User[] }>("/admin/users").then((r) => r.users),
  });

  const setActive = useMutation({
    mutationFn: ({ user, active, reason }: { user: User; active: boolean; reason?: string }) =>
      api(`/admin/users/${user.id}`, { method: "PATCH", body: { is_active: active, reason } }),
    onSuccess: (_, { user, active }) => {
      toast.success(active ? `${user.name} reactivated` : `${user.name} deactivated and signed out`);
      setDialog(null);
      qc.invalidateQueries({ queryKey: USERS_KEY });
    },
    onError: (err) => toast.error(err.message),
  });

  const counts = useMemo(() => {
    const c: Record<Filter, number> = { all: 0, sales: 0, tl: 0, member: 0, ceo: 0, hr: 0, office_admin: 0, client: 0, investor: 0, inactive: 0 };
    for (const u of users.data ?? []) {
      if (!u.is_active) c.inactive++;
      else {
        c.all++;
        c[u.role]++;
      }
    }
    return c;
  }, [users.data]);

  const rows = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (users.data ?? []).filter((u) => {
      if (filter === "inactive" ? u.is_active : !u.is_active) return false;
      if (filter !== "all" && filter !== "inactive" && u.role !== filter) return false;
      return !q || u.name.toLowerCase().includes(q) || u.email.toLowerCase().includes(q);
    });
  }, [users.data, filter, search]);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Users"
        description="Invite teammates, set their role and control who can sign in. HR accounts and admin access are managed by the CEO."
        actions={
          <Button onClick={() => setDialog({ kind: "create" })}>
            <PlusIcon /> Add user
          </Button>
        }
      />

      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <Tabs value={filter} onValueChange={(v) => setFilter(v as Filter)}>
          <TabsList className="flex-wrap">
            {FILTERS.map((f) => (
              <TabsTrigger key={f.value} value={f.value}>
                {f.label}
                <span className="ml-1.5 text-xs tabular-nums text-muted-foreground">{counts[f.value]}</span>
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <InputGroup className="md:w-72">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput placeholder="Search name or email" value={search} onChange={(e) => setSearch(e.target.value)} />
        </InputGroup>
      </div>

      <Card className="py-0">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-4">User</TableHead>
              <TableHead>Role</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Last sign-in</TableHead>
              <TableHead className="w-12" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {users.isPending &&
              Array.from({ length: 5 }, (_, i) => (
                <TableRow key={i}>
                  <TableCell className="pl-4">
                    <div className="flex items-center gap-3">
                      <Skeleton className="size-9 rounded-full" />
                      <div className="space-y-1.5">
                        <Skeleton className="h-3.5 w-32" />
                        <Skeleton className="h-3 w-44" />
                      </div>
                    </div>
                  </TableCell>
                  <TableCell><Skeleton className="h-5 w-24 rounded-full" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                  <TableCell />
                </TableRow>
              ))}
            {rows.map((u) => (
              <UserRow
                key={u.id}
                user={u}
                isSelf={u.id === me?.id}
                manageable={canManage(u)}
                onEdit={() => setDialog({ kind: "edit", user: u })}
                onReset={() => setDialog({ kind: "reset", user: u })}
                onDeactivate={() => {
                  setReason("");
                  setDialog({ kind: "deactivate", user: u });
                }}
                onReactivate={() => setActive.mutate({ user: u, active: true })}
              />
            ))}
          </TableBody>
        </Table>
        {users.isSuccess && rows.length === 0 && (
          <Empty className="border-t">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <UsersIcon />
              </EmptyMedia>
              <EmptyTitle>No users found</EmptyTitle>
              <EmptyDescription>
                {search ? "Try a different name or email." : "Nobody matches this filter yet."}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        {users.isError && <p className="border-t p-4 text-sm text-destructive">{users.error.message}</p>}
      </Card>

      <UserDialog
        open={dialog?.kind === "create" || dialog?.kind === "edit"}
        user={dialog?.kind === "edit" ? dialog.user : undefined}
        isSelf={dialog?.kind === "edit" && dialog.user.id === me?.id}
        isCEO={isCEO}
        onOpenChange={(o) => !o && setDialog(null)}
      />
      <ResetPasswordDialog
        user={dialog?.kind === "reset" ? dialog.user : undefined}
        onOpenChange={(o) => !o && setDialog(null)}
      />
      <AlertDialog open={dialog?.kind === "deactivate"} onOpenChange={(o) => !o && setDialog(null)}>
        {dialog?.kind === "deactivate" && (
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Deactivate {dialog.user.name}?</AlertDialogTitle>
              <AlertDialogDescription>
                They will be signed out on every device and won&apos;t be able to sign in. Their projects, updates and
                history stay intact, and you can reactivate them at any time. Anyone still holding open projects, a team or
                direct reports has to hand them over first.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <Field>
              <FieldLabel htmlFor="deactivate-reason">Reason</FieldLabel>
              <Textarea
                id="deactivate-reason"
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                placeholder="e.g. Left the company"
                maxLength={500}
                rows={2}
              />
              <FieldDescription>Kept in the access log with who did it and when.</FieldDescription>
            </Field>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction
                className="bg-destructive text-white hover:bg-destructive/90"
                disabled={setActive.isPending || !reason.trim()}
                onClick={(e) => {
                  e.preventDefault();
                  setActive.mutate({ user: dialog.user, active: false, reason: reason.trim() });
                }}
              >
                Deactivate
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        )}
      </AlertDialog>
    </div>
  );
}

function UserRow({
  user,
  isSelf,
  manageable,
  onEdit,
  onReset,
  onDeactivate,
  onReactivate,
}: {
  user: User;
  isSelf: boolean;
  manageable: boolean;
  onEdit: () => void;
  onReset: () => void;
  onDeactivate: () => void;
  onReactivate: () => void;
}) {
  return (
    <TableRow className={user.is_active ? "" : "opacity-60"}>
      <TableCell className="pl-4">
        <div className="flex items-center gap-3">
          <Avatar className="size-9">
            <AvatarFallback className="bg-accent text-xs font-medium text-accent-foreground">
              {initials(user.name)}
            </AvatarFallback>
          </Avatar>
          <div className="min-w-0">
            <div className="flex items-center gap-1.5 font-medium">
              {user.name}
              {isSelf && <span className="text-xs font-normal text-muted-foreground">(you)</span>}
            </div>
            <div className="truncate text-xs text-muted-foreground">{user.email}</div>
          </div>
        </div>
      </TableCell>
      <TableCell>
        <div className="flex items-center gap-1.5">
          <RoleBadge role={user.role} />
          {user.is_admin && (
            <Badge variant="outline" className="gap-1">
              <ShieldCheckIcon className="size-3" /> Admin
            </Badge>
          )}
          {user.can_view_analytics && user.role !== "ceo" && (
            <Badge variant="outline" className="gap-1">
              <ChartNoAxesCombinedIcon className="size-3" /> Analytics
            </Badge>
          )}
        </div>
      </TableCell>
      <TableCell>
        <span className="inline-flex items-center gap-1.5 text-sm">
          <span className={`size-1.5 rounded-full ${user.is_active ? "bg-emerald-500" : "bg-muted-foreground"}`} />
          {user.is_active ? "Active" : "Inactive"}
        </span>
      </TableCell>
      <TableCell className="text-sm text-muted-foreground" title={user.last_login_at ?? undefined}>
        {relativeTime(user.last_login_at)}
      </TableCell>
      <TableCell className="pr-4 text-right">
        {manageable && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon-sm" aria-label={`Actions for ${user.name}`}>
                <MoreHorizontalIcon />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuItem onSelect={onEdit}>
                <PencilIcon /> Edit details
              </DropdownMenuItem>
              {user.role !== "ceo" && (
                <DropdownMenuItem onSelect={onReset}>
                  <KeyRoundIcon /> Reset password
                </DropdownMenuItem>
              )}
              {user.role !== "hr" && (
                <DropdownMenuItem
                  onSelect={() =>
                    api<{ revoked: number }>(`/admin/users/${user.id}/assistant-tokens`, { method: "DELETE" })
                      .then((r) => toast.success(r.revoked ? `Disconnected ${r.revoked} AI assistant${r.revoked === 1 ? "" : "s"}` : "No AI assistants were connected"))
                      .catch((err) => toast.error(err instanceof Error ? err.message : "Could not disconnect"))
                  }
                >
                  <UnplugIcon /> Disconnect AI assistants
                </DropdownMenuItem>
              )}
              {!isSelf && (
                <>
                  <DropdownMenuSeparator />
                  {user.is_active ? (
                    <DropdownMenuItem variant="destructive" onSelect={onDeactivate}>
                      <UserXIcon /> Deactivate
                    </DropdownMenuItem>
                  ) : (
                    <DropdownMenuItem onSelect={onReactivate}>
                      <UserCheckIcon /> Reactivate
                    </DropdownMenuItem>
                  )}
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </TableCell>
    </TableRow>
  );
}

function UserDialog({
  open,
  user,
  isSelf,
  isCEO,
  onOpenChange,
}: {
  open: boolean;
  user?: User;
  isSelf: boolean;
  isCEO: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        {/* Keyed so the form resets between users */}
        {open && <UserForm key={user?.id ?? "new"} user={user} isSelf={isSelf} isCEO={isCEO} onDone={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  );
}

function UserForm({ user, isSelf, isCEO, onDone }: { user?: User; isSelf: boolean; isCEO: boolean; onDone: () => void }) {
  // Nobody becomes CEO here (opsctl transfer-ceo); only the CEO hands out the HR role.
  const roles = ROLES.filter((r) => (r !== "ceo" || user?.role === "ceo") && (r !== "hr" || isCEO || user?.role === "hr"));
  const qc = useQueryClient();
  const [role, setRole] = useState<Role>(user?.role ?? "member");
  const [isAdmin, setIsAdmin] = useState(user?.is_admin ?? false);
  const [analytics, setAnalytics] = useState(user?.can_view_analytics ?? false);
  const [fields, setFields] = useState<Record<string, string>>({});
  const [pending, setPending] = useState(false);

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const d = new FormData(e.currentTarget);
    const name = String(d.get("name")).trim();
    const email = String(d.get("email")).trim();
    setFields({});
    setPending(true);
    try {
      if (user) {
        const patch: Record<string, unknown> = {};
        if (name !== user.name) patch.name = name;
        if (email !== user.email) patch.email = email;
        if (role !== user.role) patch.role = role;
        if (isAdmin !== user.is_admin) patch.is_admin = isAdmin;
        if (analytics !== user.can_view_analytics) patch.can_view_analytics = analytics;
        if (Object.keys(patch).length) await api(`/admin/users/${user.id}`, { method: "PATCH", body: patch });
        toast.success("Changes saved");
      } else {
        await api("/admin/users", {
          method: "POST",
          body: { name, email, role, is_admin: isAdmin, can_view_analytics: analytics, password: d.get("password") },
        });
        toast.success(`${name} added`, { description: "Share the initial password with them securely." });
      }
      qc.invalidateQueries({ queryKey: USERS_KEY });
      onDone();
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length) setFields(err.fields);
      else toast.error(err instanceof Error ? err.message : "Could not save");
    } finally {
      setPending(false);
    }
  }

  return (
    <form onSubmit={onSubmit} className="grid gap-6">
      <DialogHeader>
        <DialogTitle>{user ? "Edit user" : "Add user"}</DialogTitle>
        <DialogDescription>
          {user ? "Role changes apply on their next click; no sign-out needed." : "They can change their password after signing in."}
        </DialogDescription>
      </DialogHeader>
      <FieldGroup>
        <Field data-invalid={!!fields.name}>
          <FieldLabel htmlFor="name">Full name</FieldLabel>
          <Input id="name" name="name" defaultValue={user?.name} required autoFocus aria-invalid={!!fields.name} />
          <FieldError>{fields.name && `Name ${fields.name}`}</FieldError>
        </Field>
        <Field data-invalid={!!fields.email}>
          <FieldLabel htmlFor="email">Email</FieldLabel>
          <Input id="email" name="email" type="email" defaultValue={user?.email} required aria-invalid={!!fields.email} />
          <FieldError>{fields.email && `Email ${fields.email}`}</FieldError>
        </Field>
        <Field>
          <FieldLabel>Role</FieldLabel>
          <Select value={role} onValueChange={(v) => setRole(v as Role)} disabled={isSelf}>
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {roles.map((r) => (
                <SelectItem key={r} value={r}>
                  {ROLE_LABELS[r]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        {!user && (
          <Field data-invalid={!!fields.password}>
            <FieldLabel htmlFor="password">Initial password</FieldLabel>
            <Input id="password" name="password" type="text" minLength={10} required autoComplete="off" aria-invalid={!!fields.password} />
            <FieldDescription>At least 10 characters.</FieldDescription>
            <FieldError>{fields.password && `Password ${fields.password}`}</FieldError>
          </Field>
        )}
        {isCEO && (
          <Field orientation="horizontal" className="rounded-lg border p-3">
            <FieldContent>
              <FieldLabel htmlFor="is_admin">Admin access</FieldLabel>
              <FieldDescription>
                {isSelf ? "You can't change your own access." : "System setup: marketplace profiles, project types and settings."}
              </FieldDescription>
            </FieldContent>
            <Switch id="is_admin" checked={isAdmin} onCheckedChange={setIsAdmin} disabled={isSelf} />
          </Field>
        )}
        {isCEO && user?.role !== "ceo" && (
          <Field orientation="horizontal" className="rounded-lg border p-3">
            <FieldContent>
              <FieldLabel htmlFor="can_view_analytics">Analytics access</FieldLabel>
              <FieldDescription>
                The Analytics page: work coming in and delivered, and every team&apos;s and person&apos;s performance. Counts only, no money.
              </FieldDescription>
            </FieldContent>
            <Switch id="can_view_analytics" checked={analytics} onCheckedChange={setAnalytics} disabled={isSelf} />
          </Field>
        )}
      </FieldGroup>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">Cancel</Button>
        </DialogClose>
        <Button type="submit" disabled={pending}>
          {user ? "Save changes" : "Add user"}
        </Button>
      </DialogFooter>
    </form>
  );
}

function ResetPasswordDialog({ user, onOpenChange }: { user?: User; onOpenChange: (open: boolean) => void }) {
  const [error, setError] = useState("");
  const reset = useMutation({
    mutationFn: (password: string) =>
      api(`/admin/users/${user!.id}/reset-password`, { method: "POST", body: { password } }),
    onSuccess: () => {
      toast.success(`Password reset for ${user!.name}`, { description: "They've been signed out everywhere." });
      onOpenChange(false);
    },
    onError: (err) => setError(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : err.message),
  });

  return (
    <Dialog
      open={!!user}
      onOpenChange={(o) => {
        setError("");
        onOpenChange(o);
      }}
    >
      <DialogContent className="sm:max-w-md">
        {user && (
          <form
            className="grid gap-6"
            onSubmit={(e) => {
              e.preventDefault();
              setError("");
              reset.mutate(String(new FormData(e.currentTarget).get("password")));
            }}
          >
            <DialogHeader>
              <DialogTitle>Reset password</DialogTitle>
              <DialogDescription>
                Set a new password for {user.name}. They&apos;ll be signed out on every device.
              </DialogDescription>
            </DialogHeader>
            <Field data-invalid={!!error}>
              <FieldLabel htmlFor="new-password">New password</FieldLabel>
              <Input id="new-password" name="password" type="text" minLength={10} required autoFocus autoComplete="off" />
              <FieldDescription>At least 10 characters.</FieldDescription>
              <FieldError>{error && `Password ${error}`}</FieldError>
            </Field>
            <DialogFooter>
              <DialogClose asChild>
                <Button type="button" variant="outline">Cancel</Button>
              </DialogClose>
              <Button type="submit" disabled={reset.isPending}>Reset password</Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
