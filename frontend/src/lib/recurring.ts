"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "./api";

export type RecurringTask = {
  id: string;
  title: string;
  description: string;
  project_id: string | null;
  project_name: string | null;
  frequency: "daily" | "weekly" | "monthly";
  day_of_week: number | null;
  day_of_month: number | null;
  next_run: string;
  last_run: string | null;
  is_active: boolean;
  created_by: string | null;
  creator_name: string | null;
  created_at: string;
};

export type RecurringListResponse = {
  recurring_tasks: RecurringTask[];
};

export type RecurringInput = {
  title: string;
  description: string;
  project_id: string | null;
  frequency: "daily" | "weekly" | "monthly";
  day_of_week: number | null;
  day_of_month: number | null;
  next_run: string;
};

const LIST_KEY = ["recurring", "list"];

export function useRecurringTasks() {
  return useQuery({
    queryKey: LIST_KEY,
    queryFn: () => api<RecurringListResponse>("/recurring"),
  });
}

export function useCreateRecurring() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: RecurringInput) =>
      api<{ id: string }>("/recurring", { method: "POST", body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: LIST_KEY });
      toast.success("Recurring task created");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useUpdateRecurring() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...input }: RecurringInput & { id: string }) =>
      api(`/recurring/${id}`, { method: "PUT", body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: LIST_KEY });
      toast.success("Recurring task updated");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useDeleteRecurring() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api(`/recurring/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: LIST_KEY });
      toast.success("Recurring task deleted");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useToggleRecurring() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      api<{ is_active: boolean }>(`/recurring/${id}/toggle`, { method: "POST" }),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: LIST_KEY });
      toast.success(r.is_active ? "Recurring task activated" : "Recurring task paused");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useRunRecurringNow() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api(`/recurring/${id}/run-now`, { method: "POST" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: LIST_KEY });
      toast.success("Task ran — card created");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export const FREQUENCY_LABELS: Record<string, string> = {
  daily: "Daily",
  weekly: "Weekly",
  monthly: "Monthly",
};

export const DAY_NAMES = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
