"use client";

import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "./api";
import { uploadCardAttachment } from "./cards";
import type { RequestDetail, RequestList, RequestStatus } from "./types";

const LIST_KEY = ["client-requests"];
const detailKey = (id: string) => ["client-requests", id];

/** A request changed: refresh it, the list, and the boards its cards show on. */
function refresh(qc: QueryClient, id?: string) {
  if (id) qc.invalidateQueries({ queryKey: detailKey(id) });
  qc.invalidateQueries({ queryKey: LIST_KEY });
  qc.invalidateQueries({ queryKey: ["cards-all"] });
  qc.invalidateQueries({ queryKey: ["updates"] });
}

export type RequestQuery = { status?: string; team?: string; mine?: boolean; q?: string; page?: number };

export function useRequests(query: RequestQuery) {
  const params = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) if (v !== undefined && v !== "" && v !== false) params.set(k, String(v));
  return useQuery({
    queryKey: [...LIST_KEY, params.toString()],
    queryFn: () => api<RequestList>(`/client-requests?${params}`),
    placeholderData: (prev) => prev,
  });
}

export function useRequest(id: string | null) {
  return useQuery({
    queryKey: detailKey(id ?? ""),
    queryFn: () => api<RequestDetail>(`/client-requests/${id}`),
    enabled: !!id,
  });
}

export type RaiseBody = {
  profile_id: string;
  client_name: string;
  order_id: string | null;
  title: string;
  details: string;
  team_id: string;
};

/** Raises the request, then uploads whatever the client sent with it onto its card. */
export function useRaiseRequest() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ files = [], ...body }: RaiseBody & { files?: File[] }) => {
      const res = await api<RequestDetail>("/client-requests", { method: "POST", body });
      const card = res.cards[0];
      const failed: string[] = [];
      for (const f of files) {
        try {
          if (card) await uploadCardAttachment(card.id, f);
        } catch {
          failed.push(f.name);
        }
      }
      return { id: res.request.id, failed };
    },
    onSuccess: (res) => refresh(qc, res.id),
  });
}

export function useEditRequest(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Omit<RaiseBody, "team_id">) =>
      api<RequestDetail>(`/client-requests/${id}`, { method: "PATCH", body }),
    onSuccess: () => refresh(qc, id),
  });
}

/**
 * Hands the request to another team. A leader who moves it to a team they don't run can
 * no longer see it, and the answer says so ({ moved: true, visible: false }).
 */
export function useMoveRequestTeam(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { team_id: string; reason: string }) =>
      api<RequestDetail | { moved: true; visible: false }>(`/client-requests/${id}/team`, { method: "POST", body }),
    onSuccess: () => refresh(qc, id),
  });
}

export function useSetRequestStatus(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { status: RequestStatus; reason?: string }) =>
      api<RequestDetail>(`/client-requests/${id}/status`, { method: "POST", body }),
    onSuccess: () => refresh(qc, id),
  });
}

/** Adds a card to a request: a follow-up ask (Sales) or a task for the team (the leader). */
export function useAddRequestCard(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({
      files = [],
      ...body
    }: { title: string; description: string; due_date: string | null; assignee_ids?: string[]; files?: File[] }) => {
      const { card } = await api<{ card: { id: string } }>(`/client-requests/${id}/cards`, { method: "POST", body });
      const failed: string[] = [];
      for (const f of files) {
        try {
          await uploadCardAttachment(card.id, f);
        } catch {
          failed.push(f.name);
        }
      }
      return { id: card.id, failed };
    },
    onSuccess: () => refresh(qc, id),
  });
}

/** Starts a client update on the request; from there it is an ordinary update. */
export function useCreateRequestUpdate(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api<{ update: { id: string } }>(`/client-requests/${id}/updates`, { method: "POST", body: { destination: "inbox" } }),
    onSuccess: () => refresh(qc, id),
  });
}
