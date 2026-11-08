"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, ApiError } from "./api";
import { useHydrated } from "./session";
import type { Role } from "./types";

export type EmploymentType = "full_time" | "part_time" | "intern" | "contract";
export const EMPLOYMENT_LABELS: Record<EmploymentType, string> = {
  full_time: "Full-time",
  part_time: "Part-time",
  intern: "Intern",
  contract: "Contract",
};

export type Employee = {
  user_id: string;
  name: string;
  email: string;
  role: Role;
  is_active: boolean;
  employee_code: string | null;
  designation: string;
  department_id: string | null;
  department_name: string | null;
  manager_id: string | null;
  manager_name: string | null;
  employment_type: EmploymentType;
  join_date: string | null;
  phone: string;
  date_of_birth: string | null;
  address: string;
  emergency_contact_name: string;
  emergency_contact_phone: string;
  national_id: string;
  updated_at: string;
  shift_id: string | null;
  shift_name: string | null;
  restricted: boolean;
};

export type DirectoryEmployee = Employee & { out_today: boolean };

export type Balance = {
  leave_type_id: string;
  leave_type_name: string;
  is_limited: boolean;
  is_paid: boolean;
  entitled: string;
  is_override: boolean;
  approved: string;
  pending: string;
};

export type LeaveStatus = "pending" | "awaiting_hr" | "approved" | "rejected" | "cancelled";
export const LEAVE_STATUS_LABELS: Record<LeaveStatus, string> = {
  pending: "Waiting for TL",
  awaiting_hr: "Waiting for HR",
  approved: "Approved",
  rejected: "Rejected",
  cancelled: "Cancelled",
};

export type LeaveRequest = {
  id: string;
  user_id: string;
  user_name: string;
  leave_type_id: string;
  leave_type_name: string;
  is_paid: boolean;
  start_date: string;
  end_date: string;
  half_day: boolean;
  days: string;
  reason: string;
  status: LeaveStatus;
  reviewed_by: string | null;
  reviewed_by_name: string | null;
  reviewed_at: string | null;
  review_note: string;
  cancelled_by: string | null;
  created_at: string;
  manager_reviewed_by: string | null;
  manager_reviewed_by_name: string | null;
  manager_reviewed_at: string | null;
  manager_note: string;
};

export type Correction = {
  id: string;
  user_id: string;
  user_name: string;
  work_date: string;
  check_in_at: string;
  check_out_at: string | null;
  reason: string;
  status: LeaveStatus;
  reviewed_by_name: string | null;
  reviewed_at: string | null;
  review_note: string;
  created_at: string;
};

export type Schedule = {
  shift_id: string | null;
  shift_name: string; // "" = default office hours
  overnight: boolean;
  timezone: string;
  work_start: string;
  work_end: string;
  grace_minutes: number;
  off_days: number[];
};

export type AttendanceRecord = {
  id: string;
  work_date: string;
  check_in_at: string;
  check_out_at: string | null;
  source: "self" | "correction" | "admin";
  note: string;
};

export type HrMe = {
  employee: Employee;
  today: string;
  work_date: string;
  today_record: AttendanceRecord | null;
  is_late: boolean;
  is_working_day: boolean;
  schedule: Schedule;
  balances: Balance[];
  pending_leave: number;
  approval_count: number;
  /** This device's public IP as the server sees it. */
  client_ip: string;
};

export type DayStatus = "present" | "late" | "absent" | "leave" | "half_day_leave" | "holiday" | "weekend" | "upcoming" | "untracked";
export const DAY_LABELS: Record<DayStatus, string> = {
  present: "Present",
  late: "Late",
  absent: "Absent",
  leave: "On leave",
  half_day_leave: "Half-day leave",
  holiday: "Holiday",
  weekend: "Weekly off",
  upcoming: "—",
  untracked: "Not tracked",
};

export type DayCell = {
  date: string;
  status: DayStatus;
  check_in: string | null;
  check_out: string | null;
  worked_minutes: number;
  left_early: boolean;
  source?: string;
  leave_type?: string;
  holiday?: string;
};

export type PersonSheet = {
  user_id: string;
  name: string;
  designation: string;
  department_name: string | null;
  schedule: Schedule;
  days: DayCell[];
  summary: Partial<Record<DayStatus, number>>;
  worked_minutes: number;
};

export type Sheet = {
  from: string;
  to: string;
  today: string;
  schedule: Schedule;
  holidays: Holiday[];
  people: PersonSheet[];
};

export type LeaveType = {
  id: string;
  name: string;
  annual_quota: string;
  is_limited: boolean;
  is_paid: boolean;
  is_active: boolean;
};

/** head_id: the department head, a Team Leader who runs every team in it. */
export type Department = { id: string; name: string; is_active: boolean; head_id?: string | null };
export type Shift = {
  id: string;
  name: string;
  start_time: string;
  end_time: string;
  grace_minutes: number;
  off_days: number[];
  is_active: boolean;
  member_count: number;
};

/** "Night · 22:00–06:00" or "Office hours · 09:00–18:00". */
export function scheduleLabel(s: Pick<Schedule, "shift_name" | "work_start" | "work_end">) {
  return `${s.shift_name || "Office hours"} · ${s.work_start}–${s.work_end}`;
}

export function offDaysLabel(days: number[]) {
  return days.length ? days.map((d) => WEEKDAYS[d]!.slice(0, 3)).join(", ") : "None";
}
export type Holiday = { id: string; date: string; name: string };
export type OutEntry = { user_id: string; user_name: string; leave_type_name: string; start_date: string; end_date: string; half_day: boolean };

export type EmployeeView = {
  employee: Employee;
  can_edit: boolean;
  can_see_hr: boolean;
  balances: Balance[];
  leave: LeaveRequest[];
  reports: Employee[];
};

export const WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

/** "2.0" → "2", "0.5" → "½"-free "0.5". */
export function days(n: string | number) {
  const v = typeof n === "number" ? n : Number(n);
  return Number.isInteger(v) ? String(v) : v.toFixed(1);
}

export function remaining(b: Balance) {
  return Number(b.entitled) - Number(b.approved) - Number(b.pending);
}

export function hours(minutes: number) {
  if (!minutes) return "—";
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return m ? `${h}h ${m}m` : `${h}h`;
}

/** Wall-clock time of an instant in the organisation's timezone. */
export function clock(iso: string | null | undefined, tz: string) {
  if (!iso) return "—";
  return new Intl.DateTimeFormat("en", { hour: "numeric", minute: "2-digit", timeZone: tz }).format(new Date(iso));
}

export function errorText(err: unknown) {
  if (err instanceof ApiError) return Object.values(err.fields)[0] ? `${Object.keys(err.fields)[0]!.replaceAll("_", " ")} ${Object.values(err.fields)[0]}` : err.message;
  return err instanceof Error ? err.message : "Something went wrong";
}

// ── Queries ────────────────────────────────────────────────

export const HR_KEY = ["hr"];

// The shell reads these too, so a page that hydrates later could find them already
// cached and render differently from the server: hide data until hydrated (see useMe).

export function useHrMe(enabled = true) {
  const hydrated = useHydrated();
  const q = useQuery({ queryKey: [...HR_KEY, "me"], queryFn: () => api<HrMe>("/hr/me"), enabled });
  return hydrated ? q : { ...q, data: undefined };
}

export function useDirectory() {
  const hydrated = useHydrated();
  const q = useQuery({
    queryKey: [...HR_KEY, "directory"],
    queryFn: () => api<{ employees: DirectoryEmployee[] }>("/hr/directory").then((r) => r.employees),
    staleTime: 60_000,
  });
  return hydrated ? q : { ...q, data: undefined };
}

export function useEmployee(id: string) {
  return useQuery({ queryKey: [...HR_KEY, "employee", id], queryFn: () => api<EmployeeView>(`/hr/employees/${id}`) });
}

export function useSheet(params: { month: string; user_id?: string; department_id?: string }) {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v) p.set(k, v);
  return useQuery({
    queryKey: [...HR_KEY, "sheet", p.toString()],
    queryFn: () => api<Sheet>(`/hr/attendance?${p}`),
    placeholderData: (prev) => prev,
    enabled: !!params.month && params.user_id !== "",
  });
}

export function useMyLeave(userId: string | undefined) {
  return useQuery({
    queryKey: [...HR_KEY, "leave", userId],
    queryFn: () => api<{ leave: LeaveRequest[] }>(`/hr/leave?user_id=${userId}`).then((r) => r.leave),
    enabled: !!userId,
  });
}

export function useMyCorrections() {
  return useQuery({
    queryKey: [...HR_KEY, "corrections"],
    queryFn: () => api<{ corrections: Correction[] }>("/hr/me/corrections").then((r) => r.corrections),
  });
}

export function useApprovals(enabled = true) {
  return useQuery({
    queryKey: [...HR_KEY, "approvals"],
    queryFn: () => api<{ leave: LeaveRequest[]; corrections: Correction[] }>("/hr/approvals"),
    enabled,
  });
}

export function useLeaveTypes(all = false) {
  return useQuery({
    queryKey: [...HR_KEY, "leave-types", all],
    queryFn: () => api<{ leave_types: LeaveType[] }>(`/hr/leave-types${all ? "?all=true" : ""}`).then((r) => r.leave_types),
    staleTime: 60_000,
  });
}

export function useDepartments(all = false) {
  return useQuery({
    queryKey: [...HR_KEY, "departments", all],
    queryFn: () => api<{ departments: Department[] }>(`/hr/departments${all ? "?all=true" : ""}`).then((r) => r.departments),
    staleTime: 60_000,
  });
}

export function useShifts(all = false) {
  return useQuery({
    queryKey: [...HR_KEY, "shifts", all],
    queryFn: () => api<{ shifts: Shift[] }>(`/hr/shifts${all ? "?all=true" : ""}`).then((r) => r.shifts),
    staleTime: 60_000,
  });
}

export function useHolidays(year: number) {
  return useQuery({
    queryKey: [...HR_KEY, "holidays", year],
    queryFn: () => api<{ holidays: Holiday[] }>(`/hr/holidays?year=${year}`).then((r) => r.holidays),
    enabled: year > 0,
  });
}

export function useWhosOut(from: string, to: string) {
  return useQuery({
    queryKey: [...HR_KEY, "whos-out", from, to],
    queryFn: () => api<{ out: OutEntry[] }>(`/hr/whos-out?from=${from}&to=${to}`).then((r) => r.out),
    enabled: !!from && !!to,
  });
}

/** A mutation that refreshes every HR query and toasts on success or failure. */
export function useHrAction<T>(fn: (vars: T) => Promise<unknown>, success: string | ((vars: T) => string)) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: (_, vars) => {
      toast.success(typeof success === "function" ? success(vars) : success);
      qc.invalidateQueries({ queryKey: HR_KEY });
      qc.invalidateQueries({ queryKey: ["notifications"] });
    },
    onError: (err) => toast.error(errorText(err)),
  });
}

// ── Office location ────────────────────────────────────────

/** The "office_location" setting: when enabled, check-in and out only work within radius_m of lat/lng. */
export type OfficeLocation = { enabled: boolean; lat: number | null; lng: number | null; radius_m: number };

export function useOfficeLocation() {
  return useQuery({
    queryKey: ["settings"],
    queryFn: () => api<{ settings: Record<string, unknown> }>("/settings").then((r) => r.settings),
    select: (s) => s.office_location as OfficeLocation | undefined,
  });
}

// ── Office network ─────────────────────────────────────────

/** The "office_network" setting: the office's public IPs (addresses or CIDR ranges). Every
 * check-in records whether it came from one; when enabled, others are refused. */
export type OfficeNetwork = { enabled: boolean; ips: string[] };

export function useOfficeNetwork() {
  return useQuery({
    queryKey: ["settings"],
    queryFn: () => api<{ settings: Record<string, unknown> }>("/settings").then((r) => r.settings),
    select: (s) => s.office_network as OfficeNetwork | undefined,
  });
}

export type DevicePosition = { lat: number; lng: number; accuracy: number };

/** Asks the browser where this device is. Rejects with a message fit for a toast. */
export function currentPosition(): Promise<DevicePosition> {
  return new Promise((resolve, reject) => {
    if (!navigator.geolocation) {
      reject(new Error("This browser can't share your location. Try another browser or your phone."));
      return;
    }
    navigator.geolocation.getCurrentPosition(
      (p) => resolve({ lat: p.coords.latitude, lng: p.coords.longitude, accuracy: p.coords.accuracy }),
      (e) =>
        reject(
          new Error(
            e.code === e.PERMISSION_DENIED
              ? "Location access is blocked. Allow it for this site in your browser settings, then try again."
              : "Couldn't find your location. Turn on location services and try again.",
          ),
        ),
      { enableHighAccuracy: true, timeout: 15_000, maximumAge: 0 },
    );
  });
}

// ── Dates ──────────────────────────────────────────────────

export function monthOf(day: string) {
  return day.slice(0, 7);
}

export function shiftMonth(month: string, by: number) {
  const [y, m] = month.split("-").map(Number);
  const d = new Date(y!, m! - 1 + by, 1);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

export function monthLabel(month: string) {
  const [y, m] = month.split("-").map(Number);
  return new Intl.DateTimeFormat("en", { month: "long", year: "numeric" }).format(new Date(y!, m! - 1, 1));
}

export function weekday(day: string) {
  const [y, m, d] = day.split("-").map(Number);
  return new Date(y!, m! - 1, d!).getDay();
}

// ── Salaries (HR + CEO only; nobody sees their own) ────────

export type SalaryEntry = {
  id: string;
  user_id: string;
  amount: string;
  currency: string;
  effective_from: string;
  note: string;
  created_at: string;
  created_by_name: string;
};

export type CurrentSalary = {
  user_id: string;
  user_name: string;
  department_name: string;
  designation: string;
  amount: string;
  currency: string;
  effective_from: string;
};

export function useSalaries(enabled: boolean) {
  return useQuery({
    queryKey: [...HR_KEY, "salaries"],
    queryFn: () => api<{ salaries: CurrentSalary[] }>("/hr/salaries").then((r) => r.salaries),
    enabled,
  });
}

export function useSalaryHistory(userId: string, enabled: boolean) {
  return useQuery({
    queryKey: [...HR_KEY, "salary", userId],
    queryFn: () => api<{ history: SalaryEntry[] }>(`/hr/employees/${userId}/salary`).then((r) => r.history),
    enabled,
  });
}
