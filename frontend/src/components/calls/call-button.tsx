"use client";

import { PhoneIcon } from "lucide-react";
import { useCalls } from "@/lib/calls";
import { useMe } from "@/lib/session";
import { Button } from "@/components/ui/button";

/** Rings someone's browser so you can talk to them. */
export function CallButton({
  userId,
  name,
  disabled,
  variant = "outline",
  size = "sm",
  label = "Call",
}: {
  userId: string;
  name: string;
  disabled?: boolean;
  variant?: "outline" | "ghost" | "default";
  size?: "sm" | "default" | "icon";
  label?: string;
}) {
  const calls = useCalls();
  const { data: me } = useMe();

  if (me?.id === userId) return null;
  const busy = calls.phase !== "idle";

  return (
    <Button
      variant={variant}
      size={size}
      disabled={disabled || busy}
      title={busy ? "You are already on a call" : `Call ${name}`}
      onClick={() => void calls.start(userId, name)}
    >
      <PhoneIcon />
      {size !== "icon" && <span className="max-sm:hidden">{label}</span>}
    </Button>
  );
}
