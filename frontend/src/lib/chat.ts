"use client";

import { useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./api";
import { onLiveEvent } from "./notifications";

export type ChatAttachment = { id: string; name: string; size: number; mime_type: string };

export type ChatMessage = {
  id: string;
  sender_id: string;
  sender_name: string;
  body: string;
  created_at: string;
  read_at: string | null;
  mine: boolean;
  attachments: ChatAttachment[];
};

/** A finished call between the two people in a one-to-one thread. */
export type ChatCall = {
  id: string;
  caller_id: string;
  caller_name: string;
  callee_id: string;
  callee_name: string;
  status: "declined" | "missed" | "ended" | "failed";
  started_at: string;
  answered_at: string | null;
  ended_at: string | null;
  incoming: boolean;
};

/** What the composer sends: text, the upload ids of files already in storage, or both. */
export type OutgoingMessage = { body: string; attachments: string[] };

export type ChatPerson = { id: string; name: string; role: string; is_active: boolean };
export type Thread = { user: ChatPerson; messages: ChatMessage[]; calls: ChatCall[]; has_more: boolean };
export type InboxThread = {
  user_id: string;
  user_name: string;
  user_role: string;
  is_active: boolean;
  last_body: string;
  last_at: string;
  last_from_me: boolean;
  unread: number;
};
export type Inbox = { threads: InboxThread[]; unread: number };

const INBOX = ["messages", "inbox"];
const threadKey = (id: string) => ["messages", "thread", id];

export function useInbox(enabled = true) {
  return useQuery({ queryKey: INBOX, queryFn: () => api<Inbox>("/messages"), enabled, placeholderData: (p) => p });
}

export function useThread(userId: string | null) {
  return useQuery({
    queryKey: threadKey(userId ?? ""),
    queryFn: () => api<Thread>(`/messages/${userId}`),
    enabled: !!userId,
  });
}

/** Where a chat file downloads from (also usable as an image source). */
export const attachmentUrl = (id: string) => `/api/chat/attachments/${id}`;

type Ticket = { upload_id: string; url: string; method: string; headers: Record<string, string> };

/**
 * Puts one file into storage for a chat message and returns its upload id, which the
 * message then names. The message itself is only written when it is sent.
 */
export async function uploadChatFile(file: File, onProgress?: (fraction: number) => void, signal?: AbortSignal): Promise<string> {
  const contentType = file.type || "application/octet-stream";
  const ticket = await api<Ticket>("/chat/uploads", {
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
    signal?.addEventListener("abort", () => {
      xhr.abort();
      reject(new DOMException("Upload cancelled", "AbortError"));
    });
    xhr.send(file);
  });
  return ticket.upload_id;
}

export function useSendMessage(userId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (m: OutgoingMessage) => api<{ message: ChatMessage }>(`/messages/${userId}`, { method: "POST", body: m }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: threadKey(userId) });
      qc.invalidateQueries({ queryKey: INBOX });
    },
  });
}

/** Opening a thread marks it read, and tells the sender it was read. */
export function useMarkThreadRead(userId: string | null, unread: number) {
  const qc = useQueryClient();
  useEffect(() => {
    if (!userId || unread === 0) return;
    let live = true;
    api(`/messages/${userId}/read`, { method: "POST" })
      .then(() => {
        if (live) {
          qc.invalidateQueries({ queryKey: INBOX });
          qc.invalidateQueries({ queryKey: threadKey(userId) });
        }
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [userId, unread, qc]);
}

/**
 * Messages arriving on the live stream refresh the inbox and the open thread; a call
 * that has just finished refreshes the thread it belongs in, so it shows there.
 */
export function useChatStream(enabled: boolean) {
  const qc = useQueryClient();
  useEffect(() => {
    if (!enabled) return;
    const offMessages = onLiveEvent(["message", "message_read"], (e) => {
      const data = e as { from?: string; with?: string };
      qc.invalidateQueries({ queryKey: INBOX });
      for (const id of [data.from, data.with]) if (id) qc.invalidateQueries({ queryKey: threadKey(id) });
    });
    const offCalls = onLiveEvent(["call", "call_state"], (e) => {
      const data = e as { status?: string; caller_id?: string; callee_id?: string };
      if (data.status === "ringing" || data.status === "answered") return;
      if (data.caller_id && data.callee_id) {
        for (const id of [data.caller_id, data.callee_id]) qc.invalidateQueries({ queryKey: threadKey(id) });
      } else {
        qc.invalidateQueries({ queryKey: ["messages", "thread"] });
      }
    });
    return () => {
      offMessages();
      offCalls();
    };
  }, [enabled, qc]);
}

// ── Groups ─────────────────────────────────────────────────

export type Group = {
  id: string;
  name: string;
  created_by: string;
  created_at: string;
  member_count: number;
  last_body: string | null;
  last_at: string | null;
  last_sender_name: string | null;
  last_from_me: boolean;
  unread: number;
  mine: boolean; // started by you, so you rename and remove
};
export type GroupMember = ChatPerson & { added_at: string };
export type GroupThread = { group: Group; members: GroupMember[]; messages: ChatMessage[]; has_more: boolean };

const GROUPS = ["messages", "groups"];
const groupKey = (id: string) => ["messages", "group", id];

export function useGroups(enabled = true) {
  return useQuery({
    queryKey: GROUPS,
    queryFn: () => api<{ groups: Group[] }>("/groups").then((r) => r.groups),
    enabled,
    placeholderData: (p) => p,
  });
}

export function useGroupThread(id: string | null) {
  return useQuery({
    queryKey: groupKey(id ?? ""),
    queryFn: () => api<GroupThread>(`/groups/${id}`),
    enabled: !!id,
  });
}

export function useCreateGroup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { name: string; member_ids: string[] }) => api<{ group: Group }>("/groups", { method: "POST", body: v }),
    onSuccess: () => qc.invalidateQueries({ queryKey: GROUPS }),
  });
}

export function useSendGroupMessage(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (m: OutgoingMessage) => api<{ message: ChatMessage }>(`/groups/${id}`, { method: "POST", body: m }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: groupKey(id) });
      qc.invalidateQueries({ queryKey: GROUPS });
    },
  });
}

export function useRenameGroup(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api(`/groups/${id}`, { method: "PATCH", body: { name } }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: groupKey(id) });
      qc.invalidateQueries({ queryKey: GROUPS });
    },
  });
}

export function useAddGroupMembers(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (user_ids: string[]) => api(`/groups/${id}/members`, { method: "POST", body: { user_ids } }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: groupKey(id) });
      qc.invalidateQueries({ queryKey: GROUPS });
    },
  });
}

/** Removes someone, or yourself: leaving is the same call with your own id. */
export function useRemoveGroupMember(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (userId: string) => api(`/groups/${id}/members/${userId}`, { method: "DELETE" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: groupKey(id) });
      qc.invalidateQueries({ queryKey: GROUPS });
      qc.invalidateQueries({ queryKey: INBOX });
    },
  });
}

/** Opening a group marks it read, for this member only. */
export function useMarkGroupRead(id: string | null, unread: number) {
  const qc = useQueryClient();
  useEffect(() => {
    if (!id || unread === 0) return;
    let live = true;
    api(`/groups/${id}/read`, { method: "POST" })
      .then(() => {
        if (live) {
          qc.invalidateQueries({ queryKey: GROUPS });
          qc.invalidateQueries({ queryKey: INBOX });
        }
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [id, unread, qc]);
}

/** Group messages and roster changes arriving on the live stream. */
export function useGroupStream(enabled: boolean) {
  const qc = useQueryClient();
  useEffect(() => {
    if (!enabled) return;
    return onLiveEvent(["group_message", "group_changed"], (e) => {
      const data = e as { group?: string };
      qc.invalidateQueries({ queryKey: GROUPS });
      qc.invalidateQueries({ queryKey: INBOX });
      if (data.group) qc.invalidateQueries({ queryKey: groupKey(data.group) });
    });
  }, [enabled, qc]);
}
