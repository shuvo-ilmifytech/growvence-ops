"use client";

import Link from "next/link";
import { BellOffIcon, BellRingIcon, ClockIcon, Volume2Icon } from "lucide-react";
import { useRing } from "@/lib/ring";
import { Button } from "@/components/ui/button";

/**
 * The card that goes with the chime: what's waiting, a way to it, and a way to quieten it.
 * It floats over the page instead of sitting in the layout, because it appears while
 * someone may already be reaching for a button — nothing underneath should move. Bottom
 * left, so it never covers the toasts on the right.
 */
export function RingBanner({ enabled }: { enabled: boolean }) {
  const ring = useRing(enabled);

  if (!enabled || ring.total === 0) return null;
  const [first, ...rest] = ring.items;

  return (
    <div
      role="status"
      aria-live="polite"
      className="fixed bottom-4 left-4 z-50 flex max-w-[min(26rem,calc(100vw-2rem))] flex-col gap-2 rounded-lg border border-amber-500/50 bg-amber-50 p-3 text-sm shadow-lg dark:bg-amber-950/90"
    >
      <div className="flex items-start gap-2">
        {ring.muted || ring.snoozed ? (
          <BellOffIcon className="mt-0.5 size-4 shrink-0 text-amber-600" />
        ) : (
          <BellRingIcon className="mt-0.5 size-4 shrink-0 animate-pulse text-amber-600" />
        )}
        <span className="min-w-0">
          <Link href={first.href} className="font-medium hover:underline">
            {first.count} {first.label}
          </Link>
          {rest.map((a) => (
            <span key={a.kind}>
              <span className="text-muted-foreground"> · </span>
              <Link href={a.href} className="font-medium hover:underline">
                {a.count} {a.label}
              </Link>
            </span>
          ))}
          {ring.snoozed && <span className="text-muted-foreground"> · quiet for {ring.snoozeMinutesLeft} more min</span>}
        </span>
      </div>

      <div className="flex flex-wrap items-center gap-1">
        {ring.soundBlocked && (
          <span className="mr-1 flex items-center gap-1 text-xs text-muted-foreground">
            <Volume2Icon className="size-3.5" /> click anywhere to allow sound
          </span>
        )}
        <Button size="sm" asChild>
          <Link href={first.href}>Open</Link>
        </Button>
        {!ring.snoozed && !ring.muted && (
          <Button size="sm" variant="ghost" onClick={() => ring.snooze(15)}>
            <ClockIcon /> Snooze 15 min
          </Button>
        )}
        <Button
          size="sm"
          variant="ghost"
          aria-pressed={ring.muted}
          onClick={() => ring.mute(!ring.muted)}
          title={ring.muted ? "Turn the sound back on" : "Silence the sound on this computer"}
        >
          {ring.muted ? <Volume2Icon /> : <BellOffIcon />}
          <span className="hidden sm:inline">{ring.muted ? "Unmute" : "Mute"}</span>
        </Button>
      </div>
    </div>
  );
}
