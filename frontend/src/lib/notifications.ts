"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "./api";

export type AppNotification = {
  id: number;
  recipient_id: string;
  type: string;
  project_id: string | null;
  update_id: string | null;
  title: string;
  body: string;
  data: { link?: string; lines?: string[]; count?: number };
  is_important: boolean;
  read_at: string | null;
  created_at: string;
};

type ListResponse = { notifications: AppNotification[]; unread_count: number };

const LIST_KEY = ["notifications", "list"];
const COUNT_KEY = ["notifications", "unread"];

export function useNotifications(enabled: boolean) {
  return useQuery({
    queryKey: LIST_KEY,
    queryFn: () => api<ListResponse>("/notifications?limit=40"),
    enabled,
    staleTime: 30_000,
  });
}

export function useUnreadCount() {
  return useQuery({
    queryKey: COUNT_KEY,
    queryFn: () => api<{ unread_count: number }>("/notifications/unread-count").then((r) => r.unread_count),
    staleTime: Infinity, // kept fresh by the stream
  });
}

export function useMarkRead() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number | "all") =>
      api<{ unread_count: number }>(id === "all" ? "/notifications/read-all" : `/notifications/${id}/read`, { method: "POST" }),
    onSuccess: (r, id) => {
      qc.setQueryData(COUNT_KEY, r.unread_count);
      qc.setQueryData<ListResponse>(LIST_KEY, (old) =>
        old && {
          ...old,
          unread_count: r.unread_count,
          notifications: old.notifications.map((n) =>
            id === "all" || n.id === id ? { ...n, read_at: n.read_at ?? new Date().toISOString() } : n,
          ),
        },
      );
    },
  });
}

/**
 * Chat messages and the back-and-forth of a voice call arrive on the same stream as
 * notifications. They are handed to whoever is interested through this little bus, so
 * there is still exactly one connection per tab.
 */
export type LiveKind = "message" | "message_read" | "group_message" | "group_changed" | "call" | "call_signal" | "call_state";

const LIVE_KINDS: LiveKind[] = ["message", "message_read", "group_message", "group_changed", "call", "call_signal", "call_state"];
const liveBus = typeof window === "undefined" ? null : new EventTarget();

export function onLiveEvent(kinds: LiveKind[], fn: (data: unknown, kind: LiveKind) => void) {
  if (!liveBus) return () => {};
  const handlers = kinds.map((kind) => {
    const handler = (e: Event) => fn((e as CustomEvent).detail, kind);
    liveBus.addEventListener(kind, handler);
    return () => liveBus.removeEventListener(kind, handler);
  });
  return () => handlers.forEach((off) => off());
}

/**
 * One Server-Sent Events connection per tab. Keeps the unread count and list
 * current, toasts new notifications, and refreshes the data they refer to.
 * EventSource reconnects on its own after network drops.
 */
export function useNotificationStream(enabled: boolean) {
  const qc = useQueryClient();
  const router = useRouter();

  useEffect(() => {
    if (!enabled) return;
    const es = new EventSource("/api/notifications/stream");

    es.addEventListener("ready", (e) => {
      const { unread_count } = JSON.parse((e as MessageEvent).data);
      qc.setQueryData(COUNT_KEY, unread_count);
      // After a reconnect we may have missed some: refetch the list lazily.
      qc.invalidateQueries({ queryKey: LIST_KEY });
    });

    es.addEventListener("notification", (e) => {
      const { notification: n, unread_count } = JSON.parse((e as MessageEvent).data) as {
        notification: AppNotification;
        unread_count: number;
      };
      qc.setQueryData(COUNT_KEY, unread_count);
      qc.setQueryData<ListResponse>(LIST_KEY, (old) =>
        old && { unread_count, notifications: [n, ...old.notifications.filter((x) => x.id !== n.id)] },
      );
      // Whatever it's about has changed: refresh it.
      qc.invalidateQueries({ queryKey: ["projects"] });
      qc.invalidateQueries({ queryKey: ["updates"] });
      if (n.type.startsWith("hr.")) qc.invalidateQueries({ queryKey: ["hr"] });
      if (n.type.startsWith("invoice.")) qc.invalidateQueries({ queryKey: ["finance"] });

      const firstLine = n.body.split("\n").at(-1);
      const show = n.is_important ? toast.warning : toast;
      show(n.title, {
        id: `n-${n.id}`, // a batch update replaces its earlier toast
        description: firstLine && firstLine.length > 140 ? firstLine.slice(0, 137) + "…" : firstLine,
        action: n.data?.link ? { label: "Open", onClick: () => router.push(n.data.link!) } : undefined,
      });
    });

    for (const kind of LIVE_KINDS) {
      es.addEventListener(kind, (e) => {
        let detail: unknown = null;
        try {
          detail = JSON.parse((e as MessageEvent).data);
        } catch {
          return;
        }
        liveBus?.dispatchEvent(new CustomEvent(kind, { detail }));
      });
    }

    return () => es.close();
  }, [enabled, qc, router]);
}
