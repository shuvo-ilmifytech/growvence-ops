"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "./api";

export type TemplateTask = {
  title: string;
  description: string;
  estimated_days: number;
};

export type ProjectTemplate = {
  id: string;
  name: string;
  description: string;
  category: string;
  tasks: TemplateTask[];
  created_by: string | null;
  creator_name?: string | null;
  created_at: string;
  updated_at: string;
};

export type TemplateListResponse = {
  templates: ProjectTemplate[];
  categories: string[];
};

const LIST_KEY = ["templates", "list"];

export function useTemplates(category?: string) {
  const qs = category ? `?category=${encodeURIComponent(category)}` : "";
  return useQuery({
    queryKey: [...LIST_KEY, category ?? ""],
    queryFn: () => api<TemplateListResponse>(`/templates${qs}`),
  });
}

export function useTemplate(id: string) {
  return useQuery({
    queryKey: ["templates", "detail", id],
    queryFn: () => api<ProjectTemplate>(`/templates/${id}`),
    enabled: !!id,
  });
}

export type TemplateInput = {
  name: string;
  description: string;
  category: string;
  tasks: TemplateTask[];
};

export function useCreateTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: TemplateInput) =>
      api<ProjectTemplate>("/templates", { method: "POST", body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: LIST_KEY });
      toast.success("Template created");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useUpdateTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...input }: TemplateInput & { id: string }) =>
      api<ProjectTemplate>(`/templates/${id}`, { method: "PUT", body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: LIST_KEY });
      toast.success("Template updated");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useDeleteTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api(`/templates/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: LIST_KEY });
      toast.success("Template deleted");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useApplyTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, project }: { id: string; project: Record<string, unknown> }) =>
      api<{ project_id: string; cards_created: number }>(`/templates/${id}/apply`, {
        method: "POST",
        body: { project },
      }),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: ["projects"] });
      toast.success(`Project created with ${r.cards_created} task cards`);
    },
    onError: (e: Error) => toast.error(e.message),
  });
}
