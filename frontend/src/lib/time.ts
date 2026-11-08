"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "./api";

export type TimeEntry = {
  id: string;
  user_id: string;
  user_name: string | null;
  project_id: string | null;
  project_name: string | null;
  description: string;
  started_at: string;
  ended_at: string | null;
  duration_minutes: number | null;
  created_at: string;
};

export type TimeListResponse = {
  entries: TimeEntry[];
  today_total: number;
  week_total: number;
  can_view_all: boolean;
};

export type RunningTimer = { running: TimeEntry | null };

const LIST_KEY = ["time", "entries"];
const RUNNING_KEY = ["time", "running"];

export function useTimeEntries(filters: { user_id?: string; project_id?: string; from?: string; to?: string }) {
  const params = new URLSearchParams();
  if (filters.user_id) params.set("user_id", filters.user_id);
  if (filters.project_id) params.set("project_id", filters.project_id);
  if (filters.from) params.set("from", filters.from);
  if (filters.to) params.set("to", filters.to);
  const qs = params.toString();
  return useQuery({
    queryKey: [...LIST_KEY, filters],
    queryFn: () => api<TimeListResponse>(`/time/entries${qs ? `?${qs}` : ""}`),
  });
}

export function useRunningTimer() {
  return useQuery({
    queryKey: RUNNING_KEY,
    queryFn: () => api<RunningTimer>("/time/running"),
    refetchInterval: 30_000,
  });
}

export function useStartTimer() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { project_id?: string; description: string }) =>
      api<{ id: string }>("/time/start", { method: "POST", body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: RUNNING_KEY });
      qc.invalidateQueries({ queryKey: LIST_KEY });
      toast.success("Timer started");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useStopTimer() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api<{ ok: boolean }>("/time/stop", { method: "POST" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: RUNNING_KEY });
      qc.invalidateQueries({ queryKey: LIST_KEY });
      toast.success("Timer stopped");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useDeleteTimeEntry() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api<{ ok: boolean }>(`/time/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: LIST_KEY });
      toast.success("Entry deleted");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

/** Format minutes as "2h 15m" or "45m". */
export function formatDuration(minutes: number | null | undefined): string {
  if (minutes == null) return "—";
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  if (h === 0) return `${m}m`;
  return `${h}h ${m}m`;
}

/** Live elapsed minutes between an ISO start and now. */
export function elapsedMinutes(startedAt: string): number {
  return Math.max(0, Math.floor((Date.now() - new Date(startedAt).getTime()) / 60000));
}
