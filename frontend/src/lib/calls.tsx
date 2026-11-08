"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, ApiError } from "./api";
import { onLiveEvent } from "./notifications";

/**
 * Voice calls between two browsers (WebRTC). The audio goes straight from one person
 * to the other; the server only carries the introduction — the offer, the answer and
 * the network candidates — and remembers who rang whom.
 *
 * One connection per tab, held here so a call survives moving between pages.
 */

export type Call = {
  id: string;
  caller_id: string;
  caller_name: string;
  callee_id: string;
  callee_name: string;
  status: "ringing" | "answered" | "declined" | "missed" | "ended" | "failed";
  started_at: string;
  answered_at: string | null;
  ended_at: string | null;
  incoming: boolean;
};

type Phase = "idle" | "calling" | "incoming" | "connecting" | "talking";

type CallContext = {
  phase: Phase;
  call: Call | null;
  who: string; // the other person's name
  muted: boolean;
  seconds: number;
  /** The remote side is connected and audio should be flowing. */
  connected: boolean;
  start: (userId: string, name: string) => Promise<void>;
  answer: () => Promise<void>;
  decline: () => Promise<void>;
  hangUp: () => Promise<void>;
  toggleMute: () => void;
};

const Ctx = createContext<CallContext | null>(null);

export function useCalls() {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useCalls needs <CallProvider>");
  return ctx;
}

type Signal = { call_id: string; from: string; kind: "offer" | "answer" | "candidate"; payload: unknown };

/** A repeating two-tone ring, generated rather than loaded from a file. */
function makeRing() {
  let ctx: AudioContext | null = null;
  let timer: ReturnType<typeof setInterval> | null = null;
  const beep = (at: number, freq: number) => {
    if (!ctx) return;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = "sine";
    osc.frequency.value = freq;
    gain.gain.setValueAtTime(0.0001, at);
    gain.gain.exponentialRampToValueAtTime(0.3, at + 0.03);
    gain.gain.exponentialRampToValueAtTime(0.0001, at + 0.4);
    osc.connect(gain).connect(ctx.destination);
    osc.start(at);
    osc.stop(at + 0.42);
  };
  return {
    async start() {
      try {
        type WithWebkit = typeof window & { webkitAudioContext?: typeof AudioContext };
        const Ctor = window.AudioContext ?? (window as WithWebkit).webkitAudioContext;
        if (!Ctor || timer) return;
        ctx ??= new Ctor();
        if (ctx.state === "suspended") await ctx.resume();
        const ring = () => {
          if (!ctx) return;
          beep(ctx.currentTime, 760);
          beep(ctx.currentTime + 0.5, 980);
        };
        ring();
        timer = setInterval(ring, 2500);
      } catch {
        // No sound: the on-screen card still shows the call.
      }
    },
    stop() {
      if (timer) clearInterval(timer);
      timer = null;
    },
  };
}

export function CallProvider({ children, enabled }: { children: React.ReactNode; enabled: boolean }) {
  const qc = useQueryClient();
  const [call, setCall] = useState<Call | null>(null);
  const [phase, setPhase] = useState<Phase>("idle");
  const [muted, setMuted] = useState(false);
  const [connected, setConnected] = useState(false);
  const [seconds, setSeconds] = useState(0);

  const pc = useRef<RTCPeerConnection | null>(null);
  const localStream = useRef<MediaStream | null>(null);
  const audio = useRef<HTMLAudioElement | null>(null);
  const ringer = useRef<ReturnType<typeof makeRing> | null>(null);
  // Signals that arrive before this side is ready to use them.
  const pendingOffer = useRef<RTCSessionDescriptionInit | null>(null);
  const pendingCandidates = useRef<RTCIceCandidateInit[]>([]);
  // The handlers below run at event time, long after render, and need the call as it is
  // then rather than as it was when they were created.
  const currentCall = useRef<Call | null>(null);
  useEffect(() => {
    currentCall.current = call;
  }, [call]);

  const ring = useCallback((on: boolean) => {
    ringer.current ??= makeRing();
    if (on) void ringer.current.start();
    else ringer.current.stop();
  }, []);

  /** Tear everything down, whatever state it was in. */
  const cleanUp = useCallback(() => {
    ring(false);
    pc.current?.close();
    pc.current = null;
    localStream.current?.getTracks().forEach((t) => t.stop());
    localStream.current = null;
    if (audio.current) audio.current.srcObject = null;
    pendingOffer.current = null;
    pendingCandidates.current = [];
    setConnected(false);
    setMuted(false);
    setSeconds(0);
    setPhase("idle");
    setCall(null);
    qc.invalidateQueries({ queryKey: ["calls"] });
  }, [qc, ring]);

  const signal = useCallback(async (callId: string, kind: Signal["kind"], payload: unknown) => {
    try {
      await api(`/calls/${callId}/signal`, { method: "POST", body: { kind, payload } });
    } catch {
      // The other side may already have hung up; the call state event will tell us.
    }
  }, []);

  /** Build the peer connection, with the microphone attached. */
  const connect = useCallback(
    async (callId: string) => {
      const { ice_servers } = await api<{ ice_servers: RTCIceServer[] }>("/calls/ice");
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      localStream.current = stream;
      const peer = new RTCPeerConnection({ iceServers: ice_servers });
      pc.current = peer;
      stream.getTracks().forEach((t) => peer.addTrack(t, stream));
      peer.onicecandidate = (e) => {
        if (e.candidate) void signal(callId, "candidate", e.candidate.toJSON());
      };
      peer.ontrack = (e) => {
        if (audio.current) {
          audio.current.srcObject = e.streams[0];
          void audio.current.play().catch(() => {});
        }
      };
      peer.onconnectionstatechange = () => {
        const state = peer.connectionState;
        if (state === "connected") {
          setConnected(true);
          setPhase("talking");
        } else if (state === "failed") {
          toast.error("The call could not connect. Your networks may need a relay (TURN).");
          void api(`/calls/${callId}/status`, { method: "POST", body: { status: "failed" } }).catch(() => {});
          cleanUp();
        }
      };
      return peer;
    },
    [cleanUp, signal],
  );

  // ── Starting, answering, ending ──────────────────────────

  const start = useCallback(
    async (userId: string, name: string) => {
      if (phase !== "idle") {
        toast.error("You are already on a call");
        return;
      }
      setPhase("calling");
      try {
        const { call: made } = await api<{ call: Call }>(`/calls/${userId}`, { method: "POST" });
        setCall(made);
        const peer = await connect(made.id);
        const offer = await peer.createOffer();
        await peer.setLocalDescription(offer);
        await signal(made.id, "offer", offer);
        ring(true); // the caller hears it ringing out
      } catch (err) {
        const message =
          err instanceof ApiError
            ? err.message
            : err instanceof DOMException
              ? "No microphone. Allow access to call."
              : `Could not call ${name}`;
        toast.error(message);
        const live = currentCall.current;
        if (live) await api(`/calls/${live.id}/status`, { method: "POST", body: { status: "failed" } }).catch(() => {});
        cleanUp();
      }
    },
    [phase, connect, signal, ring, cleanUp],
  );

  const answer = useCallback(async () => {
    const live = currentCall.current;
    if (!live) return;
    ring(false);
    setPhase("connecting");
    try {
      await api(`/calls/${live.id}/status`, { method: "POST", body: { status: "answered" } });
      const peer = await connect(live.id);
      // The caller's offer usually arrives while it is still ringing.
      const offer = pendingOffer.current;
      if (offer) {
        await peer.setRemoteDescription(offer);
        for (const c of pendingCandidates.current) await peer.addIceCandidate(c).catch(() => {});
        pendingCandidates.current = [];
        const reply = await peer.createAnswer();
        await peer.setLocalDescription(reply);
        await signal(live.id, "answer", reply);
      }
    } catch (err) {
      toast.error(err instanceof DOMException ? "No microphone. Allow access to answer." : "Could not answer the call");
      await api(`/calls/${live.id}/status`, { method: "POST", body: { status: "failed" } }).catch(() => {});
      cleanUp();
    }
  }, [connect, signal, ring, cleanUp]);

  const finish = useCallback(
    async (status: "declined" | "ended") => {
      const live = currentCall.current;
      if (live) await api(`/calls/${live.id}/status`, { method: "POST", body: { status } }).catch(() => {});
      cleanUp();
    },
    [cleanUp],
  );

  const decline = useCallback(() => finish("declined"), [finish]);
  const hangUp = useCallback(() => finish("ended"), [finish]);

  const toggleMute = useCallback(() => {
    const tracks = localStream.current?.getAudioTracks() ?? [];
    const next = !muted;
    tracks.forEach((t) => (t.enabled = !next));
    setMuted(next);
  }, [muted]);

  // ── What the other side does ─────────────────────────────

  useEffect(() => {
    if (!enabled) return;
    return onLiveEvent(["call", "call_signal", "call_state"], (data, kind) => {
      if (kind === "call") {
        const c = data as Call;
        const live = currentCall.current;
        if (c.status === "ringing" && c.incoming && !live) {
          setCall(c);
          setPhase("incoming");
          ring(true);
          return;
        }
        if (!live || c.id !== live.id) return;
        setCall(c);
        if (c.status === "answered") {
          ring(false);
          setPhase((p) => (p === "calling" ? "connecting" : p));
        } else if (c.status !== "ringing") {
          // declined, missed, ended or failed
          if (c.status === "declined") toast.info(`${live.incoming ? live.caller_name : live.callee_name} declined`);
          cleanUp();
        }
        return;
      }
      if (kind === "call_state") {
        const s = data as { call_id: string; status: string };
        if (currentCall.current?.id === s.call_id && s.status === "missed") cleanUp();
        return;
      }
      // A WebRTC signal from the other end.
      const s = data as Signal;
      const live = currentCall.current;
      if (!live || s.call_id !== live.id) return;
      const peer = pc.current;
      void (async () => {
        if (s.kind === "offer") {
          const offer = s.payload as RTCSessionDescriptionInit;
          if (!peer) {
            pendingOffer.current = offer; // still ringing: keep it until they answer
            return;
          }
          await peer.setRemoteDescription(offer);
          const reply = await peer.createAnswer();
          await peer.setLocalDescription(reply);
          await signal(live.id, "answer", reply);
        } else if (s.kind === "answer" && peer) {
          await peer.setRemoteDescription(s.payload as RTCSessionDescriptionInit).catch(() => {});
        } else if (s.kind === "candidate") {
          const candidate = s.payload as RTCIceCandidateInit;
          if (peer?.remoteDescription) await peer.addIceCandidate(candidate).catch(() => {});
          else pendingCandidates.current.push(candidate);
        }
      })();
    });
  }, [enabled, ring, cleanUp, signal]);

  // A call this tab missed noticing (a reload mid-call, or a ring that came in before
  // the page was open) is picked up once on mount.
  useEffect(() => {
    if (!enabled) return;
    let live = true;
    api<{ call: Call | null }>("/calls/live")
      .then(({ call: found }) => {
        if (!live || !found || currentCall.current) return;
        // Only an incoming ring can be joined from scratch; a call whose audio was lost
        // with the old page is hung up rather than left hanging.
        if (found.status === "ringing" && found.incoming) {
          setCall(found);
          setPhase("incoming");
          ring(true);
        } else {
          void api(`/calls/${found.id}/status`, { method: "POST", body: { status: "ended" } }).catch(() => {});
        }
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [enabled, ring]);

  // The call timer, once the audio is up.
  useEffect(() => {
    if (!connected) return;
    const t = setInterval(() => setSeconds((s) => s + 1), 1000);
    return () => clearInterval(t);
  }, [connected]);

  // Leaving the page ends the call rather than leaving the other side listening to nothing.
  useEffect(() => {
    if (phase === "idle") return;
    const onLeave = () => {
      const live = currentCall.current;
      if (live) navigator.sendBeacon?.(`/api/calls/${live.id}/status`, new Blob([JSON.stringify({ status: "ended" })], { type: "application/json" }));
    };
    window.addEventListener("pagehide", onLeave);
    return () => window.removeEventListener("pagehide", onLeave);
  }, [phase]);

  const who = call ? (call.incoming ? call.caller_name : call.callee_name) : "";

  const value = useMemo<CallContext>(
    () => ({ phase, call, who, muted, seconds, connected, start, answer, decline, hangUp, toggleMute }),
    [phase, call, who, muted, seconds, connected, start, answer, decline, hangUp, toggleMute],
  );

  return (
    <Ctx.Provider value={value}>
      {children}
      {/* The other person's voice. Hidden, but it has to be in the page to play. */}
      <audio ref={audio} autoPlay playsInline className="hidden" />
    </Ctx.Provider>
  );
}
