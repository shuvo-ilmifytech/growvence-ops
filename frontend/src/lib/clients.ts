"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./api";
import type { CardColumn } from "./types";

// Client collaboration (Phase 4): business clients, shared task comments +
// approvals, per-project messages and shared files.

export type ClientType = "B2B" | "Individual";
export type ClientStatus = "active" | "inactive";

export type BusinessClient = {
  id: string;
  client_code: string;
  company_name: string;
  contact_person: string | null;
  email: string | null;
  phone: string | null;
  address: string | null;
  type: ClientType;
  status: ClientStatus;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  project_count: number;
};

export type ClientContact = {
  id: string;
  client_id: string;
  name: string;
  email: string | null;
  phone: string | null;
  role: string | null;
  is_primary: boolean;
  created_at: string;
};

export type ProjectClient = {
  id: string;
  project_id: string;
  client_id: string;
  created_at: string;
  client_code: string;
  company_name: string;
  contact_person: string | null;
  email: string | null;
  type: ClientType;
  status: ClientStatus;
};

export type ClientProjectLink = {
  id: string;
  project_id: string;
  client_id: string;
  created_at: string;
  number: number;
  name: string | null;
  status: string;
};

export type ClientDetail = {
  client: BusinessClient;
  contacts: ClientContact[];
  projects: ClientProjectLink[];
};

export type TaskComment = {
  id: string;
  card_id: string;
  author_id: string | null;
  body: string;
  is_internal: boolean;
  created_at: string;
  updated_at: string;
  author_name: string | null;
  author_role: string | null;
};

export type ApprovalStatus = "pending" | "approved" | "changes_requested";

export type TaskApproval = {
  id: string;
  card_id: string;
  requested_by: string | null;
  requested_at: string;
  status: ApprovalStatus;
  reviewed_by: string | null;
  reviewed_at: string | null;
  review_note: string;
  requested_by_name: string | null;
  reviewed_by_name: string | null;
};

export type ClientMessage = {
  id: string;
  project_id: string;
  author_id: string | null;
  body: string;
  created_at: string;
  author_name: string | null;
  author_role: string | null;
};

export type ClientFile = {
  id: string;
  project_id: string;
  client_id: string | null;
  uploaded_by: string | null;
  file_name: string;
  file_url: string;
  file_size: number;
  is_client_upload: boolean;
  uploaded_at: string;
  uploaded_by_name: string | null;
  client_code: string | null;
};

export type CollabCard = {
  id: string;
  project_id: string | null;
  client_request_id: string | null;
  container_id: string;
  number: number;
  title: string;
  description: string;
  source: string;
  column_key: CardColumn;
  position: number;
  visible_to_client: boolean;
  due_date: string | null;
  created_by: string;
  created_by_name: string;
  created_at: string;
  updated_at: string;
  done_at: string | null;
  assignee_ids: string[];
  assignee_names: string[];
  comment_count: number;
  attachment_count: number;
  approval_status: ApprovalStatus | null;
  approval_requested_at: string | null;
};

export type ProjectCollab = {
  clients: ProjectClient[];
  messages: ClientMessage[];
  files: ClientFile[];
};

export type ClientCollab = {
  cards: CollabCard[];
  messages: ClientMessage[];
  files: ClientFile[];
};

export type ClientInput = {
  company_name: string;
  contact_person: string;
  email: string;
  phone: string;
  address: string;
  type: ClientType;
};

export type ContactInput = {
  name: string;
  email: string;
  phone: string;
  role: string;
  is_primary: boolean;
};

// ── Clients ──────────────────────────────────────────────────

export function useClients() {
  return useQuery({
    queryKey: ["clients"],
    queryFn: () => api<{ clients: BusinessClient[] }>("/clients"),
  });
}

export function useClient(id: string) {
  return useQuery({
    queryKey: ["clients", id],
    queryFn: () => api<ClientDetail>(`/clients/${id}`),
    enabled: !!id,
  });
}

export function useCreateClient() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ClientInput) =>
      api<{ id: string; client_code: string }>("/clients", { method: "POST", body: input }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["clients"] }),
  });
}

export function useUpdateClient(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ClientInput & { status: ClientStatus }) =>
      api<{ ok: boolean }>(`/clients/${id}`, { method: "PUT", body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["clients"] });
      qc.invalidateQueries({ queryKey: ["clients", id] });
    },
  });
}

export function useAddContact(clientId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ContactInput) =>
      api<{ id: string }>(`/clients/${clientId}/contacts`, { method: "POST", body: input }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["clients", clientId] }),
  });
}

export function useDeleteContact(clientId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (contactId: string) =>
      api<{ ok: boolean }>(`/clients/${clientId}/contacts/${contactId}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["clients", clientId] }),
  });
}

// ── Project collaboration (team side) ────────────────────────

export function useProjectCollab(projectId: string) {
  return useQuery({
    queryKey: ["collab", projectId],
    queryFn: () => api<ProjectCollab>(`/projects/${projectId}/collab`),
    enabled: !!projectId,
  });
}

export function useLinkClient(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (clientId: string) =>
      api<{ ok: boolean }>(`/projects/${projectId}/link-client`, {
        method: "POST",
        body: { client_id: clientId },
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["collab", projectId] });
      qc.invalidateQueries({ queryKey: ["clients"] });
    },
  });
}

export function useUnlinkClient(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (clientId: string) =>
      api<{ ok: boolean }>(`/projects/${projectId}/link-client/${clientId}`, { method: "DELETE" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["collab", projectId] });
      qc.invalidateQueries({ queryKey: ["clients"] });
    },
  });
}

// ── Task comments + visibility + approvals ───────────────────

function commentsBase(projectId: string, portal: boolean) {
  return portal ? `/client/projects/${projectId}` : `/projects/${projectId}`;
}

export function useTaskComments(projectId: string, cardId: string | null, portal = false) {
  return useQuery({
    queryKey: ["task-comments", portal ? "client" : "team", projectId, cardId],
    queryFn: () =>
      api<{ comments: TaskComment[] }>(
        `${commentsBase(projectId, portal)}/cards/${cardId}/comments`,
      ),
    enabled: !!projectId && !!cardId,
  });
}

export function useAddTaskComment(projectId: string, cardId: string, portal = false) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { body: string; is_internal?: boolean }) =>
      api<{ id: string; created_at: string }>(
        `${commentsBase(projectId, portal)}/cards/${cardId}/comments`,
        { method: "POST", body: input },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["task-comments", portal ? "client" : "team", projectId, cardId] });
      // The portal's card list carries the approval badge, not the comments.
      qc.invalidateQueries({ queryKey: ["collab", "client", projectId] });
    },
  });
}

export function useSetCardVisible(projectId: string, cardId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (visible: boolean) =>
      api<{ ok: boolean; visible_to_client: boolean }>(
        `/projects/${projectId}/cards/${cardId}/visible`,
        { method: "PUT", body: { visible } },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["cards", cardId] });
      qc.invalidateQueries({ queryKey: ["projects", "board", projectId] });
      qc.invalidateQueries({ queryKey: ["collab", "client", projectId] });
    },
  });
}

export function useTaskApprovals(projectId: string, cardId: string | null) {
  return useQuery({
    queryKey: ["task-approvals", projectId, cardId],
    queryFn: () =>
      api<{ approvals: TaskApproval[] }>(
        `/projects/${projectId}/cards/${cardId}/approvals`,
      ),
    enabled: !!projectId && !!cardId,
  });
}

export function useRequestApproval(projectId: string, cardId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () =>
      api<{ id: string }>(`/projects/${projectId}/cards/${cardId}/request-approval`, {
        method: "POST",
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["cards", cardId] });
      qc.invalidateQueries({ queryKey: ["collab", "client", projectId] });
      qc.invalidateQueries({ queryKey: ["task-approvals", projectId, cardId] });
    },
  });
}

export function useReviewApproval(projectId: string, cardId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { status: "approved" | "changes_requested"; note: string }) =>
      api<{ ok: boolean; status: string }>(
        `/client/projects/${projectId}/cards/${cardId}/review`,
        { method: "POST", body: input },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["collab", "client", projectId] });
      qc.invalidateQueries({ queryKey: ["task-comments", "client", projectId, cardId] });
    },
  });
}

// ── Messages ─────────────────────────────────────────────────

function messagesBase(projectId: string, portal: boolean) {
  return portal ? `/client/projects/${projectId}/messages` : `/projects/${projectId}/messages`;
}

export function useClientMessages(projectId: string, portal = false) {
  return useQuery({
    queryKey: ["collab-messages", portal ? "client" : "team", projectId],
    queryFn: () => api<{ messages: ClientMessage[] }>(messagesBase(projectId, portal)),
    enabled: !!projectId,
  });
}

export function useAddClientMessage(projectId: string, portal = false) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: string) =>
      api<{ id: string; created_at: string }>(messagesBase(projectId, portal), {
        method: "POST",
        body: { body },
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["collab-messages", portal ? "client" : "team", projectId] });
      qc.invalidateQueries({ queryKey: ["collab", projectId] });
      qc.invalidateQueries({ queryKey: ["collab", "client", projectId] });
    },
  });
}

// ── Files ────────────────────────────────────────────────────

function filesBase(projectId: string, portal: boolean) {
  return portal ? `/client/projects/${projectId}/files` : `/projects/${projectId}/files`;
}

export function useClientFiles(projectId: string, portal = false) {
  return useQuery({
    queryKey: ["collab-files", portal ? "client" : "team", projectId],
    queryFn: () => api<{ files: ClientFile[] }>(filesBase(projectId, portal)),
    enabled: !!projectId,
  });
}

/** Share an already-uploaded project file with the client. */
export function useShareProjectFile(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { file_name: string; file_url: string; file_size: number; client_id?: string | null }) =>
      api<{ id: string }>(`/projects/${projectId}/files`, { method: "POST", body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["collab-files", "team", projectId] });
      qc.invalidateQueries({ queryKey: ["collab-files", "client", projectId] });
      qc.invalidateQueries({ queryKey: ["collab", projectId] });
      qc.invalidateQueries({ queryKey: ["collab", "client", projectId] });
    },
  });
}

type UploadTicket = { upload_id: string; url: string; method: string; headers: Record<string, string> };

/** Upload new bytes straight to storage, then record them as a shared file. */
export async function uploadClientFile(
  projectId: string,
  file: File,
  opts: { portal?: boolean; onProgress?: (fraction: number) => void } = {},
): Promise<{ id: string }> {
  const base = opts.portal ? `/client/projects/${projectId}` : `/projects/${projectId}`;
  const ticket = await api<UploadTicket>(`${base}/collab-files/uploads`, {
    method: "POST",
    body: { filename: file.name, size: file.size, content_type: file.type || "application/octet-stream" },
  });
  await new Promise<void>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(ticket.method, ticket.url);
    for (const [k, v] of Object.entries(ticket.headers)) xhr.setRequestHeader(k, v);
    xhr.upload.onprogress = (e) => e.lengthComputable && opts.onProgress?.(e.loaded / e.total);
    xhr.onload = () => (xhr.status >= 200 && xhr.status < 300 ? resolve() : reject(new Error(`Upload failed (${xhr.status})`)));
    xhr.onerror = () => reject(new Error("Upload failed. Check your connection and try again."));
    xhr.send(file);
  });
  return api<{ id: string }>(`${base}/collab-files/uploads/${ticket.upload_id}/complete`, { method: "POST" });
}

export function useUploadClientFile(projectId: string, portal = false) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (file: File) => uploadClientFile(projectId, file, { portal }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["collab-files", portal ? "client" : "team", projectId] });
      qc.invalidateQueries({ queryKey: ["collab", projectId] });
      qc.invalidateQueries({ queryKey: ["collab", "client", projectId] });
    },
  });
}

export function clientFileDownloadUrl(fileId: string) {
  return `/api/client-files/${fileId}/download`;
}

// ── Client portal: one project's collaboration ───────────────

export function useClientCollab(projectId: string) {
  return useQuery({
    queryKey: ["collab", "client", projectId],
    queryFn: () => api<ClientCollab>(`/client/projects/${projectId}/collab`),
    enabled: !!projectId,
  });
}
