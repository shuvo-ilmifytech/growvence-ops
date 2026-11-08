"use client";

import Link from "next/link";
import { useMemo } from "react";
import { Building2Icon, CrownIcon, UsersRoundIcon } from "lucide-react";
import { initials } from "@/lib/format";
import { useDepartments, useDirectory, type DirectoryEmployee } from "@/lib/hr";
import { useTeams, useUserDirectory } from "@/lib/teams";
import type { Team } from "@/lib/types";
import { useMe } from "@/lib/session";
import { PageHeader } from "@/components/page-header";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { RoleBadge } from "@/components/role-badge";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

const NONE = "none";

/** Org chart: departments, then teams, then people, plus the reporting line as a tree. */
export default function OrgChartPage() {
  const { data: me } = useMe();
  const people = useDirectory();
  const departments = useDepartments();
  const teams = useTeams();
  const users = useUserDirectory();
  const loading = !people.data || !departments.data || !teams.data || !users.data;
  const teamOf = useMemo(() => new Map((users.data ?? []).map((u) => [u.id, u.team_id])), [users.data]);

  // UX only: the same names and reporting lines are in the People directory.
  if (me && !["ceo", "hr", "tl"].includes(me.role)) {
    return (
      <Empty className="min-h-[60vh]">
        <EmptyHeader>
          <EmptyTitle>For Team Leaders, HR and the CEO</EmptyTitle>
          <EmptyDescription>Your dashboard shows who you report to.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Org chart"
        description="Departments, their teams and people, and who reports to whom. Departments and reporting lines are set on each person's HR record; teams under Admin → Teams."
      />
      {loading ? (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Skeleton className="h-48" />
          <Skeleton className="h-48" />
        </div>
      ) : (
        <Tabs defaultValue="departments">
          <TabsList>
            <TabsTrigger value="departments">By department</TabsTrigger>
            <TabsTrigger value="reporting">Reporting line</TabsTrigger>
          </TabsList>
          <TabsContent value="departments" className="mt-4">
            <ByDepartment
              people={people.data!}
              departments={departments.data!}
              teams={teams.data!}
              teamOf={teamOf}
            />
          </TabsContent>
          <TabsContent value="reporting" className="mt-4">
            <ReportingLine people={people.data!} />
          </TabsContent>
        </Tabs>
      )}
    </div>
  );
}

function ByDepartment({
  people,
  departments,
  teams,
  teamOf,
}: {
  people: DirectoryEmployee[];
  departments: { id: string; name: string; head_id?: string | null }[];
  teams: Team[];
  teamOf: Map<string, string | null>;
}) {
  const ceo = people.filter((p) => p.role === "ceo");
  const byId = useMemo(() => new Map(people.map((p) => [p.user_id, p])), [people]);
  const groups = useMemo(() => {
    const rest = people.filter((p) => p.role !== "ceo");
    const known = (id: string | null) => !!id && departments.some((d) => d.id === id);
    const teamPeople = (t: Team) => [
      ...(t.lead_id && byId.has(t.lead_id) ? [t.lead_id] : []),
      ...people.filter((p) => teamOf.get(p.user_id) === t.id).map((p) => p.user_id),
    ];
    const heads = departments.flatMap((d) => (d.head_id && byId.has(d.head_id) ? [d.head_id] : []));
    // Someone in a team that has a department, or heading one, is shown there, even if their own record has none.
    const placedByTeam = new Set([...teams.filter((t) => known(t.department_id)).flatMap(teamPeople), ...heads]);
    const group = (id: string, name: string, head: DirectoryEmployee | undefined, groupTeams: Team[], own: DirectoryEmployee[]) => {
      const inTeams = new Set([...groupTeams.flatMap(teamPeople), ...(head ? [head.user_id] : [])]);
      return {
        id,
        name,
        head,
        teams: groupTeams,
        others: own.filter((p) => !inTeams.has(p.user_id)),
        count: new Set([...inTeams, ...own.map((p) => p.user_id)]).size,
      };
    };
    const out = departments.map((d) =>
      group(
        d.id,
        d.name,
        d.head_id ? byId.get(d.head_id) : undefined,
        teams.filter((t) => t.department_id === d.id),
        rest.filter((p) => p.department_id === d.id),
      ),
    );
    const unplaced = group(
      NONE,
      "No department",
      undefined,
      teams.filter((t) => !known(t.department_id)),
      rest.filter((p) => !known(p.department_id) && !placedByTeam.has(p.user_id)),
    );
    if (unplaced.count || unplaced.teams.length) out.push(unplaced);
    return out;
  }, [people, departments, teams, teamOf, byId]);

  return (
    <div className="space-y-6">
      {ceo.length > 0 && (
        <Card className="border-violet-200 dark:border-violet-900">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <CrownIcon className="size-4 text-violet-600" /> Leadership
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-wrap gap-2">
            {ceo.map((p) => (
              <Person key={p.user_id} person={p} />
            ))}
          </CardContent>
        </Card>
      )}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        {groups.map((g) => {
          const others = g.others;
          return (
            <Card key={g.id} className={g.id === NONE ? "border-dashed" : ""}>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Building2Icon className="size-4 text-primary" /> {g.name}
                  <Badge variant="secondary" className="ml-auto tabular-nums">
                    {g.count} {g.count === 1 ? "person" : "people"}
                  </Badge>
                </CardTitle>
                {g.teams.length > 0 && (
                  <CardDescription>
                    {g.teams.length} {g.teams.length === 1 ? "team" : "teams"}
                  </CardDescription>
                )}
              </CardHeader>
              <CardContent className="space-y-4">
                {g.head && (
                  <div className="flex flex-wrap gap-2">
                    <Person person={g.head} note="Head" />
                  </div>
                )}
                {g.teams.map((t) => {
                  const lead = t.lead_id ? byId.get(t.lead_id) : undefined;
                  const members = people.filter((p) => teamOf.get(p.user_id) === t.id);
                  return (
                    <div key={t.id} className="rounded-lg border p-3">
                      <div className="mb-2 flex items-center gap-2 text-sm font-medium">
                        <UsersRoundIcon className="size-4 text-muted-foreground" /> {t.name} team
                        {t.project_types.length > 0 && (
                          <span className="truncate text-xs font-normal text-muted-foreground">· {t.project_types.join(", ")}</span>
                        )}
                      </div>
                      <div className="flex flex-wrap gap-2">
                        {lead ? (
                          <Person person={lead} note="Lead" />
                        ) : (
                          <span className="text-xs text-muted-foreground">No lead</span>
                        )}
                        {members.map((p) => (
                          <Person key={p.user_id} person={p} />
                        ))}
                        {members.length === 0 && <span className="self-center text-xs text-muted-foreground">No members yet</span>}
                      </div>
                    </div>
                  );
                })}
                {others.length > 0 && (
                  <div className="flex flex-wrap gap-2">
                    {others.map((p) => (
                      <Person key={p.user_id} person={p} />
                    ))}
                  </div>
                )}
                {g.count === 0 && g.teams.length === 0 && <p className="text-sm text-muted-foreground">Nobody here yet.</p>}
              </CardContent>
            </Card>
          );
        })}
      </div>
    </div>
  );
}

/** The reporting line as a tree: everyone with no (active) manager at the top. */
function ReportingLine({ people }: { people: DirectoryEmployee[] }) {
  const ids = new Set(people.map((p) => p.user_id));
  const children = new Map<string, DirectoryEmployee[]>();
  const roots: DirectoryEmployee[] = [];
  for (const p of people) {
    if (p.manager_id && ids.has(p.manager_id) && p.manager_id !== p.user_id) {
      children.set(p.manager_id, [...(children.get(p.manager_id) ?? []), p]);
    } else {
      roots.push(p);
    }
  }
  // CEO first, then by name.
  const order = (a: DirectoryEmployee, b: DirectoryEmployee) => Number(b.role === "ceo") - Number(a.role === "ceo") || a.name.localeCompare(b.name);
  const render = (p: DirectoryEmployee, seen: Set<string>): React.ReactNode => {
    if (seen.has(p.user_id)) return null; // guards a reporting loop
    const next = new Set(seen).add(p.user_id);
    const kids = (children.get(p.user_id) ?? []).sort(order);
    return (
      <li key={p.user_id}>
        <Person person={p} showDept />
        {kids.length > 0 && <ul className="mt-2 ml-4 space-y-2 border-l pl-4">{kids.map((k) => render(k, next))}</ul>}
      </li>
    );
  };
  return (
    <Card>
      <CardContent>
        <ul className="space-y-3">{roots.sort(order).map((p) => render(p, new Set()))}</ul>
      </CardContent>
    </Card>
  );
}

function Person({ person: p, note, showDept }: { person: DirectoryEmployee; note?: string; showDept?: boolean }) {
  return (
    <Link
      href={`/hr/people/${p.user_id}`}
      className="inline-flex max-w-full items-center gap-2 rounded-lg border bg-card px-2 py-1.5 text-sm hover:bg-muted/60"
    >
      <Avatar className="size-6">
        <AvatarFallback className="bg-accent text-[10px] font-medium text-accent-foreground">{initials(p.name)}</AvatarFallback>
      </Avatar>
      <span className="min-w-0">
        <span className="block truncate font-medium">
          {p.name}
          {note && <span className="font-normal text-muted-foreground"> · {note}</span>}
        </span>
        <span className="block truncate text-xs text-muted-foreground">
          {[p.designation, showDept ? p.department_name : null].filter(Boolean).join(" · ") || <RoleBadge role={p.role} className="h-4 px-1 text-[10px]" />}
        </span>
      </span>
    </Link>
  );
}
