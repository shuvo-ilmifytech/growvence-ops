"use client";

import { useEffect } from "react";

/** Registers the PWA service worker. Production only: dev reloads and SW caches don't mix. */
export function ServiceWorkerRegister() {
  useEffect(() => {
    if (process.env.NODE_ENV !== "production") return;
    if (!("serviceWorker" in navigator)) return;
    navigator.serviceWorker.register("/sw.js").catch(() => {
      // Offline/PWA is a nice-to-have; a failed registration must never break the app.
    });
  }, []);
  return null;
}
