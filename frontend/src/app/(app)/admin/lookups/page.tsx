"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LayersIcon, PlusIcon, StoreIcon, type LucideIcon } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import { useTeams } from "@/lib/teams";
import type { Profile, ProjectType, Team } from "@/lib/types";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Field, FieldError } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";

// fee: a marketplace profile's share of each order, in percent.
type Item = { id: string; name: string; is_active: boolean; sub?: string; team_id?: string | null; fee?: number | string };

const NO_TEAM = "none";

export default function LookupsPage() {
  const qc = useQueryClient();

  const profiles = useQuery({
    queryKey: ["admin", "profiles"],
    queryFn: () => api<{ profiles: Profile[] }>("/admin/profiles").then((r) => r.profiles),
  });
  const types = useQuery({
    queryKey: ["admin", "project-types"],
    queryFn: () => api<{ project_types: ProjectType[] }>("/admin/project-types").then((r) => r.project_types),
  });
  const teams = useTeams();

  return (
    <div className="space-y-6">
      <PageHeader
        title="Profiles & project types"
        description="These fill the dropdowns on the project form. Turn entries off instead of deleting them so older projects keep their labels."
      />
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <LookupCard
          title="Marketplace profiles"
          description="The seller profiles your orders come through, and the share each marketplace keeps (Fiverr: 20%). A project's fee and what you receive follow its profile automatically."
          icon={StoreIcon}
          noun="profile"
          items={profiles.data?.map((p) => ({ ...p, sub: p.platform, fee: p.fee_percent }))}
          loading={profiles.isPending}
          withPlatform
          endpoint="/admin/profiles"
          onChanged={() => qc.invalidateQueries({ queryKey: ["admin", "profiles"] })}
        />
        <LookupCard
          title="Project types"
          description="Each type goes to one team: a submitted project lands with its type's team (e.g. every Shopify Store project with the Shopify team). Types without a team are open to every Team Leader."
          icon={LayersIcon}
          noun="project type"
          items={types.data}
          loading={types.isPending}
          teams={teams.data}
          endpoint="/admin/project-types"
          onChanged={() => {
            qc.invalidateQueries({ queryKey: ["admin", "project-types"] });
            qc.invalidateQueries({ queryKey: ["teams"] });
          }}
        />
      </div>
    </div>
  );
}

function LookupCard({
  title,
  description,
  icon: Icon,
  noun,
  items,
  loading,
  withPlatform,
  teams,
  endpoint,
  onChanged,
}: {
  title: string;
  description: string;
  icon: LucideIcon;
  noun: string;
  items?: Item[];
  loading: boolean;
  withPlatform?: boolean;
  teams?: Team[]; // set to route each item to a team
  endpoint: string;
  onChanged: () => void;
}) {
  const [error, setError] = useState("");
  const [newTeam, setNewTeam] = useState(NO_TEAM);

  const setTeam = useMutation({
    mutationFn: ({ item, teamId }: { item: Item; teamId: string | null }) =>
      api(`${endpoint}/${item.id}`, { method: "PATCH", body: { team_id: teamId } }),
    onSuccess: (_, { item, teamId }) => {
      const team = teams?.find((t) => t.id === teamId)?.name;
      toast.success(team ? `${item.name} projects now go to ${team}` : `${item.name} no longer goes to a team`);
      onChanged();
    },
    onError: (err) => toast.error(err.message),
  });

  const setFee = useMutation({
    mutationFn: ({ item, fee }: { item: Item; fee: number }) => api(`${endpoint}/${item.id}`, { method: "PATCH", body: { fee_percent: fee } }),
    onSuccess: (_, { item, fee }) => {
      toast.success(`${item.name} now keeps ${fee}% (new projects and profile changes)`);
      onChanged();
    },
    onError: (err) => toast.error(err instanceof ApiError && err.fields.fee_percent ? `Fee ${err.fields.fee_percent}` : err.message),
  });

  const toggle = useMutation({
    mutationFn: (item: Item) => api(`${endpoint}/${item.id}`, { method: "PATCH", body: { is_active: !item.is_active } }),
    onSuccess: (_, item) => {
      toast.success(`${item.name} ${item.is_active ? "turned off" : "turned on"}`);
      onChanged();
    },
    onError: (err) => toast.error(err.message),
  });

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget;
    const d = new FormData(form);
    setError("");
    try {
      const body: Record<string, unknown> = { name: d.get("name") };
      if (withPlatform) body.platform = d.get("platform");
      if (teams && newTeam !== NO_TEAM) body.team_id = newTeam;
      await api(endpoint, { method: "POST", body });
      toast.success(`Added ${d.get("name")}`);
      form.reset();
      setNewTeam(NO_TEAM);
      onChanged();
    } catch (err) {
      setError(err instanceof ApiError ? (err.fields.name ? `Name ${err.fields.name}` : err.message) : "Could not add");
    }
  }

  const active = items?.filter((i) => i.is_active).length ?? 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Icon className="size-4 text-primary" />
          {title}
          {items && (
            <Badge variant="secondary" className="ml-auto tabular-nums">
              {active} active
            </Badge>
          )}
        </CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <form onSubmit={onSubmit}>
          <Field data-invalid={!!error}>
            <div className="flex gap-2">
              <Input name="name" placeholder={`New ${noun} name`} required aria-invalid={!!error} />
              {withPlatform && <Input name="platform" placeholder="Platform" className="w-36" />}
              {teams && <TeamSelect teams={teams} value={newTeam} onChange={setNewTeam} />}
              <Button type="submit" variant="outline">
                <PlusIcon /> Add
              </Button>
            </div>
            <FieldError>{error}</FieldError>
          </Field>
        </form>

        <div className="divide-y rounded-lg border">
          {loading &&
            Array.from({ length: 3 }, (_, i) => (
              <div key={i} className="flex items-center justify-between p-3">
                <Skeleton className="h-4 w-36" />
                <Skeleton className="h-5 w-9 rounded-full" />
              </div>
            ))}
          {items?.map((item) => (
            <div key={item.id} className="flex items-center justify-between gap-4 px-3 py-2.5 hover:bg-muted/50">
              <label htmlFor={`active-${item.id}`} className={`min-w-0 flex-1 cursor-pointer ${item.is_active ? "" : "opacity-50"}`}>
                <div className="text-sm font-medium">{item.name}</div>
                {item.sub && <div className="text-xs text-muted-foreground">{item.sub}</div>}
              </label>
              {item.fee !== undefined && (
                <div className="flex items-center gap-1 text-xs text-muted-foreground">
                  <Input
                    type="number"
                    min={0}
                    max={100}
                    step={0.5}
                    defaultValue={Number(item.fee)}
                    aria-label={`${item.name} marketplace fee percent`}
                    className="h-8 w-16 text-right"
                    onBlur={(e) => {
                      const fee = Number(e.target.value);
                      if (e.target.value !== "" && fee !== Number(item.fee)) setFee.mutate({ item, fee });
                    }}
                  />
                  % fee
                </div>
              )}
              {teams && (
                <TeamSelect
                  teams={teams}
                  value={item.team_id ?? NO_TEAM}
                  disabled={setTeam.isPending}
                  onChange={(v) => setTeam.mutate({ item, teamId: v === NO_TEAM ? null : v })}
                  label={`${item.name} team`}
                />
              )}
              <Switch
                id={`active-${item.id}`}
                checked={item.is_active}
                onCheckedChange={() => toggle.mutate(item)}
                aria-label={`${item.name} active`}
              />
            </div>
          ))}
          {items?.length === 0 && (
            <Empty className="py-8">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <Icon />
                </EmptyMedia>
                <EmptyTitle>No {noun}s yet</EmptyTitle>
                <EmptyDescription>Add the first one above.</EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function TeamSelect({
  teams,
  value,
  onChange,
  disabled,
  label = "Team",
}: {
  teams: Team[];
  value: string;
  onChange: (v: string) => void;
  disabled?: boolean;
  label?: string;
}) {
  // A type may still point at a retired team; keep it visible.
  const known = value === NO_TEAM || teams.some((t) => t.id === value);
  return (
    <Select value={value} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger className="w-36 shrink-0" size="sm" aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={NO_TEAM}>No team</SelectItem>
        {!known && <SelectItem value={value}>Retired team</SelectItem>}
        {teams.map((t) => (
          <SelectItem key={t.id} value={t.id}>
            {t.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
