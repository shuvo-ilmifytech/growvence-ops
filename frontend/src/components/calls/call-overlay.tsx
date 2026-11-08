"use client";

import { MicIcon, MicOffIcon, PhoneIcon, PhoneOffIcon } from "lucide-react";
import { useCalls } from "@/lib/calls";
import { initials } from "@/lib/format";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";

function clock(seconds: number) {
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}:${String(s).padStart(2, "0")}`;
}

/**
 * The call itself: a ringing card when someone calls you, and a small bar while you
 * talk. It floats top centre so it's visible on any page, and it is the only place a
 * call can be answered or ended.
 */
export function CallOverlay() {
  const { phase, who, muted, seconds, connected, answer, decline, hangUp, toggleMute } = useCalls();
  if (phase === "idle") return null;

  const incoming = phase === "incoming";
  const state = incoming
    ? "is calling you"
    : phase === "calling"
      ? "ringing…"
      : connected
        ? clock(seconds)
        : "connecting…";

  return (
    <div
      role="dialog"
      aria-live="assertive"
      aria-label={incoming ? `${who} is calling you` : `On a call with ${who}`}
      className="fixed top-4 left-1/2 z-50 w-[min(24rem,calc(100vw-2rem))] -translate-x-1/2 rounded-xl border bg-background p-4 shadow-xl"
    >
      <div className="flex items-center gap-3">
        <Avatar className={incoming ? "size-10 animate-pulse" : "size-10"}>
          <AvatarFallback>{initials(who)}</AvatarFallback>
        </Avatar>
        <div className="min-w-0 flex-1">
          <div className="truncate font-medium">{who}</div>
          <div className="text-sm text-muted-foreground tabular-nums">{state}</div>
        </div>
      </div>

      <div className="mt-4 flex items-center justify-end gap-2">
        {incoming ? (
          <>
            <Button variant="outline" onClick={() => void decline()}>
              <PhoneOffIcon /> Decline
            </Button>
            <Button onClick={() => void answer()}>
              <PhoneIcon /> Answer
            </Button>
          </>
        ) : (
          <>
            <Button variant="outline" aria-pressed={muted} onClick={toggleMute} disabled={!connected}>
              {muted ? <MicOffIcon /> : <MicIcon />}
              {muted ? "Unmute" : "Mute"}
            </Button>
            <Button variant="destructive" onClick={() => void hangUp()}>
              <PhoneOffIcon /> Hang up
            </Button>
          </>
        )}
      </div>
    </div>
  );
}
