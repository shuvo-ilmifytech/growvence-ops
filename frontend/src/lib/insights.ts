"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "./api";

export type ForecastMonth = {
  month: string; // "2026-11"
  projected: number;
  currency: string;
  count: number;
};

export type RevenueForecast = {
  months: ForecastMonth[];
  total_pipeline: number;
  currency: string;
};

export type LeaderboardRow = {
  user_id: string;
  name: string;
  role: string;
  completed: number;
  on_time_pct: number | null;
  active: number;
  avg_rating: number | null;
};

export function useRevenueForecast() {
  return useQuery({
    queryKey: ["insights", "revenue-forecast"],
    queryFn: () => api<RevenueForecast>("/insights/revenue-forecast"),
    staleTime: 60_000,
  });
}

export function useLeaderboard() {
  return useQuery({
    queryKey: ["insights", "leaderboard"],
    queryFn: () => api<LeaderboardRow[]>("/insights/leaderboard"),
    staleTime: 60_000,
  });
}
