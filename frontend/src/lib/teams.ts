"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "./api";
import type { DirectoryUser, Team, TeamMember } from "./types";

export const TEAMS_KEY = ["teams"];

/** Teams for pickers and display; `all` includes retired ones. */
export function useTeams(all = false) {
  return useQuery({
    queryKey: [...TEAMS_KEY, all ? "all" : "active"],
    queryFn: () => api<{ teams: Team[] }>(`/teams${all ? "?all=true" : ""}`).then((r) => r.teams),
    staleTime: 60_000,
  });
}

export function useTeam(id: string | null) {
  return useQuery({
    queryKey: [...TEAMS_KEY, "detail", id],
    queryFn: () => api<{ team: Team; members: TeamMember[] }>(`/admin/teams/${id}`),
    enabled: !!id,
  });
}

export function useDirectoryByRole(role: "tl" | "member") {
  return useQuery({
    queryKey: ["directory", role],
    queryFn: () => api<{ users: DirectoryUser[] }>(`/users/directory?role=${role}`).then((r) => r.users),
    staleTime: 60_000,
  });
}

/** Every active user with their team, for the org chart. */
export function useUserDirectory() {
  return useQuery({
    queryKey: ["directory", "all"],
    queryFn: () => api<{ users: DirectoryUser[] }>("/users/directory").then((r) => r.users),
    staleTime: 60_000,
  });
}
