"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "./api";

export type DeadlineItem = {
  id: string;
  number: number;
  name: string;
  status: string;
  due_date: string | null;
  days_until_due: number;
  assignee_id: string | null;
  assignee_name: string | null;
  team_name: string | null;
  urgency: "overdue" | "today" | "soon" | "upcoming";
};

export type DeadlinesResponse = {
  overdue: DeadlineItem[];
  due_soon: DeadlineItem[];
  overdue_count: number;
  due_week_count: number;
};

export function useDeadlines() {
  return useQuery({
    queryKey: ["alerts", "deadlines"],
    queryFn: () => api<DeadlinesResponse>("/alerts/deadlines"),
    staleTime: 60_000,
  });
}
