import { UsersRoundIcon } from "lucide-react";
import { initials } from "@/lib/format";
import { cn } from "@/lib/utils";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";

// A steady colour per name, so a person is recognisable at a glance in the list and
// in a group thread. Light and dark variants keep the initials readable in both.
const TONES = [
  "bg-sky-500/15 text-sky-700 dark:text-sky-300",
  "bg-violet-500/15 text-violet-700 dark:text-violet-300",
  "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
  "bg-amber-500/15 text-amber-700 dark:text-amber-300",
  "bg-rose-500/15 text-rose-700 dark:text-rose-300",
  "bg-teal-500/15 text-teal-700 dark:text-teal-300",
  "bg-indigo-500/15 text-indigo-700 dark:text-indigo-300",
  "bg-orange-500/15 text-orange-700 dark:text-orange-300",
];

export function toneOf(name: string) {
  let h = 0;
  for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) | 0;
  return TONES[Math.abs(h) % TONES.length];
}

/** `small` fits the initials into the little avatars stacked in a group's header. */
export function PersonAvatar({ name, group = false, small = false, className }: { name: string; group?: boolean; small?: boolean; className?: string }) {
  return (
    <Avatar className={cn("size-10", group && "rounded-xl", className)}>
      <AvatarFallback className={cn(small ? "text-[9px]" : "text-xs", "font-semibold", group ? "rounded-xl bg-primary/12 text-primary" : toneOf(name))}>
        {group ? <UsersRoundIcon className="size-[45%]" /> : initials(name)}
      </AvatarFallback>
    </Avatar>
  );
}
