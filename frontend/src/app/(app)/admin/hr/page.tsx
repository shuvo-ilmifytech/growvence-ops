"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Building2Icon, CalendarHeartIcon, ClockIcon, CrosshairIcon, MapPinIcon, PlaneIcon, PlusIcon, Trash2Icon, WifiIcon } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { formatDate } from "@/lib/format";
import {
  currentPosition,
  days,
  errorText,
  HR_KEY,
  useDepartments,
  useHolidays,
  useHrMe,
  useLeaveTypes,
  useOfficeLocation,
  useOfficeNetwork,
  WEEKDAYS,
  type LeaveType,
  type OfficeLocation,
  type OfficeNetwork,
} from "@/lib/hr";
import { useDirectoryByRole } from "@/lib/teams";
import { PageHeader } from "@/components/page-header";
import { ShiftsAdmin } from "@/components/hr/shifts-admin";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";

function useAdminAction<T>(fn: (v: T) => Promise<unknown>, success: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      toast.success(success);
      qc.invalidateQueries({ queryKey: HR_KEY });
      qc.invalidateQueries({ queryKey: ["settings"] });
    },
    onError: (err) => toast.error(errorText(err)),
  });
}

export default function HrSettingsPage() {
  return (
    <div className="max-w-4xl space-y-6">
      <PageHeader title="HR settings" description="Office hours, office location and Wi-Fi, shifts, departments, leave types and holidays." />
      <Schedule />
      <OfficeLocationCard />
      <OfficeNetworkCard />
      <ShiftsAdmin />
      <LeaveTypes />
      <Holidays />
      <Departments />
    </div>
  );
}

// ── Work schedule ──────────────────────────────────────────

type ScheduleSettings = { work_start: string; work_end: string; late_grace_minutes: number; weekly_off_days: number[] };

function Schedule() {
  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => api<{ settings: ScheduleSettings }>("/settings").then((r) => r.settings),
  });
  const save = useAdminAction(
    ({ key, value }: { key: keyof ScheduleSettings; value: unknown }) => api(`/admin/settings/${key}`, { method: "PUT", body: { value } }),
    "Schedule saved",
  );
  const s = settings.data;
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <ClockIcon className="size-4 text-primary" /> Default office hours
        </CardTitle>
        <CardDescription>
          For everyone who isn&apos;t on a shift. Check-ins after the start time plus the grace period count as late. Times use the
          organisation timezone.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {!s ? (
          <Skeleton className="h-24" />
        ) : (
          <>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
              <div className="grid gap-2">
                <Label htmlFor="ws">Work starts</Label>
                <Input id="ws" type="time" defaultValue={s.work_start} onBlur={(e) => e.target.value !== s.work_start && save.mutate({ key: "work_start", value: e.target.value })} />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="we">Work ends</Label>
                <Input id="we" type="time" defaultValue={s.work_end} onBlur={(e) => e.target.value !== s.work_end && save.mutate({ key: "work_end", value: e.target.value })} />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="grace">Grace period (minutes)</Label>
                <Input
                  id="grace"
                  type="number"
                  min={0}
                  max={240}
                  defaultValue={s.late_grace_minutes}
                  onBlur={(e) => Number(e.target.value) !== s.late_grace_minutes && save.mutate({ key: "late_grace_minutes", value: Number(e.target.value) })}
                />
              </div>
            </div>
            <div className="space-y-2">
              <Label>Weekly off days</Label>
              <div className="flex flex-wrap gap-2">
                {WEEKDAYS.map((name, i) => {
                  const on = s.weekly_off_days.includes(i);
                  return (
                    <Button
                      key={name}
                      type="button"
                      size="sm"
                      variant={on ? "default" : "outline"}
                      aria-pressed={on}
                      disabled={save.isPending}
                      onClick={() =>
                        save.mutate({
                          key: "weekly_off_days",
                          value: on ? s.weekly_off_days.filter((d) => d !== i) : [...s.weekly_off_days, i].sort(),
                        })
                      }
                    >
                      {name.slice(0, 3)}
                    </Button>
                  );
                })}
              </div>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}

// ── Office location ────────────────────────────────────────

function OfficeLocationCard() {
  const office = useOfficeLocation();
  const o = office.data;
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <MapPinIcon className="size-4 text-primary" /> Office location
        </CardTitle>
        <CardDescription>
          When this is on, people can check in and out only from within the radius around the office. Their browser asks for their
          location each time. Anyone outside is refused and can request a correction instead.
        </CardDescription>
      </CardHeader>
      <CardContent>{!o ? <Skeleton className="h-24" /> : <OfficeLocationForm key={JSON.stringify(o)} office={o} />}</CardContent>
    </Card>
  );
}

function OfficeLocationForm({ office }: { office: OfficeLocation }) {
  const [enabled, setEnabled] = useState(office.enabled);
  const [lat, setLat] = useState(office.lat?.toString() ?? "");
  const [lng, setLng] = useState(office.lng?.toString() ?? "");
  const [radius, setRadius] = useState(office.radius_m.toString());
  const [locating, setLocating] = useState(false);
  const save = useAdminAction(
    (value: OfficeLocation) => api("/admin/settings/office_location", { method: "PUT", body: { value } }),
    "Office location saved",
  );

  const num = (v: string) => (v.trim() === "" || !Number.isFinite(Number(v)) ? null : Number(v));
  const value: OfficeLocation = { enabled, lat: num(lat), lng: num(lng), radius_m: Math.round(Number(radius) || 0) };
  const hasPlace = value.lat !== null && value.lng !== null;
  const problem =
    value.lat !== null && Math.abs(value.lat) > 90
      ? "Latitude must be between -90 and 90."
      : value.lng !== null && Math.abs(value.lng) > 180
        ? "Longitude must be between -180 and 180."
        : value.radius_m < 20 || value.radius_m > 5000
          ? "Radius must be between 20 and 5000 metres."
          : enabled && !hasPlace
            ? "Set the latitude and longitude before switching this on."
            : null;

  async function fillFromDevice() {
    setLocating(true);
    try {
      const p = await currentPosition();
      setLat(p.lat.toFixed(6));
      setLng(p.lng.toFixed(6));
      toast.info(`Location found (accurate to about ${Math.round(p.accuracy)} m). Check it on the map, then save.`);
    } catch (err) {
      toast.error(errorText(err));
    } finally {
      setLocating(false);
    }
  }

  return (
    <form
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault();
        if (!problem) save.mutate(value);
      }}
    >
      <div className="flex items-center gap-3">
        <Switch id="office-on" checked={enabled} onCheckedChange={setEnabled} />
        <Label htmlFor="office-on">Check location on check-in and check-out</Label>
      </div>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <div className="grid gap-2">
          <Label htmlFor="office-lat">Latitude</Label>
          <Input id="office-lat" inputMode="decimal" placeholder="23.780800" value={lat} onChange={(e) => setLat(e.target.value)} />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="office-lng">Longitude</Label>
          <Input id="office-lng" inputMode="decimal" placeholder="90.407400" value={lng} onChange={(e) => setLng(e.target.value)} />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="office-radius">Allowed radius (metres)</Label>
          <Input id="office-radius" type="number" min={20} max={5000} value={radius} onChange={(e) => setRadius(e.target.value)} />
        </div>
      </div>
      <p className="text-xs text-muted-foreground">
        Tip: in Google Maps, right-click the office and click the numbers at the top to copy them. Phone GPS is usually accurate to
        10–50 m, so a radius of 100–200 m works for most offices.
      </p>
      {problem && <p className="text-sm text-destructive">{problem}</p>}
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" disabled={!!problem || save.isPending}>
          Save
        </Button>
        <Button type="button" variant="outline" onClick={fillFromDevice} disabled={locating}>
          <CrosshairIcon /> {locating ? "Finding you…" : "Use my current location"}
        </Button>
        {hasPlace && (
          <Button variant="ghost" asChild>
            <a href={`https://www.google.com/maps?q=${value.lat},${value.lng}`} target="_blank" rel="noreferrer">
              <MapPinIcon /> View on map
            </a>
          </Button>
        )}
      </div>
    </form>
  );
}

// ── Office network ─────────────────────────────────────────

function OfficeNetworkCard() {
  const net = useOfficeNetwork();
  const n = net.data;
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <WifiIcon className="size-4 text-primary" /> Office Wi-Fi (public IP)
        </CardTitle>
        <CardDescription>
          Every check-in and check-out records the network it came from, and whether that is one of the office IPs below. When the
          check is on, people can check in and out only from the office Wi-Fi; anyone else is refused and can request a correction.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {!n ? <Skeleton className="h-24" /> : <OfficeNetworkForm key={JSON.stringify(n)} network={{ enabled: n.enabled, ips: n.ips ?? [] }} />}
      </CardContent>
    </Card>
  );
}

const IP_ENTRY = /^[0-9a-fA-F:.]+(\/\d{1,3})?$/;

function OfficeNetworkForm({ network }: { network: OfficeNetwork }) {
  const me = useHrMe();
  const myIP = me.data?.client_ip ?? "";
  const [enabled, setEnabled] = useState(network.enabled);
  const [text, setText] = useState(network.ips.join("\n"));
  const save = useAdminAction(
    (value: OfficeNetwork) => api("/admin/settings/office_network", { method: "PUT", body: { value } }),
    "Office Wi-Fi saved",
  );

  const ips = [...new Set(text.split(/[\s,]+/).map((s) => s.trim()).filter(Boolean))];
  const bad = ips.find((ip) => !IP_ENTRY.test(ip));
  const problem = bad
    ? `"${bad}" isn't an IP address or range.`
    : ips.length > 50
      ? "List at most 50 addresses."
      : enabled && ips.length === 0
        ? "Add the office IP before switching this on."
        : null;
  const myIPListed = myIP !== "" && ips.includes(myIP);

  return (
    <form
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault();
        if (!problem) save.mutate({ enabled, ips });
      }}
    >
      <div className="flex items-center gap-3">
        <Switch id="net-on" checked={enabled} onCheckedChange={setEnabled} />
        <Label htmlFor="net-on">Allow check-in and check-out only from the office Wi-Fi</Label>
      </div>
      <div className="grid gap-2">
        <Label htmlFor="net-ips">Office public IPs</Label>
        <Textarea
          id="net-ips"
          rows={3}
          className="font-mono"
          placeholder={"103.220.204.41\n103.220.204.0/24"}
          value={text}
          onChange={(e) => setText(e.target.value)}
        />
        <p className="text-xs text-muted-foreground">
          One per line. A range like <span className="font-mono">103.220.204.0/24</span> covers every address in it, which helps when
          the internet provider changes the last number.
        </p>
      </div>
      {myIP && (
        <p className="text-sm text-muted-foreground">
          This device is on <span className="font-mono text-foreground">{myIP}</span>
          {myIPListed ? " (listed)." : ". If you're at the office on its Wi-Fi, add it."}
        </p>
      )}
      {problem && <p className="text-sm text-destructive">{problem}</p>}
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" disabled={!!problem || save.isPending}>
          Save
        </Button>
        {myIP && !myIPListed && (
          <Button type="button" variant="outline" onClick={() => setText((t) => (t.trim() ? `${t.trim()}\n${myIP}` : myIP))}>
            <PlusIcon /> Add this device&apos;s IP
          </Button>
        )}
      </div>
    </form>
  );
}

// ── Leave types ────────────────────────────────────────────

function LeaveTypes() {
  const types = useLeaveTypes(true);
  const create = useAdminAction(
    (body: { name: string; annual_quota: number }) => api("/admin/leave-types", { method: "POST", body }),
    "Leave type added",
  );
  const update = useAdminAction(
    ({ id, ...body }: Partial<Omit<LeaveType, "annual_quota">> & { id: string; annual_quota?: number }) =>
      api(`/admin/leave-types/${id}`, { method: "PATCH", body }),
    "Leave type saved",
  );
  return (
    <Card className="pb-0">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <PlaneIcon className="size-4 text-primary" /> Leave types
        </CardTitle>
        <CardDescription>
          The yearly quota applies to everyone; set a different amount for one person on their profile. Unlimited types skip the balance check.
        </CardDescription>
      </CardHeader>
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="pl-6">Name</TableHead>
            <TableHead className="w-32">Days / year</TableHead>
            <TableHead>Limited</TableHead>
            <TableHead>Paid</TableHead>
            <TableHead className="pr-6">Active</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {(types.data ?? []).map((t) => (
            <TableRow key={t.id} className={t.is_active ? "" : "opacity-60"}>
              <TableCell className="pl-6 font-medium">{t.name}</TableCell>
              <TableCell>
                <Input
                  type="number"
                  min={0}
                  max={365}
                  step={0.5}
                  className="h-8 w-24"
                  aria-label={`${t.name} days per year`}
                  defaultValue={days(t.annual_quota)}
                  disabled={!t.is_limited}
                  onBlur={(e) => Number(e.target.value) !== Number(t.annual_quota) && update.mutate({ id: t.id, annual_quota: Number(e.target.value) })}
                />
              </TableCell>
              <TableCell>
                <Switch checked={t.is_limited} aria-label="Limited" onCheckedChange={(v) => update.mutate({ id: t.id, is_limited: v })} />
              </TableCell>
              <TableCell>
                <Switch checked={t.is_paid} aria-label="Paid" onCheckedChange={(v) => update.mutate({ id: t.id, is_paid: v })} />
              </TableCell>
              <TableCell className="pr-6">
                <Switch checked={t.is_active} aria-label="Active" onCheckedChange={(v) => update.mutate({ id: t.id, is_active: v })} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <AddRow
        placeholder="New leave type, e.g. Maternity"
        extra={{ label: "Days", def: "0" }}
        pending={create.isPending}
        onAdd={(name, extra) => create.mutateAsync({ name, annual_quota: Number(extra) })}
      />
    </Card>
  );
}

// ── Holidays ───────────────────────────────────────────────

function Holidays() {
  const today = useHrMe().data?.today;
  const [picked, setYear] = useState<number | null>(null);
  const year = picked ?? (today ? Number(today.slice(0, 4)) : 0);
  const holidays = useHolidays(year);
  const create = useAdminAction((body: { date: string; name: string }) => api("/admin/holidays", { method: "POST", body }), "Holiday added");
  const remove = useAdminAction((id: string) => api(`/admin/holidays/${id}`, { method: "DELETE" }), "Holiday removed");
  return (
    <Card className="pb-0">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <CalendarHeartIcon className="size-4 text-primary" /> Holidays
        </CardTitle>
        <CardDescription>Public holidays and office closures. They aren&apos;t counted as leave or absence.</CardDescription>
        <CardAction className={year ? "flex items-center gap-1" : "hidden"}>
          <Button variant="outline" size="sm" onClick={() => setYear(year - 1)}>
            {year - 1}
          </Button>
          <span className="px-2 text-sm font-medium tabular-nums">{year}</span>
          <Button variant="outline" size="sm" onClick={() => setYear(year + 1)}>
            {year + 1}
          </Button>
        </CardAction>
      </CardHeader>
      {(holidays.data ?? []).length > 0 && (
        <Table>
          <TableBody>
            {holidays.data!.map((h) => (
              <TableRow key={h.id}>
                <TableCell className="w-40 pl-6 tabular-nums">{formatDate(h.date)}</TableCell>
                <TableCell className="font-medium">{h.name}</TableCell>
                <TableCell className="w-12 pr-6 text-right">
                  <Button variant="ghost" size="icon-sm" aria-label={`Remove ${h.name}`} onClick={() => remove.mutate(h.id)} disabled={remove.isPending}>
                    <Trash2Icon />
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <AddRow
        placeholder="Holiday name, e.g. Victory Day"
        extra={{ label: "Date", def: `${year}-01-01`, type: "date" }}
        pending={create.isPending}
        onAdd={(name, date) => create.mutateAsync({ name, date })}
      />
    </Card>
  );
}

// ── Departments ────────────────────────────────────────────

const NO_HEAD = "none";

function Departments() {
  const departments = useDepartments(true);
  const leads = useDirectoryByRole("tl");
  const create = useAdminAction((name: string) => api("/admin/departments", { method: "POST", body: { name } }), "Department added");
  const update = useAdminAction(
    ({ id, ...body }: { id: string; name?: string; is_active?: boolean; head_id?: string | null }) =>
      api(`/admin/departments/${id}`, { method: "PATCH", body }),
    "Department saved",
  );
  return (
    <Card className="pb-0">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Building2Icon className="size-4 text-primary" /> Departments
        </CardTitle>
        <CardDescription>
          Assign people to a department on their profile. The department head, a Team Leader, runs every team in it: they see, assign
          and approve those teams&apos; projects.
        </CardDescription>
      </CardHeader>
      {(departments.data ?? []).length > 0 && (
        <Table>
          <TableBody>
            {departments.data!.map((d) => (
              <TableRow key={d.id} className={d.is_active ? "" : "opacity-60"}>
                <TableCell className="pl-6">
                  <Input
                    className="h-8 max-w-xs"
                    aria-label="Department name"
                    defaultValue={d.name}
                    onBlur={(e) => e.target.value.trim() && e.target.value.trim() !== d.name && update.mutate({ id: d.id, name: e.target.value.trim() })}
                  />
                </TableCell>
                <TableCell className="w-56">
                  <Select
                    value={d.head_id ?? NO_HEAD}
                    onValueChange={(v) => update.mutate({ id: d.id, head_id: v === NO_HEAD ? null : v })}
                  >
                    <SelectTrigger className="h-8 w-full" aria-label={`${d.name} head`}>
                      <SelectValue placeholder={leads.isPending ? "Loading…" : "No head"} />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={NO_HEAD}>No head</SelectItem>
                      {(leads.data ?? []).map((u) => (
                        <SelectItem key={u.id} value={u.id}>
                          {u.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </TableCell>
                <TableCell className="w-24 pr-6 text-right">
                  <Switch checked={d.is_active} aria-label="Active" onCheckedChange={(v) => update.mutate({ id: d.id, is_active: v })} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <AddRow placeholder="New department, e.g. Operations" pending={create.isPending} onAdd={(name) => create.mutateAsync(name)} />
    </Card>
  );
}

function AddRow({
  placeholder,
  extra,
  pending,
  onAdd,
}: {
  placeholder: string;
  extra?: { label: string; def: string; type?: string };
  pending: boolean;
  onAdd: (name: string, extra: string) => Promise<unknown>;
}) {
  return (
    <form
      className="flex flex-wrap gap-2 border-t px-6 py-4"
      onSubmit={async (e) => {
        e.preventDefault();
        const form = e.currentTarget;
        const d = new FormData(form);
        try {
          await onAdd(String(d.get("name")).trim(), String(d.get("extra") ?? ""));
          form.reset();
        } catch {
          // toast already shown
        }
      }}
    >
      <Input name="name" required placeholder={placeholder} className="min-w-48 flex-1" />
      {extra && (
        <Input
          name="extra"
          aria-label={extra.label}
          type={extra.type ?? "number"}
          defaultValue={extra.def}
          step={extra.type ? undefined : 0.5}
          min={extra.type ? undefined : 0}
          className="w-40"
          required
        />
      )}
      <Button type="submit" variant="outline" disabled={pending}>
        <PlusIcon /> Add
      </Button>
    </form>
  );
}
