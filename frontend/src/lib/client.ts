"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./api";

export type ClientProject = {
  id: string;
  number: number;
  name: string;
  status: string;
  priority: string;
  due_date: string | null;
  updated_at: string;
};

export type ClientProjectDetail = {
  id: string;
  number: number;
  name: string;
  status: string;
  priority: string;
  scope: string;
  due_date: string | null;
};

export type ClientUpdate = {
  id: string;
  number: number;
  message: string;
  sent_at: string;
  created_at: string;
};

export type ClientFeedback = {
  id: string;
  body: string;
  created_at: string;
  mine: boolean;
};

export function useClientProjects() {
  return useQuery({
    queryKey: ["client", "projects"],
    queryFn: () => api<{ projects: ClientProject[] }>("/client/projects"),
  });
}

export function useClientProject(id: string) {
  return useQuery({
    queryKey: ["client", "project", id],
    queryFn: () => api<ClientProjectDetail>(`/client/projects/${id}`),
    enabled: !!id,
  });
}

export function useClientUpdates(id: string) {
  return useQuery({
    queryKey: ["client", "updates", id],
    queryFn: () => api<{ updates: ClientUpdate[] }>(`/client/projects/${id}/updates`),
    enabled: !!id,
  });
}

export function useClientFeedback(id: string) {
  return useQuery({
    queryKey: ["client", "feedback", id],
    queryFn: () => api<{ feedback: ClientFeedback[] }>(`/client/projects/${id}/feedback`),
    enabled: !!id,
  });
}

export function useSubmitFeedback(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: string) =>
      api<{ id: string }>(`/client/projects/${projectId}/feedback`, {
        method: "POST",
        body: { body },
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["client", "feedback", projectId] });
    },
  });
}
