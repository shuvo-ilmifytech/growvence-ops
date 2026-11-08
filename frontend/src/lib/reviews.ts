"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "./api";

export type Review = {
  id: string;
  order_id: string;
  project_id: string | null;
  project_name: string | null;
  member_id: string | null;
  member_name: string | null;
  rating: number;
  review_text: string;
  reviewer_name: string;
  reviewed_at: string;
  created_at: string;
};

export type ReviewStats = {
  avg_rating: number;
  total: number;
  by_stars: Record<string, number>;
  five_star_pct: number;
  trend: { month: string; count: number; avg_rating: number }[];
  top_members: { name: string; count: number; avg_rating: number }[];
};

export type ReviewInput = {
  order_id: string;
  project_id?: string;
  member_id?: string;
  rating: number;
  review_text: string;
  reviewer_name: string;
  reviewed_at: string;
};

const LIST_KEY = ["reviews", "list"];
const STATS_KEY = ["reviews", "stats"];

export function useReviews(filters: { rating?: number; member_id?: string; q?: string }) {
  const params = new URLSearchParams();
  if (filters.rating) params.set("rating", String(filters.rating));
  if (filters.member_id) params.set("member_id", filters.member_id);
  if (filters.q) params.set("q", filters.q);
  const qs = params.toString();
  return useQuery({
    queryKey: [...LIST_KEY, filters],
    queryFn: () => api<{ reviews: Review[] }>(`/reviews${qs ? `?${qs}` : ""}`),
  });
}

export function useReviewStats() {
  return useQuery({
    queryKey: STATS_KEY,
    queryFn: () => api<ReviewStats>("/reviews/stats"),
  });
}

export function useCreateReview() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ReviewInput) => api<{ id: string }>("/reviews", { method: "POST", body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: LIST_KEY });
      qc.invalidateQueries({ queryKey: STATS_KEY });
      toast.success("Review added");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useUpdateReview() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: ReviewInput }) =>
      api(`/reviews/${id}`, { method: "PUT", body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: LIST_KEY });
      qc.invalidateQueries({ queryKey: STATS_KEY });
      toast.success("Review updated");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useDeleteReview() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api(`/reviews/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: LIST_KEY });
      qc.invalidateQueries({ queryKey: STATS_KEY });
      toast.success("Review deleted");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}
