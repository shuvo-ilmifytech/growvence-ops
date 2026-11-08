"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "./api";

export type KbArticle = {
  id: string;
  title: string;
  slug: string;
  content: string;
  category: string;
  created_by: string | null;
  updated_by: string | null;
  author_name?: string | null;
  editor_name?: string | null;
  created_at: string;
  updated_at: string;
  is_published: boolean;
};

export type KbListResponse = {
  articles: KbArticle[];
  categories: string[];
};

const LIST_KEY = ["kb", "articles"];

export function useKbArticles(opts: { category?: string; search?: string } = {}) {
  const params = new URLSearchParams();
  if (opts.category) params.set("category", opts.category);
  if (opts.search) params.set("search", opts.search);
  const qs = params.toString();
  return useQuery({
    queryKey: [...LIST_KEY, opts.category ?? "", opts.search ?? ""],
    queryFn: () => api<KbListResponse>(`/kb/articles${qs ? `?${qs}` : ""}`),
  });
}

export function useKbArticle(slug: string) {
  return useQuery({
    queryKey: ["kb", "article", slug],
    queryFn: () => api<KbArticle>(`/kb/articles/${encodeURIComponent(slug)}`),
    enabled: !!slug,
  });
}

export type KbArticleInput = {
  title: string;
  content: string;
  category: string;
  slug?: string;
  is_published?: boolean;
};

export function useCreateKbArticle() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: KbArticleInput) => api<KbArticle>("/kb/articles", { method: "POST", body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: LIST_KEY });
      toast.success("Article created");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useUpdateKbArticle() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...input }: KbArticleInput & { id: string }) =>
      api<KbArticle>(`/kb/articles/${id}`, { method: "PUT", body: input }),
    onSuccess: (article) => {
      qc.invalidateQueries({ queryKey: LIST_KEY });
      qc.invalidateQueries({ queryKey: ["kb", "article", article.slug] });
      toast.success("Article updated");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useDeleteKbArticle() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api<void>(`/kb/articles/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: LIST_KEY });
      toast.success("Article deleted");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

/** Whether the current user can curate the knowledge base (create/edit/delete). */
export function canEditKb(role: string | undefined, isAdmin: boolean | undefined): boolean {
  return !!isAdmin || role === "tl" || role === "hr" || role === "ceo";
}
