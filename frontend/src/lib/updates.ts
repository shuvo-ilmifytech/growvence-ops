"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./api";
import type { Destination, UpdateDetail, UpdateList } from "./types";

export type UpdateQuery = {
  status?: string;
  destination?: string;
  q?: string;
  project_id?: string;
  client_request_id?: string;
  page?: number;
  page_size?: number;
};

export function useUpdates(query: UpdateQuery, enabled = true) {
  const params = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) if (v !== undefined && v !== "") params.set(k, String(v));
  return useQuery({
    queryKey: ["updates", "list", params.toString()],
    queryFn: () => api<UpdateList>(`/updates?${params}`),
    placeholderData: (prev) => prev,
    enabled,
  });
}

export function useUpdate(id: string | null) {
  return useQuery({
    queryKey: ["updates", "detail", id],
    queryFn: () => api<UpdateDetail>(`/updates/${id}`),
    enabled: !!id,
  });
}

/** Every update mutation returns the fresh detail; seed it and refresh lists and the project. */
export function useUpdateMutation<V>(id: string, fn: (v: V) => Promise<UpdateDetail>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: (d) => {
      qc.setQueryData(["updates", "detail", id], d);
      qc.invalidateQueries({ queryKey: ["updates", "list"] });
      qc.invalidateQueries({ queryKey: ["projects"] });
      qc.invalidateQueries({ queryKey: ["client-requests"] });
    },
  });
}

/**
 * Starts a draft update on a project, or on a client request (a client with no project).
 * From there both run the same flow.
 */
export function useCreateUpdate(containerId: string, isRequest = false) {
  const qc = useQueryClient();
  const where = isRequest ? `/client-requests/${containerId}/updates` : `/projects/${containerId}/updates`;
  return useMutation({
    mutationFn: (v: { destination: Destination; message?: string }) =>
      api<UpdateDetail>(where, { method: "POST", body: v }),
    onSuccess: (d) => {
      qc.setQueryData(["updates", "detail", d.update.id], d);
      qc.invalidateQueries({ queryKey: ["updates", "list"] });
    },
  });
}

type Ticket = { upload_id: string; url: string; method: string; headers: Record<string, string> };

export async function uploadUpdateAttachment(updateId: string, file: File, onProgress?: (f: number) => void) {
  const contentType = file.type || "application/octet-stream";
  const ticket = await api<Ticket>(`/updates/${updateId}/attachments/uploads`, {
    method: "POST",
    body: { filename: file.name, size: file.size, content_type: contentType },
  });
  await new Promise<void>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(ticket.method, ticket.url);
    for (const [k, v] of Object.entries(ticket.headers)) xhr.setRequestHeader(k, v);
    xhr.upload.onprogress = (e) => e.lengthComputable && onProgress?.(e.loaded / e.total);
    xhr.onload = () => (xhr.status < 300 ? resolve() : reject(new Error(`Upload failed (${xhr.status})`)));
    xhr.onerror = () => reject(new Error("Upload failed. Check your connection and try again."));
    xhr.send(file);
  });
  return api<UpdateDetail>(`/updates/${updateId}/attachments/uploads/${ticket.upload_id}/complete`, { method: "POST" });
}
