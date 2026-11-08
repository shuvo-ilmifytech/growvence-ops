"use client";

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "./api";

export type Alert = { kind: string; count: number; label: string; href: string };
export type Alerts = { items: Alert[]; every_seconds: number };

const MUTE_KEY = "opsflow.ring.muted";
const SNOOZE_KEY = "opsflow.ring.snoozed_until";

/** localStorage, but never throwing: private windows and blocked site data are normal. */
function read(key: string) {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key: string, value: string | null) {
  try {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    // Nothing to do: the alert still shows, it just won't remember the choice.
  }
  for (const l of listeners) l();
}

// Mute and snooze live in localStorage, read through useSyncExternalStore so the
// prerender sees nothing and every open tab (and this one) stays in step.
const listeners = new Set<() => void>();

function subscribe(cb: () => void) {
  listeners.add(cb);
  window.addEventListener("storage", cb);
  return () => {
    listeners.delete(cb);
    window.removeEventListener("storage", cb);
  };
}

function useStored(key: string) {
  return useSyncExternalStore(
    subscribe,
    () => read(key),
    () => null,
  );
}

// A once-a-second clock, read the same way. The snapshot must be a cached value:
// returning a fresh Date.now() on every read makes React re-render for ever.
// The prerender gets 0 rather than a timestamp baked into the HTML, and the timer
// only runs while a snooze is counting down.
let clockValue = 0;
const clockListeners = new Set<() => void>();
let clockTimer: ReturnType<typeof setInterval> | null = null;

const noClock = () => () => {};

function subscribeClock(cb: () => void) {
  clockListeners.add(cb);
  clockValue = Date.now();
  clockTimer ??= setInterval(() => {
    clockValue = Date.now();
    for (const l of clockListeners) l();
  }, 1000);
  return () => {
    clockListeners.delete(cb);
    if (clockListeners.size === 0 && clockTimer) {
      clearInterval(clockTimer);
      clockTimer = null;
    }
  };
}

function useNow(ticking: boolean) {
  return useSyncExternalStore(
    ticking ? subscribeClock : noClock,
    () => clockValue,
    () => 0,
  );
}

/**
 * chime plays a short double beep through the Web Audio API, so there's no audio file
 * to ship and nothing to load. Browsers refuse to play before the person has clicked
 * something, so play() reports whether the sound was actually allowed.
 */
function makeChime() {
  let ctx: AudioContext | null = null;
  return async (): Promise<boolean> => {
    try {
      type WithWebkit = typeof window & { webkitAudioContext?: typeof AudioContext };
      const Ctor = window.AudioContext ?? (window as WithWebkit).webkitAudioContext;
      if (!Ctor) return false;
      ctx ??= new Ctor();
      if (ctx.state === "suspended") await ctx.resume();
      if (ctx.state !== "running") return false;
      const now = ctx.currentTime;
      // Two short notes, a fifth apart: audible without being a siren.
      for (const [at, freq] of [
        [0, 880],
        [0.18, 1320],
      ] as const) {
        const osc = ctx.createOscillator();
        const gain = ctx.createGain();
        osc.type = "sine";
        osc.frequency.value = freq;
        gain.gain.setValueAtTime(0.0001, now + at);
        gain.gain.exponentialRampToValueAtTime(0.25, now + at + 0.02);
        gain.gain.exponentialRampToValueAtTime(0.0001, now + at + 0.16);
        osc.connect(gain).connect(ctx.destination);
        osc.start(now + at);
        osc.stop(now + at + 0.18);
      }
      return true;
    } catch {
      return false;
    }
  };
}

/**
 * useRing watches what is waiting for this person and chimes while anything is.
 * It stops the moment the queue empties, and remembers mute and snooze per browser.
 */
export function useRing(enabled: boolean) {
  const alerts = useQuery({
    queryKey: ["alerts"],
    queryFn: () => api<Alerts>("/alerts"),
    enabled,
    refetchInterval: 20_000,
    refetchOnWindowFocus: true,
  });

  const muted = useStored(MUTE_KEY) === "1";
  const snoozedUntil = Number(useStored(SNOOZE_KEY) ?? 0);
  const [blocked, setBlocked] = useState(false); // the browser refused to play
  const now = useNow(snoozedUntil > 0);
  const chime = useRef<ReturnType<typeof makeChime> | null>(null);

  const items = alerts.data?.items ?? [];
  const total = items.reduce((n, i) => n + i.count, 0);
  const every = (alerts.data?.every_seconds ?? 30) * 1000;
  // Before the clock has been read (first render, or a prerender) a stored snooze
  // counts as still running: err on the side of silence, never a stray chime.
  const snoozed = snoozedUntil > 0 && (now === 0 || snoozedUntil > now);
  const ringing = total > 0 && !muted && !snoozed;

  // The chime itself: once straight away, then every `every` while anything waits.
  useEffect(() => {
    if (!ringing) return;
    let live = true;
    chime.current ??= makeChime();
    const play = async () => {
      const ok = await chime.current?.();
      if (live) setBlocked(!ok);
    };
    void play();
    const t = setInterval(play, every);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, [ringing, every, total]);

  // A click anywhere is the gesture browsers want before they allow sound.
  useEffect(() => {
    if (!blocked) return;
    const onClick = () => {
      void chime.current?.().then((ok) => setBlocked(!ok));
    };
    window.addEventListener("pointerdown", onClick, { once: true });
    return () => window.removeEventListener("pointerdown", onClick);
  }, [blocked]);

  // The tab's title says what's waiting, so a background tab still shows it.
  useEffect(() => {
    const base = document.title.replace(/^\(\d+\)\s*/, "");
    document.title = total > 0 ? `(${total}) ${base}` : base;
    return () => {
      document.title = document.title.replace(/^\(\d+\)\s*/, "");
    };
  }, [total]);

  const mute = useCallback((on: boolean) => write(MUTE_KEY, on ? "1" : null), []);

  const snooze = useCallback((minutes: number) => write(SNOOZE_KEY, String(Date.now() + minutes * 60_000)), []);

  return {
    items,
    total,
    muted,
    snoozed,
    snoozeMinutesLeft: snoozed ? Math.ceil((snoozedUntil - now) / 60_000) : 0,
    soundBlocked: blocked && !muted && !snoozed && total > 0,
    mute,
    snooze,
  };
}
