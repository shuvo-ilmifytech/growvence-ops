import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { ROLE_LABELS, type Role } from "@/lib/types";

const TONES: Record<Role, string> = {
  sales: "bg-sky-50 text-sky-700 ring-sky-600/15",
  tl: "bg-orange-50 text-orange-700 ring-orange-600/20",
  member: "bg-emerald-50 text-emerald-700 ring-emerald-600/15",
  ceo: "bg-violet-50 text-violet-700 ring-violet-600/15",
  hr: "bg-pink-50 text-pink-700 ring-pink-600/15",
  office_admin: "bg-amber-50 text-amber-700 ring-amber-600/15",
  client: "bg-teal-50 text-teal-700 ring-teal-600/15",
  investor: "bg-lime-50 text-lime-700 ring-lime-600/15",
};

export function RoleBadge({ role, className }: { role: Role; className?: string }) {
  return (
    <Badge variant="outline" className={cn("border-0 ring-1 ring-inset", TONES[role], className)}>
      {ROLE_LABELS[role]}
    </Badge>
  );
}
