"use client";

import { useEffect } from "react";
import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "./api";
import type { AllCards, Board, CardColumn, CardView } from "./types";

const boardKey = (containerId: string) => ["projects", "board", containerId];
const ALL_KEY = ["cards-all"];
const cardKey = (id: string) => ["cards", id];

/**
 * A card changed: refresh the board it is on (its project's, or its client request's)
 * and the all-projects board. containerId is the card's project_id or client_request_id.
 */
function refresh(qc: QueryClient, containerId: string) {
  qc.invalidateQueries({ queryKey: boardKey(containerId) });
  qc.invalidateQueries({ queryKey: ["client-requests", containerId] });
  qc.invalidateQueries({ queryKey: ALL_KEY });
}

export function useBoard(projectId: string) {
  return useQuery({ queryKey: boardKey(projectId), queryFn: () => api<Board>(`/projects/${projectId}/cards`) });
}

export type AllCardsQuery = { project?: string; request?: string; team?: string; mine?: boolean; source?: string; q?: string };

/** Every card across the user's projects. */
export function useAllCards(query: AllCardsQuery) {
  const params = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) if (v !== undefined && v !== "" && v !== false) params.set(k, String(v));
  return useQuery({
    queryKey: [...ALL_KEY, params.toString()],
    queryFn: () => api<AllCards>(`/cards?${params}`),
    placeholderData: (prev) => prev,
  });
}

/**
 * Records that this person has opened the card, which stops the "unopened task"
 * alert ringing. Fire and forget: a failure here must not break opening a card.
 */
export function useMarkCardOpened(id: string | null) {
  const qc = useQueryClient();
  useEffect(() => {
    if (!id) return;
    let live = true;
    api(`/cards/${id}/opened`, { method: "POST" })
      .then(() => {
        if (live) qc.invalidateQueries({ queryKey: ["alerts"] });
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [id, qc]);
}

export function useCard(id: string | null) {
  return useQuery({ queryKey: cardKey(id ?? ""), queryFn: () => api<CardView>(`/cards/${id}`), enabled: !!id });
}

/** Card mutations return the fresh card: seed it and refresh the boards around it. */
function useCardMutation<V>(projectId: string, fn: (v: V) => Promise<CardView | unknown>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: (res) => {
      const view = res as CardView | undefined;
      if (view?.card) qc.setQueryData(cardKey(view.card.id), view);
      refresh(qc, projectId);
    },
  });
}

/** Creates the card, then uploads any files the client sent with it. */
export function useCreateCard() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({
      projectId,
      isRequest,
      files = [],
      ...body
    }: {
      projectId: string;
      /** The container is a client request, not a project. */
      isRequest?: boolean;
      title: string;
      description: string;
      due_date: string | null;
      assignee_ids?: string[];
      files?: File[];
    }) => {
      const where = isRequest ? `/client-requests/${projectId}/cards` : `/projects/${projectId}/cards`;
      const { card } = await api<{ card: { id: string } }>(where, { method: "POST", body });
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
    onSuccess: (_, v) => refresh(qc, v.projectId),
  });
}

type Ticket = { upload_id: string; url: string; method: string; headers: Record<string, string> };

/** Presign, PUT straight to storage, then confirm (same flow as project files). */
export async function uploadCardAttachment(cardId: string, file: File, onProgress?: (f: number) => void) {
  const contentType = file.type || "application/octet-stream";
  const ticket = await api<Ticket>(`/cards/${cardId}/attachments/uploads`, {
    method: "POST",
    body: { filename: file.name, size: file.size, content_type: contentType },
  });
  await new Promise<void>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(ticket.method, ticket.url);
    for (const [k, v] of Object.entries(ticket.headers)) xhr.setRequestHeader(k, v);
    xhr.upload.onprogress = (e) => e.lengthComputable && onProgress?.(e.loaded / e.total);
    xhr.onload = () => (xhr.status >= 200 && xhr.status < 300 ? resolve() : reject(new Error(`Upload failed (${xhr.status})`)));
    xhr.onerror = () => reject(new Error("Upload failed. Check your connection and try again."));
    xhr.send(file);
  });
  return api<CardView>(`/cards/${cardId}/attachments/uploads/${ticket.upload_id}/complete`, { method: "POST" });
}

export function useAttachToCard(projectId: string, id: string) {
  return useCardMutation(projectId, (file: File) => uploadCardAttachment(id, file));
}

export function useRemoveCardAttachment(projectId: string, id: string) {
  return useCardMutation(projectId, (fileId: string) => api<CardView>(`/cards/${id}/attachments/${fileId}`, { method: "DELETE" }));
}

export function useUpdateCard(projectId: string, id: string) {
  return useCardMutation(projectId, (v: { title?: string; description?: string; due_date?: string }) =>
    api<CardView>(`/cards/${id}`, { method: "PATCH", body: v }),
  );
}

export function useMoveCard() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; projectId: string; column: CardColumn; index: number }) =>
      api<CardView>(`/cards/${v.id}/move`, { method: "POST", body: { column: v.column, index: v.index } }),
    onSuccess: (view) => qc.setQueryData(cardKey(view.card.id), view),
    // Always re-read: on success for the server's order, on failure to undo the drag.
    onSettled: (_, __, v) => refresh(qc, v.projectId),
  });
}

export function useCardAssignees(projectId: string, id: string) {
  return useCardMutation(projectId, (assignee_ids: string[]) =>
    api<CardView>(`/cards/${id}/assignees`, { method: "PUT", body: { assignee_ids } }),
  );
}

export function useCardComment(projectId: string, id: string) {
  return useCardMutation(projectId, (body: string) => api<CardView>(`/cards/${id}/comments`, { method: "POST", body: { body } }));
}

export function useRemoveCard(projectId: string) {
  return useCardMutation(projectId, (id: string) => api(`/cards/${id}`, { method: "DELETE" }));
}
