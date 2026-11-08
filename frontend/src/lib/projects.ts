"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./api";
import type { DirectoryUser, Priority, ProjectDetail, ProjectList, ProjectStatus, StoreAccess } from "./types";

export type ProjectQuery = {
  status?: string;
  q?: string;
  priority?: string;
  assignee?: string;
  overdue?: boolean;
  sort?: string;
  page?: number;
  page_size?: number;
};

export function useProjects(query: ProjectQuery) {
  const params = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) {
    if (v !== undefined && v !== "" && v !== false) params.set(k, String(v));
  }
  return useQuery({
    queryKey: ["projects", "list", params.toString()],
    queryFn: () => api<ProjectList>(`/projects?${params}`),
    placeholderData: (prev) => prev,
  });
}

export function useProject(id: string) {
  return useQuery({
    queryKey: ["projects", "detail", id],
    queryFn: () => api<ProjectDetail>(`/projects/${id}`),
  });
}

export function useMembers() {
  return useQuery({
    queryKey: ["directory", "member"],
    queryFn: () => api<{ users: DirectoryUser[] }>("/users/directory?role=member").then((r) => r.users),
    staleTime: 60_000,
  });
}

/** Every project mutation returns the fresh detail; seed the cache and refresh lists. */
function useProjectMutation<V>(id: string, fn: (v: V) => Promise<ProjectDetail>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: (detail) => {
      qc.setQueryData(["projects", "detail", id], detail);
      qc.invalidateQueries({ queryKey: ["projects", "list"] });
    },
  });
}

export function useTransition(id: string) {
  return useProjectMutation(id, (v: { to: ProjectStatus; comment?: string }) =>
    api<ProjectDetail>(`/projects/${id}/transition`, { method: "POST", body: v }),
  );
}

export function useAssign(id: string) {
  return useProjectMutation(id, (v: { assignee_ids: string[]; priority: Priority; due_date: string | null }) =>
    api<ProjectDetail>(`/projects/${id}/assign`, { method: "POST", body: v }),
  );
}

/** Hands the project to another team; a lead who doesn't run that team loses sight of it. */
export function useMoveTeam(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { team_id: string; comment: string }) =>
      api<ProjectDetail | { moved: true; visible: false }>(`/projects/${id}/team`, { method: "POST", body: v }),
    // When it's out of sight the page navigates away; the stale detail refetches if reopened.
    onSuccess: (res) => {
      if ("project" in res) qc.setQueryData(["projects", "detail", id], res);
      qc.invalidateQueries({ queryKey: ["projects", "list"] });
    },
  });
}

export function usePlan(id: string) {
  return useProjectMutation(id, (v: { priority: Priority; due_date: string | null }) =>
    api<ProjectDetail>(`/projects/${id}/planning`, { method: "PUT", body: v }),
  );
}

export function useStoreAccess(id: string) {
  return useProjectMutation(id, (v: { status: StoreAccess; link?: string }) =>
    api<ProjectDetail>(`/projects/${id}/store-access`, { method: "PUT", body: v }),
  );
}

export function useComment(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: string) => api(`/projects/${id}/comments`, { method: "POST", body: { body } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["projects", "detail", id] }),
  });
}

export function useAcknowledge(id: string) {
  return useProjectMutation(id, () => api<ProjectDetail>(`/projects/${id}/acknowledge`, { method: "POST" }));
}

export function useRemoveFile(id: string) {
  return useProjectMutation(id, (fileId: string) => api<ProjectDetail>(`/projects/${id}/files/${fileId}`, { method: "DELETE" }));
}

/** Records the visit; resolves to when the user last opened the project before now. */
export function useMarkViewed(id: string) {
  const qc = useQueryClient();
  return useQuery({
    queryKey: ["projects", "viewed", id],
    queryFn: async () => {
      const r = await api<{ previous_viewed_at: string | null }>(`/projects/${id}/view`, { method: "POST" });
      qc.invalidateQueries({ queryKey: ["projects", "list"] });
      return r.previous_viewed_at;
    },
    staleTime: Infinity,
    gcTime: 0,
  });
}
