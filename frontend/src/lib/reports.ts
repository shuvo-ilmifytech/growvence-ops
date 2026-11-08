"use client";

import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { api } from "./api";
import type { Destination, Priority, ProjectStatus, UpdateStatus } from "./types";

export type ReportSummary = {
  range: { from: string; to: string };
  projects_by_status: { status: ProjectStatus; n: number }[];
  projects_by_member: { id: string; name: string; active: number; blocked: number; overdue: number; completed: number }[];
  overdue: { id: string; number: number; name: string | null; status: ProjectStatus; due: string; days_overdue: number; assignee_name: string | null }[];
  unassigned: { id: string; number: number; name: string | null; priority: Priority; deadline: string; sales_name: string; waiting_since: string }[];
  timing: { assigned_n: number; assign_avg_h: number; assign_median_h: number; completed_n: number; complete_avg_h: number; complete_median_h: number };
  updates_by_destination_status: { destination: Destination; status: UpdateStatus; n: number }[];
  approval_turnaround: { n: number; avg_h: number; median_h: number; sent_back: number };
  revisions: { completed_n: number; avg_revisions: number; with_revisions: number };
  top_revisions: { id: string; number: number; name: string | null; revision_count: number; status: ProjectStatus; assignee_name: string | null }[];
  sales_by_person: { id: string; name: string; created: number; submitted: number; completed: number; active_now: number; revenue: Record<string, string> }[];
};

export function useReportSummary(from: string, to: string) {
  return useQuery({
    queryKey: ["reports", "summary", from, to],
    queryFn: () => api<ReportSummary>(`/reports/summary?from=${from}&to=${to}`),
    placeholderData: (prev) => prev,
  });
}

export type WorkloadMember = {
  id: string;
  name: string;
  team_id: string | null;
  team_name: string | null;
  open_projects: number;
  overdue_projects: number;
  blocked_projects: number;
  open_cards: number;
  late_cards: number;
  completed_30d: number;
  on_leave_today: boolean;
};

export type TeamThroughput = {
  id: string;
  name: string;
  lead_name: string | null;
  members: number;
  live: number;
  unassigned: number;
  overdue: number;
  completed_this_month: number;
};

export type Workload = {
  scope: "team" | "company";
  team: string | null;
  members: WorkloadMember[];
  teams: TeamThroughput[];
};

/** Who is carrying what: a Team Leader's own team, or everyone for the CEO. */
export function useWorkload(enabled = true) {
  return useQuery({ queryKey: ["reports", "workload"], queryFn: () => api<Workload>("/reports/workload"), enabled });
}

export type CompanyKpis = {
  live_projects: number;
  unassigned_projects: number;
  overdue_projects: number;
  blocked_projects: number;
  completed_this_month: number;
  updates_pending_approval: number;
  updates_unsent: number;
  headcount: number;
  checked_in_today: number;
  on_leave_today: number;
};

export type Company = {
  kpis: CompanyKpis;
  teams: TeamThroughput[];
  revenue_this_month: { currency: string; total: string; projects: number }[];
  overdue: ReportSummary["overdue"];
  unassigned: ReportSummary["unassigned"];
  month: string;
};

/** The CEO's company numbers as of now. */
export function useCompany(enabled = true) {
  return useQuery({ queryKey: ["reports", "company"], queryFn: () => api<Company>("/reports/company"), enabled });
}

export type ActivityRow = {
  id: number;
  action: string;
  entity_type: string;
  entity_id: string | null;
  project_id: string | null;
  client_request_id: string | null;
  metadata: Record<string, unknown>;
  created_at: string;
  actor_id: string | null;
  actor_name: string | null;
  project_number: number;
  /** The container's name: a project's, or a client request's title. Empty if it has none. */
  project_name: string;
};

export function useActivity(filter: { type?: string; actor_id?: string; project_id?: string }) {
  const params = new URLSearchParams();
  for (const [k, v] of Object.entries(filter)) if (v) params.set(k, v);
  return useInfiniteQuery({
    queryKey: ["activity", params.toString()],
    initialPageParam: 0,
    queryFn: ({ pageParam }) => {
      const p = new URLSearchParams(params);
      p.set("limit", "50");
      if (pageParam) p.set("before_id", String(pageParam));
      return api<{ activity: ActivityRow[] }>(`/activity?${p}`).then((r) => r.activity);
    },
    getNextPageParam: (last) => (last.length === 50 ? last[last.length - 1]!.id : undefined),
  });
}

/** "36 h" / "2.5 d" — durations in the unit that reads naturally. */
export function formatHours(h: number, n: number) {
  if (!n) return "—";
  if (h < 1) return `${Math.round(h * 60)} min`;
  if (h < 48) return `${h.toFixed(h < 10 ? 1 : 0)} h`;
  return `${(h / 24).toFixed(1)} d`;
}
