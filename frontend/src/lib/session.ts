"use client";

import { useSyncExternalStore } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "./api";
import type { Me } from "./types";

const noop = () => () => {};

/** False on the server and during hydration, true afterwards. */
export function useHydrated() {
  return useSyncExternalStore(
    noop,
    () => true,
    () => false,
  );
}

/**
 * The signed-in user. Role-specific UI must render identically on the server and
 * during hydration, so the user only becomes visible once the page has hydrated
 * (otherwise a suspended boundary hydrating after /me resolves would mismatch).
 */
export function useMe() {
  const hydrated = useHydrated();
  const q = useQuery({
    queryKey: ["me"],
    queryFn: () => api<{ user: Me }>("/auth/me").then((r) => r.user),
    staleTime: 60_000,
    retry: false,
  });
  return hydrated ? q : { ...q, data: undefined };
}

export function can(me: Me | undefined, action: string) {
  return !!me?.permissions.includes(action);
}
