"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "./api";

export type Bucket = "day" | "week" | "month";

/** The ratios every row of the page shares: on time, turnaround and revisions. */
type Performance = {
  delivered: number;
  delivered_with_due: number;
  on_time: number;
  turnaround_avg_h: number;
  delivered_with_revisions: number;
};

export type AnalyticsTeam = Performance & {
  id: string;
  name: string;
  lead_name: string | null;
  members: number;
  incoming: number;
  completed: number;
  live_now: number;
  new_now: number;
  overdue_now: number;
};

export type AnalyticsPerson = Performance & {
  id: string;
  name: string;
  role: "member" | "tl";
  team_id: string | null;
  team_name: string | null;
  completed: number;
  open_now: number;
  overdue_now: number;
};

export type Totals = Performance & {
  incoming: number;
  completed: number;
  cancelled: number;
  turnaround_median_h: number;
  live_now: number;
  new_now: number;
  in_progress_now: number;
  blocked_now: number;
  in_review_now: number;
  ready_to_deliver_now: number;
  delivered_now: number;
  overdue_now: number;
};

export type RevenueRow = { team_id: string | null; currency: string; net: string; projects: number };

/** The steps a project waits in, in the order it moves through them. */
export const STEPS = ["new", "in_progress", "blocked", "ready_for_review", "ready_to_deliver", "delivered"] as const;
export type Step = (typeof STEPS)[number];
export const STEP_LABELS: Record<Step, string> = {
  new: "Waiting to be assigned",
  in_progress: "In progress",
  blocked: "Blocked",
  ready_for_review: "In review (TL check)",
  ready_to_deliver: "Ready to deliver",
  delivered: "Awaiting client",
};

export type Analytics = {
  range: { from: string; to: string };
  previous_range: { from: string; to: string };
  previous_totals: Totals;
  revenue: RevenueRow[];
  previous_revenue: RevenueRow[];
  step_times: { step: Step; projects: number; avg_h: number; median_h: number; max_h: number }[];
  stuck_now: { step: Step; projects: number; avg_h: number; max_h: number }[];
  lateness: { late: number; avg_h: number; median_h: number; max_h: number };
  bucket: Bucket;
  team_id: string | null;
  totals: Totals;
  series: { start: string; incoming: number; delivered: number; completed: number; cancelled: number }[];
  teams: AnalyticsTeam[];
  people: AnalyticsPerson[];
  types: { id: string; name: string; incoming: number; delivered: number; live_now: number }[];
  sales: { id: string; name: string; incoming: number; delivered: number; completed: number; cancelled: number }[];
};

export function useAnalytics(from: string, to: string, teamId: string | null, enabled = true) {
  const p = new URLSearchParams({ from, to });
  if (teamId) p.set("team_id", teamId);
  return useQuery({
    queryKey: ["analytics", p.toString()],
    queryFn: () => api<Analytics>(`/analytics?${p}`),
    placeholderData: (prev) => prev,
    enabled,
  });
}

/** "83%" of the deliveries that had a due date, or "—" when none did. */
export function onTimeRate(p: Pick<Performance, "on_time" | "delivered_with_due">) {
  return p.delivered_with_due ? `${Math.round((p.on_time / p.delivered_with_due) * 100)}%` : "—";
}

/** Revenue rows summed per currency: { USD: 1234.5 }. */
export function sumRevenue(rows: RevenueRow[], teamId?: string) {
  const out: Record<string, number> = {};
  for (const r of rows) if (teamId === undefined || r.team_id === teamId) out[r.currency] = (out[r.currency] ?? 0) + Number(r.net);
  return out;
}

// ── The projects behind a number ─────────────────────────

export type Metric =
  | "incoming" | "delivered" | "completed" | "cancelled" | "on_time" | "late" | "revisions"
  | "open" | "member_open" | "overdue" | "awaiting_client"
  | "new" | "in_progress" | "blocked" | "ready_for_review" | "ready_to_deliver";

export type DrillFilter = { team_id?: string; user_id?: string; type_id?: string; sales_id?: string };

export type DrillProject = {
  id: string;
  number: number;
  name: string | null;
  status: import("./types").ProjectStatus;
  client_name: string | null;
  team_name: string | null;
  type_name: string | null;
  sales_name: string;
  members: string;
  first_submitted_at: string | null;
  delivered_at: string | null;
  completed_at: string | null;
  due: string;
  late_h: number | null;
  turnaround_h: number | null;
  revision_count: number;
  currency: string;
  net: string;
};

export function useAnalyticsProjects(metric: Metric | null, from: string, to: string, filter: DrillFilter) {
  const p = new URLSearchParams({ from, to, metric: metric ?? "" });
  for (const [k, v] of Object.entries(filter)) if (v) p.set(k, v);
  return useQuery({
    queryKey: ["analytics", "projects", p.toString()],
    queryFn: () => api<{ projects: DrillProject[] }>(`/analytics/projects?${p}`).then((r) => r.projects),
    enabled: !!metric,
  });
}

// ── Trend ────────────────────────────────────────────────

export type TargetAmount = { amount: string; currency: string };

export type Trend = {
  months: string[];
  teams: { id: string; name: string; months: Record<string, { delivered: number; revenue: Record<string, string>; target: TargetAmount | null }> }[];
  people: { id: string; name: string; team_name: string | null; months: Record<string, number> }[];
};

export function useTrend(months: number, teamId: string | null, enabled = true) {
  const p = new URLSearchParams({ months: String(months) });
  if (teamId) p.set("team_id", teamId);
  return useQuery({ queryKey: ["analytics", "trend", p.toString()], queryFn: () => api<Trend>(`/analytics/trend?${p}`), enabled });
}

// ── Targets ──────────────────────────────────────────────

export type TeamTarget = {
  team_id: string;
  team_name: string;
  lead_name: string | null;
  amount: string; // "" when no target is set
  currency: string;
  set_by_name: string | null;
  updated_at: string | null;
  achieved: string;
  projects: number;
  other_currency: number;
};

export type Targets = { month: string; elapsed: number; teams: TeamTarget[] };

export function useTargets(month: string, enabled = true) {
  return useQuery({ queryKey: ["analytics", "targets", month], queryFn: () => api<Targets>(`/analytics/targets?month=${month}`), enabled });
}

/** Set (amount given) or clear (amount null) a team's target for a month. CEO only. */
export function useSaveTarget() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ teamId, month, amount, currency }: { teamId: string; month: string; amount: string | null; currency?: string }) =>
      amount === null
        ? api(`/analytics/targets/${teamId}/${month}`, { method: "DELETE" })
        : api(`/analytics/targets/${teamId}/${month}`, { method: "PUT", body: { amount, currency } }),
    onSuccess: (_, v) => {
      toast.success(v.amount === null ? "Target removed" : "Target saved");
      qc.invalidateQueries({ queryKey: ["analytics"] });
    },
  });
}

/** "Nov 2026" from "2026-11". */
export function monthLabel(m: string, long = false) {
  return new Date(`${m}-01T00:00:00`).toLocaleDateString(undefined, { month: long ? "long" : "short", year: "numeric" });
}

export function addMonth(m: string, n: number) {
  const d = new Date(`${m}-01T00:00:00`);
  d.setMonth(d.getMonth() + n);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}
