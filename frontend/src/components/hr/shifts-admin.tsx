"use client";

import { useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { MoonIcon, PencilIcon, PlusIcon, SearchIcon, SunIcon, TimerIcon, UsersIcon } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import { HR_KEY, offDaysLabel, useDirectory, useHrAction, useShifts, WEEKDAYS, type Shift } from "@/lib/hr";
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
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

const DEFAULT = "default";

/** Shifts: HR and the CEO create them and put people on them. */
export function ShiftsAdmin() {
  const shifts = useShifts(true);
  const [editing, setEditing] = useState<Shift | "new" | null>(null);
  const [assigning, setAssigning] = useState<string | null>(null); // shift id, or DEFAULT
  const toggle = useHrAction(
    ({ id, is_active }: { id: string; is_active: boolean }) => api(`/admin/shifts/${id}`, { method: "PATCH", body: { is_active } }),
    ({ is_active }) => (is_active ? "Shift reactivated" : "Shift retired"),
  );
  const rows = shifts.data ?? [];

  return (
    <Card className="pb-0">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <TimerIcon className="size-4 text-primary" /> Shifts
        </CardTitle>
        <CardDescription>
          Each shift has its own hours, grace period and weekly off days. People without a shift follow the default office hours above. A
          shift that ends before it starts runs overnight and counts for the day it starts.
        </CardDescription>
        <CardAction className="flex gap-2">
          <Button variant="outline" size="sm" onClick={() => setAssigning(rows.find((s) => s.is_active)?.id ?? DEFAULT)}>
            <UsersIcon /> Assign people
          </Button>
          <Button size="sm" onClick={() => setEditing("new")}>
            <PlusIcon /> New shift
          </Button>
        </CardAction>
      </CardHeader>
      {rows.length === 0 ? (
        <p className="border-t px-6 py-4 text-sm text-muted-foreground">No shifts yet. Everyone follows the default office hours.</p>
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="pl-6">Shift</TableHead>
                <TableHead>Hours</TableHead>
                <TableHead className="hidden md:table-cell">Grace</TableHead>
                <TableHead className="hidden md:table-cell">Off days</TableHead>
                <TableHead className="text-right">People</TableHead>
                <TableHead>Active</TableHead>
                <TableHead className="w-12 pr-6" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((s) => {
                const overnight = s.end_time <= s.start_time;
                return (
                  <TableRow key={s.id} className={s.is_active ? "" : "opacity-60"}>
                    <TableCell className="pl-6 font-medium">
                      <span className="inline-flex items-center gap-1.5">
                        {overnight ? <MoonIcon className="size-3.5 text-indigo-500" /> : <SunIcon className="size-3.5 text-amber-500" />}
                        {s.name}
                      </span>
                    </TableCell>
                    <TableCell className="tabular-nums whitespace-nowrap">
                      {s.start_time}–{s.end_time}
                      {overnight && <span className="text-xs text-muted-foreground"> (+1 day)</span>}
                    </TableCell>
                    <TableCell className="hidden tabular-nums md:table-cell">{s.grace_minutes} min</TableCell>
                    <TableCell className="hidden md:table-cell">{offDaysLabel(s.off_days)}</TableCell>
                    <TableCell className="text-right">
                      <Button variant="link" className="h-auto p-0 tabular-nums" onClick={() => setAssigning(s.id)} disabled={!s.is_active}>
                        {s.member_count}
                      </Button>
                    </TableCell>
                    <TableCell>
                      <Switch
                        checked={s.is_active}
                        aria-label={`${s.name} active`}
                        disabled={toggle.isPending}
                        onCheckedChange={(v) => toggle.mutate({ id: s.id, is_active: v })}
                      />
                    </TableCell>
                    <TableCell className="pr-6 text-right">
                      <Button variant="ghost" size="icon-sm" aria-label={`Edit ${s.name}`} onClick={() => setEditing(s)}>
                        <PencilIcon />
                      </Button>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      )}
      <ShiftDialog shift={editing} onOpenChange={(o) => !o && setEditing(null)} />
      <AssignDialog shiftId={assigning} shifts={rows.filter((s) => s.is_active)} onOpenChange={(o) => !o && setAssigning(null)} />
    </Card>
  );
}

// ── Create / edit ──────────────────────────────────────────

function ShiftDialog({ shift, onOpenChange }: { shift: Shift | "new" | null; onOpenChange: (o: boolean) => void }) {
  return (
    <Dialog open={!!shift} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        {shift && <ShiftForm key={shift === "new" ? "new" : shift.id} shift={shift === "new" ? undefined : shift} onDone={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  );
}

function ShiftForm({ shift, onDone }: { shift?: Shift; onDone: () => void }) {
  const qc = useQueryClient();
  const [off, setOff] = useState<number[]>(shift?.off_days ?? [5, 6]);
  const [start, setStart] = useState(shift?.start_time ?? "09:00");
  const [end, setEnd] = useState(shift?.end_time ?? "18:00");
  const [fields, setFields] = useState<Record<string, string>>({});
  const [pending, setPending] = useState(false);
  const overnight = !!start && !!end && end <= start;

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const d = new FormData(e.currentTarget);
    const body = {
      name: String(d.get("name")).trim(),
      start_time: start,
      end_time: end,
      grace_minutes: Number(d.get("grace")),
      off_days: [...off].sort(),
    };
    setFields({});
    setPending(true);
    try {
      await api(shift ? `/admin/shifts/${shift.id}` : "/admin/shifts", { method: shift ? "PATCH" : "POST", body });
      toast.success(shift ? "Shift saved" : `${body.name} shift created`);
      qc.invalidateQueries({ queryKey: HR_KEY });
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
        <DialogTitle>{shift ? `Edit ${shift.name}` : "New shift"}</DialogTitle>
        <DialogDescription>
          Changes apply to attendance going forward and to how past days are shown.
        </DialogDescription>
      </DialogHeader>
      <FieldGroup>
        <Field data-invalid={!!fields.name}>
          <FieldLabel htmlFor="shift-name">Name</FieldLabel>
          <Input id="shift-name" name="name" defaultValue={shift?.name} placeholder="e.g. Night" maxLength={60} required autoFocus />
          <FieldError>{fields.name && `Name ${fields.name}`}</FieldError>
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field data-invalid={!!fields.start_time}>
            <FieldLabel htmlFor="shift-start">Starts</FieldLabel>
            <Input id="shift-start" type="time" value={start} onChange={(e) => setStart(e.target.value)} required />
            <FieldError>{fields.start_time && `Start ${fields.start_time}`}</FieldError>
          </Field>
          <Field data-invalid={!!fields.end_time}>
            <FieldLabel htmlFor="shift-end">Ends</FieldLabel>
            <Input id="shift-end" type="time" value={end} onChange={(e) => setEnd(e.target.value)} required />
            <FieldError>{fields.end_time && `End ${fields.end_time}`}</FieldError>
          </Field>
        </div>
        {overnight && (
          <p className="-mt-3 flex items-center gap-1.5 text-xs text-indigo-700 dark:text-indigo-300">
            <MoonIcon className="size-3.5" /> Overnight: ends the next morning. Attendance counts for the day it starts.
          </p>
        )}
        <Field data-invalid={!!fields.grace_minutes}>
          <FieldLabel htmlFor="shift-grace">Grace period (minutes)</FieldLabel>
          <Input id="shift-grace" name="grace" type="number" min={0} max={240} defaultValue={shift?.grace_minutes ?? 15} required />
          <FieldDescription>Check-ins later than this after the start count as late.</FieldDescription>
          <FieldError>{fields.grace_minutes && `Grace ${fields.grace_minutes}`}</FieldError>
        </Field>
        <Field data-invalid={!!fields.off_days}>
          <FieldLabel>Weekly off days</FieldLabel>
          <div className="flex flex-wrap gap-1.5">
            {WEEKDAYS.map((name, i) => {
              const on = off.includes(i);
              return (
                <Button
                  key={name}
                  type="button"
                  size="sm"
                  variant={on ? "default" : "outline"}
                  aria-pressed={on}
                  onClick={() => setOff(on ? off.filter((d) => d !== i) : [...off, i])}
                >
                  {name.slice(0, 3)}
                </Button>
              );
            })}
          </div>
          <FieldError>{fields.off_days && `Off days ${fields.off_days}`}</FieldError>
        </Field>
      </FieldGroup>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">Cancel</Button>
        </DialogClose>
        <Button type="submit" disabled={pending}>{shift ? "Save" : "Create shift"}</Button>
      </DialogFooter>
    </form>
  );
}

// ── Assign people ──────────────────────────────────────────

function AssignDialog({ shiftId, shifts, onOpenChange }: { shiftId: string | null; shifts: Shift[]; onOpenChange: (o: boolean) => void }) {
  return (
    <Dialog open={!!shiftId} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        {shiftId && <AssignForm key={shiftId} initial={shiftId} shifts={shifts} onDone={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  );
}

function AssignForm({ initial, shifts, onDone }: { initial: string; shifts: Shift[]; onDone: () => void }) {
  const people = useDirectory();
  const [target, setTarget] = useState(initial);
  const [search, setSearch] = useState("");
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const assign = useHrAction(
    () =>
      api("/admin/shifts/assign", {
        method: "POST",
        body: { shift_id: target === DEFAULT ? null : target, user_ids: [...picked] },
      }),
    () => `${picked.size} ${picked.size === 1 ? "person" : "people"} moved to ${target === DEFAULT ? "office hours" : shifts.find((s) => s.id === target)?.name}`,
  );
  const rows = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (people.data ?? []).filter((p) => !q || p.name.toLowerCase().includes(q) || (p.department_name ?? "").toLowerCase().includes(q));
  }, [people.data, search]);
  const toggle = (id: string) =>
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  return (
    <form
      className="grid gap-5"
      onSubmit={(e) => {
        e.preventDefault();
        assign.mutate(undefined, { onSuccess: onDone });
      }}
    >
      <DialogHeader>
        <DialogTitle>Assign people to a shift</DialogTitle>
        <DialogDescription>Pick the shift, then tick the people who work it.</DialogDescription>
      </DialogHeader>
      <Select value={target} onValueChange={setTarget}>
        <SelectTrigger className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {shifts.map((s) => (
            <SelectItem key={s.id} value={s.id}>
              {s.name} · {s.start_time}–{s.end_time}
            </SelectItem>
          ))}
          <SelectItem value={DEFAULT}>Default office hours (no shift)</SelectItem>
        </SelectContent>
      </Select>
      <InputGroup>
        <InputGroupAddon>
          <SearchIcon />
        </InputGroupAddon>
        <InputGroupInput placeholder="Search name or department" value={search} onChange={(e) => setSearch(e.target.value)} />
      </InputGroup>
      <ul className="max-h-72 divide-y overflow-y-auto rounded-lg border">
        {rows.map((p) => {
          const current = p.shift_id ?? DEFAULT;
          return (
            <li key={p.user_id}>
              <label className="flex cursor-pointer items-center gap-3 px-3 py-2 hover:bg-muted/60">
                <input
                  type="checkbox"
                  className="size-4 accent-[var(--primary)]"
                  checked={picked.has(p.user_id)}
                  onChange={() => toggle(p.user_id)}
                />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium">{p.name}</span>
                  <span className="block truncate text-xs text-muted-foreground">{p.designation || p.department_name || p.email}</span>
                </span>
                {current === target ? (
                  <Badge variant="outline">On this shift</Badge>
                ) : (
                  <span className="shrink-0 text-xs text-muted-foreground">{p.shift_name ?? "Office hours"}</span>
                )}
              </label>
            </li>
          );
        })}
      </ul>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">Cancel</Button>
        </DialogClose>
        <Button type="submit" disabled={assign.isPending || picked.size === 0}>
          Assign {picked.size > 0 && picked.size}
        </Button>
      </DialogFooter>
    </form>
  );
}
