"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import { days, EMPLOYMENT_LABELS, HR_KEY, useDepartments, useDirectory, useHrMe, useShifts, type Balance, type Employee, type EmploymentType } from "@/lib/hr";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldSeparator } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";

const NONE = "none";

function useSave(onDone: () => void) {
  const qc = useQueryClient();
  const [fields, setFields] = useState<Record<string, string>>({});
  const [pending, setPending] = useState(false);
  async function save(fn: () => Promise<unknown>, success: string) {
    setFields({});
    setPending(true);
    try {
      await fn();
      toast.success(success);
      qc.invalidateQueries({ queryKey: HR_KEY });
      onDone();
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length) setFields(err.fields);
      else toast.error(err instanceof Error ? err.message : "Could not save");
    } finally {
      setPending(false);
    }
  }
  return { fields, pending, save };
}

function TextField({
  name,
  label,
  value,
  error,
  type = "text",
  max,
}: {
  name: string;
  label: string;
  value: string | null;
  error?: string;
  type?: string;
  max?: number;
}) {
  return (
    <Field data-invalid={!!error}>
      <FieldLabel htmlFor={name}>{label}</FieldLabel>
      <Input id={name} name={name} type={type} defaultValue={value ?? ""} maxLength={max} aria-invalid={!!error} />
      <FieldError>{error && `${label} ${error}`}</FieldError>
    </Field>
  );
}

// ── Admin: full HR record ──────────────────────────────────

export function EmployeeDialog({ open, onOpenChange, employee }: { open: boolean; onOpenChange: (o: boolean) => void; employee: Employee }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        {open && <EmployeeForm employee={employee} onDone={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  );
}

function EmployeeForm({ employee: e, onDone }: { employee: Employee; onDone: () => void }) {
  const people = useDirectory();
  const departments = useDepartments(true);
  const [manager, setManager] = useState(e.manager_id ?? NONE);
  const [dept, setDept] = useState(e.department_id ?? NONE);
  const [type, setType] = useState<EmploymentType>(e.employment_type);
  const shifts = useShifts(true);
  const [shift, setShift] = useState(e.shift_id ?? NONE);
  const { fields, pending, save } = useSave(onDone);

  return (
    <form
      className="grid gap-6"
      onSubmit={(ev) => {
        ev.preventDefault();
        const d = new FormData(ev.currentTarget);
        const s = (k: string) => String(d.get(k) ?? "").trim();
        const opt = (k: string) => s(k) || null;
        save(
          () =>
            api(`/hr/employees/${e.user_id}`, {
              method: "PUT",
              body: {
                employee_code: opt("employee_code"),
                designation: s("designation"),
                department_id: dept === NONE ? null : dept,
                manager_id: manager === NONE ? null : manager,
                employment_type: type,
                join_date: opt("join_date"),
                phone: s("phone"),
                date_of_birth: opt("date_of_birth"),
                address: s("address"),
                emergency_contact_name: s("emergency_contact_name"),
                emergency_contact_phone: s("emergency_contact_phone"),
                national_id: s("national_id"),
                shift_id: shift === NONE ? null : shift,
              },
            }),
          "HR record saved",
        );
      }}
    >
      <DialogHeader>
        <DialogTitle>Edit HR record</DialogTitle>
        <DialogDescription>
          {e.name}&apos;s manager approves their leave and attendance corrections. With no manager, requests go to HR.
        </DialogDescription>
      </DialogHeader>
      <FieldGroup>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <TextField name="designation" label="Designation" value={e.designation} error={fields.designation} max={120} />
          <TextField name="employee_code" label="Employee ID" value={e.employee_code} error={fields.employee_code} max={30} />
          <Field data-invalid={!!fields.department_id}>
            <FieldLabel>Department</FieldLabel>
            <Select value={dept} onValueChange={setDept}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NONE}>No department</SelectItem>
                {(departments.data ?? []).map((d) => (
                  <SelectItem key={d.id} value={d.id} disabled={!d.is_active && d.id !== e.department_id}>
                    {d.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldError>{fields.department_id && `Department ${fields.department_id}`}</FieldError>
          </Field>
          <Field data-invalid={!!fields.manager_id}>
            <FieldLabel>Reports to</FieldLabel>
            <Select value={manager} onValueChange={setManager}>
              <SelectTrigger className="w-full" aria-invalid={!!fields.manager_id}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NONE}>Nobody</SelectItem>
                {(people.data ?? [])
                  .filter((p) => p.user_id !== e.user_id)
                  .map((p) => (
                    <SelectItem key={p.user_id} value={p.user_id}>
                      {p.name}
                    </SelectItem>
                  ))}
              </SelectContent>
            </Select>
            <FieldError>{fields.manager_id && `Manager ${fields.manager_id}`}</FieldError>
          </Field>
          <Field>
            <FieldLabel>Employment type</FieldLabel>
            <Select value={type} onValueChange={(v) => setType(v as EmploymentType)}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(Object.keys(EMPLOYMENT_LABELS) as EmploymentType[]).map((t) => (
                  <SelectItem key={t} value={t}>
                    {EMPLOYMENT_LABELS[t]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field data-invalid={!!fields.shift_id}>
            <FieldLabel>Shift</FieldLabel>
            <Select value={shift} onValueChange={setShift}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NONE}>Default office hours</SelectItem>
                {(shifts.data ?? []).map((sh) => (
                  <SelectItem key={sh.id} value={sh.id} disabled={!sh.is_active && sh.id !== e.shift_id}>
                    {sh.name} · {sh.start_time}–{sh.end_time}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldError>{fields.shift_id && `Shift ${fields.shift_id}`}</FieldError>
          </Field>
          <TextField name="join_date" label="Join date" type="date" value={e.join_date} error={fields.join_date} />
          <TextField name="phone" label="Phone" type="tel" value={e.phone} error={fields.phone} max={40} />
        </div>
        <FieldSeparator>Private</FieldSeparator>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <TextField name="date_of_birth" label="Date of birth" type="date" value={e.date_of_birth} error={fields.date_of_birth} />
          <TextField name="national_id" label="National ID" value={e.national_id} error={fields.national_id} max={40} />
          <TextField name="emergency_contact_name" label="Emergency contact" value={e.emergency_contact_name} error={fields.emergency_contact_name} max={120} />
          <TextField name="emergency_contact_phone" label="Emergency phone" type="tel" value={e.emergency_contact_phone} error={fields.emergency_contact_phone} max={40} />
        </div>
        <Field data-invalid={!!fields.address}>
          <FieldLabel htmlFor="address">Address</FieldLabel>
          <Textarea id="address" name="address" defaultValue={e.address} maxLength={500} />
          <FieldError>{fields.address && `Address ${fields.address}`}</FieldError>
        </Field>
      </FieldGroup>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">Cancel</Button>
        </DialogClose>
        <Button type="submit" disabled={pending}>Save</Button>
      </DialogFooter>
    </form>
  );
}

// ── Self: contact details ──────────────────────────────────

export function PersonalDialog({ open, onOpenChange, employee }: { open: boolean; onOpenChange: (o: boolean) => void; employee: Employee }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">{open && <PersonalForm employee={employee} onDone={() => onOpenChange(false)} />}</DialogContent>
    </Dialog>
  );
}

function PersonalForm({ employee: e, onDone }: { employee: Employee; onDone: () => void }) {
  const { fields, pending, save } = useSave(onDone);
  return (
    <form
      className="grid gap-6"
      onSubmit={(ev) => {
        ev.preventDefault();
        const d = new FormData(ev.currentTarget);
        const s = (k: string) => String(d.get(k) ?? "");
        save(
          () =>
            api("/hr/me/profile", {
              method: "PUT",
              body: {
                phone: s("phone"),
                address: s("address"),
                emergency_contact_name: s("emergency_contact_name"),
                emergency_contact_phone: s("emergency_contact_phone"),
              },
            }),
          "Contact details saved",
        );
      }}
    >
      <DialogHeader>
        <DialogTitle>My contact details</DialogTitle>
        <DialogDescription>Ask an admin to change anything else on your record.</DialogDescription>
      </DialogHeader>
      <FieldGroup>
        <TextField name="phone" label="Phone" type="tel" value={e.phone} error={fields.phone} max={40} />
        <TextField name="emergency_contact_name" label="Emergency contact" value={e.emergency_contact_name} error={fields.emergency_contact_name} max={120} />
        <TextField name="emergency_contact_phone" label="Emergency phone" type="tel" value={e.emergency_contact_phone} error={fields.emergency_contact_phone} max={40} />
        <Field data-invalid={!!fields.address}>
          <FieldLabel htmlFor="p-address">Address</FieldLabel>
          <Textarea id="p-address" name="address" defaultValue={e.address} maxLength={500} />
          <FieldError>{fields.address && `Address ${fields.address}`}</FieldError>
        </Field>
      </FieldGroup>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">Cancel</Button>
        </DialogClose>
        <Button type="submit" disabled={pending}>Save</Button>
      </DialogFooter>
    </form>
  );
}

// ── Admin: leave entitlements ──────────────────────────────

export function AllocationDialog({
  open,
  onOpenChange,
  employee,
  balances,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  employee: Employee;
  balances: Balance[];
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        {open && <AllocationForm employee={employee} balances={balances} onDone={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  );
}

function AllocationForm({ employee, balances, onDone }: { employee: Employee; balances: Balance[]; onDone: () => void }) {
  const { pending, save } = useSave(onDone);
  const year = Number(useHrMe().data?.today.slice(0, 4));
  const limited = balances.filter((b) => b.is_limited);
  return (
    <form
      className="grid gap-6"
      onSubmit={(ev) => {
        ev.preventDefault();
        const d = new FormData(ev.currentTarget);
        save(async () => {
          for (const b of limited) {
            const raw = String(d.get(b.leave_type_id) ?? "").trim();
            const value = raw === "" ? null : Number(raw);
            const current = b.is_override ? Number(b.entitled) : null;
            if (value === current) continue;
            await api("/admin/leave-allocations", {
              method: "PUT",
              body: { user_id: employee.user_id, leave_type_id: b.leave_type_id, year, days: value },
            });
          }
        }, "Entitlements saved");
      }}
    >
      <DialogHeader>
        <DialogTitle>Leave entitlements {year}</DialogTitle>
        <DialogDescription>Leave a field empty to use the standard quota for that leave type.</DialogDescription>
      </DialogHeader>
      <FieldGroup>
        {limited.map((b) => (
          <Field key={b.leave_type_id}>
            <FieldLabel htmlFor={b.leave_type_id}>{b.leave_type_name}</FieldLabel>
            <Input
              id={b.leave_type_id}
              name={b.leave_type_id}
              type="number"
              min={0}
              max={365}
              step={0.5}
              defaultValue={b.is_override ? days(b.entitled) : ""}
              placeholder={b.is_override ? undefined : `Standard: ${days(b.entitled)}`}
            />
            <FieldDescription>{days(b.approved)} taken, {days(b.pending)} pending.</FieldDescription>
          </Field>
        ))}
      </FieldGroup>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">Cancel</Button>
        </DialogClose>
        <Button type="submit" disabled={pending}>Save</Button>
      </DialogFooter>
    </form>
  );
}
