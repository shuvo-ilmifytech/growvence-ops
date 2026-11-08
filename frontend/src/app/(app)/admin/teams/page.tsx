"use client";

import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PencilIcon, PlusIcon, UserMinusIcon, UsersIcon } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import { errorText, useDepartments } from "@/lib/hr";
import { TEAMS_KEY, useDirectoryByRole, useTeam, useTeams } from "@/lib/teams";
import type { Team } from "@/lib/types";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

const NO_LEAD = "none";
const NO_DEPT = "none";

function useTeamAction<T>(fn: (v: T) => Promise<unknown>, success: (v: T) => string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: (_, v) => {
      toast.success(success(v));
      qc.invalidateQueries({ queryKey: TEAMS_KEY });
      qc.invalidateQueries({ queryKey: ["directory"] });
    },
    onError: (err) => toast.error(errorText(err)),
  });
}

/** Teams: HR and the CEO set each team's lead and members. Projects reach a team through its project types. */
export default function TeamsPage() {
  const teams = useTeams(true);
  const [editing, setEditing] = useState<Team | "new" | null>(null);
  const [members, setMembers] = useState<Team | null>(null);
  const toggle = useTeamAction(
    ({ id, is_active }: { id: string; is_active: boolean }) => api(`/admin/teams/${id}`, { method: "PATCH", body: { is_active } }),
    ({ is_active }) => (is_active ? "Team reactivated" : "Team retired"),
  );
  const rows = teams.data ?? [];

  return (
    <div className="max-w-4xl space-y-6">
      <PageHeader
        title="Teams"
        description="Each team has a Team Leader and its members, and owns the projects of its project types (set under Profiles & types). A Team Leader sees and assigns only their own team's projects."
      />
      <Card className="pb-0">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <UsersIcon className="size-4 text-primary" /> Teams
          </CardTitle>
          <CardDescription>
            Projects whose type has no team, or whose team has no active lead, stay visible to every Team Leader.
          </CardDescription>
          <CardAction>
            <Button size="sm" onClick={() => setEditing("new")}>
              <PlusIcon /> New team
            </Button>
          </CardAction>
        </CardHeader>
        {teams.isPending ? (
          <div className="space-y-2 border-t p-6">
            <Skeleton className="h-8" />
            <Skeleton className="h-8" />
          </div>
        ) : rows.length === 0 ? (
          <p className="border-t px-6 py-4 text-sm text-muted-foreground">No teams yet. Every Team Leader sees every project.</p>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="pl-6">Team</TableHead>
                  <TableHead>Team Leader</TableHead>
                  <TableHead className="hidden sm:table-cell">Department</TableHead>
                  <TableHead className="hidden md:table-cell">Project types</TableHead>
                  <TableHead className="text-right">Members</TableHead>
                  <TableHead>Active</TableHead>
                  <TableHead className="w-12 pr-6" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((t) => (
                  <TableRow key={t.id} className={t.is_active ? "" : "opacity-60"}>
                    <TableCell className="pl-6 font-medium">{t.name}</TableCell>
                    <TableCell>{t.lead_name ?? <span className="text-muted-foreground">No lead</span>}</TableCell>
                    <TableCell className="hidden sm:table-cell">
                      {t.department_name ?? <span className="text-muted-foreground">None</span>}
                    </TableCell>
                    <TableCell className="hidden md:table-cell">
                      <div className="flex flex-wrap gap-1">
                        {t.project_types.length === 0 && <span className="text-sm text-muted-foreground">None</span>}
                        {t.project_types.map((name) => (
                          <Badge key={name} variant="secondary">
                            {name}
                          </Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      <Button variant="link" className="h-auto p-0 tabular-nums" onClick={() => setMembers(t)}>
                        {t.member_count}
                      </Button>
                    </TableCell>
                    <TableCell>
                      <Switch
                        checked={t.is_active}
                        aria-label={`${t.name} active`}
                        disabled={toggle.isPending}
                        onCheckedChange={(v) => toggle.mutate({ id: t.id, is_active: v })}
                      />
                    </TableCell>
                    <TableCell className="pr-6 text-right">
                      <Button variant="ghost" size="icon-sm" aria-label={`Edit ${t.name}`} onClick={() => setEditing(t)}>
                        <PencilIcon />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </Card>
      <Dialog open={!!editing} onOpenChange={(o) => !o && setEditing(null)}>
        <DialogContent className="sm:max-w-md">
          {editing && (
            <TeamForm key={editing === "new" ? "new" : editing.id} team={editing === "new" ? undefined : editing} onDone={() => setEditing(null)} />
          )}
        </DialogContent>
      </Dialog>
      <MembersDialog team={members} teams={rows} onOpenChange={(o) => !o && setMembers(null)} />
    </div>
  );
}

function TeamForm({ team, onDone }: { team?: Team; onDone: () => void }) {
  const qc = useQueryClient();
  const leads = useDirectoryByRole("tl");
  const departments = useDepartments();
  const [lead, setLead] = useState(team?.lead_id ?? NO_LEAD);
  const [dept, setDept] = useState(team?.department_id ?? NO_DEPT);
  // Keep a since-retired department selectable so the form shows it.
  const deptOptions = [...(departments.data ?? [])];
  if (team?.department_id && team.department_name && !deptOptions.some((d) => d.id === team.department_id)) {
    deptOptions.push({ id: team.department_id, name: `${team.department_name} (retired)`, is_active: false });
  }
  const [fields, setFields] = useState<Record<string, string>>({});
  const [pending, setPending] = useState(false);
  // Keep a since-deactivated lead selectable so the form shows who it is.
  const options = [...(leads.data ?? [])];
  if (team?.lead_id && team.lead_name && !options.some((u) => u.id === team.lead_id)) {
    options.push({ id: team.lead_id, name: `${team.lead_name} (inactive)`, role: "tl", team_id: null });
  }

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const name = String(new FormData(e.currentTarget).get("name")).trim();
    setFields({});
    setPending(true);
    try {
      await api(team ? `/admin/teams/${team.id}` : "/admin/teams", {
        method: team ? "PATCH" : "POST",
        body: { name, lead_id: lead === NO_LEAD ? null : lead, department_id: dept === NO_DEPT ? null : dept },
      });
      toast.success(team ? "Team saved" : `${name} team created`);
      qc.invalidateQueries({ queryKey: TEAMS_KEY });
      onDone();
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length) setFields(err.fields);
      else toast.error(errorText(err));
    } finally {
      setPending(false);
    }
  }

  return (
    <form onSubmit={onSubmit} className="grid gap-6">
      <DialogHeader>
        <DialogTitle>{team ? `Edit ${team.name}` : "New team"}</DialogTitle>
        <DialogDescription>The Team Leader gets this team&apos;s projects and assigns them to its members.</DialogDescription>
      </DialogHeader>
      <FieldGroup>
        <Field data-invalid={!!fields.name}>
          <FieldLabel htmlFor="team-name">Name</FieldLabel>
          <Input id="team-name" name="name" defaultValue={team?.name} placeholder="e.g. Shopify" maxLength={120} required autoFocus />
          <FieldError>{fields.name && `Name ${fields.name}`}</FieldError>
        </Field>
        <Field data-invalid={!!fields.lead_id}>
          <FieldLabel>Team Leader</FieldLabel>
          <Select value={lead} onValueChange={setLead}>
            <SelectTrigger className="w-full">
              <SelectValue placeholder={leads.isPending ? "Loading…" : "Choose a Team Leader"} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={NO_LEAD}>No lead yet</SelectItem>
              {options.map((u) => (
                <SelectItem key={u.id} value={u.id}>
                  {u.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <FieldDescription>Without a lead, the team&apos;s projects are open to every Team Leader.</FieldDescription>
          <FieldError>{fields.lead_id && `Team Leader ${fields.lead_id}`}</FieldError>
        </Field>
        <Field data-invalid={!!fields.department_id}>
          <FieldLabel>Department</FieldLabel>
          <Select value={dept} onValueChange={setDept}>
            <SelectTrigger className="w-full">
              <SelectValue placeholder={departments.isPending ? "Loading…" : "Choose a department"} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={NO_DEPT}>No department</SelectItem>
              {deptOptions.map((d) => (
                <SelectItem key={d.id} value={d.id}>
                  {d.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <FieldDescription>Departments are managed in HR settings.</FieldDescription>
          <FieldError>{fields.department_id && `Department ${fields.department_id}`}</FieldError>
        </Field>
      </FieldGroup>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            Cancel
          </Button>
        </DialogClose>
        <Button type="submit" disabled={pending}>
          {pending && <Spinner />}
          {team ? "Save" : "Create team"}
        </Button>
      </DialogFooter>
    </form>
  );
}

function MembersDialog({ team, teams, onOpenChange }: { team: Team | null; teams: Team[]; onOpenChange: (o: boolean) => void }) {
  const detail = useTeam(team?.id ?? null);
  const people = useDirectoryByRole("member");
  const [adding, setAdding] = useState("");
  const add = useTeamAction(
    (userId: string) => api(`/admin/teams/${team!.id}/members/${userId}`, { method: "PUT" }),
    () => "Member added",
  );
  const remove = useTeamAction(
    (userId: string) => api(`/admin/teams/${team!.id}/members/${userId}`, { method: "DELETE" }),
    () => "Member removed",
  );
  const teamName = (id: string | null) => teams.find((t) => t.id === id)?.name;
  const current = detail.data?.members ?? [];
  const candidates = (people.data ?? []).filter((u) => u.team_id !== team?.id);

  return (
    <Dialog open={!!team} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{team?.name} members</DialogTitle>
          <DialogDescription>
            A member is in one team at a time; adding someone from another team moves them. Work already assigned to them stays with them.
          </DialogDescription>
        </DialogHeader>
        {team?.is_active && (
          <div className="flex gap-2">
            <Select value={adding} onValueChange={setAdding}>
              <SelectTrigger className="w-full">
                <SelectValue placeholder={people.isPending ? "Loading…" : "Add a member"} />
              </SelectTrigger>
              <SelectContent>
                {candidates.map((u) => (
                  <SelectItem key={u.id} value={u.id}>
                    {u.name}
                    {u.team_id && <span className="text-muted-foreground"> · from {teamName(u.team_id) ?? "another team"}</span>}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              variant="outline"
              disabled={!adding || add.isPending}
              onClick={() => add.mutate(adding, { onSuccess: () => setAdding("") })}
            >
              <PlusIcon /> Add
            </Button>
          </div>
        )}
        <div className="divide-y rounded-lg border">
          {detail.isPending && <Skeleton className="m-3 h-5" />}
          {!detail.isPending && current.length === 0 && <p className="p-3 text-sm text-muted-foreground">No members yet.</p>}
          {current.map((m) => (
            <div key={m.id} className={`flex items-center justify-between gap-3 px-3 py-2 ${m.is_active ? "" : "opacity-60"}`}>
              <span className="text-sm font-medium">
                {m.name}
                {!m.is_active && <span className="font-normal text-muted-foreground"> (inactive)</span>}
              </span>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`Remove ${m.name}`}
                disabled={remove.isPending}
                onClick={() => remove.mutate(m.id)}
              >
                <UserMinusIcon />
              </Button>
            </div>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
