"use client";

import { usePathname } from "next/navigation";
import { ShieldAlertIcon } from "lucide-react";
import { useMe } from "@/lib/session";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";

// UX only: the API rejects non-admins regardless.
export default function AdminLayout({ children }: { children: React.ReactNode }) {
  const { data: me } = useMe();
  const path = usePathname();
  // People pages: HR + CEO. System setup: admins + CEO.
  const people = path.startsWith("/admin/users") || path.startsWith("/admin/hr") || path.startsWith("/admin/teams");
  const allowed = !!me && (people ? me.role === "hr" || me.role === "ceo" : me.is_admin || me.role === "ceo");
  if (me && !allowed) {
    return (
      <Empty className="min-h-[60vh]">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ShieldAlertIcon />
          </EmptyMedia>
          <EmptyTitle>{people ? "HR and the CEO only" : "Admins only"}</EmptyTitle>
          <EmptyDescription>
            {people
              ? "Accounts, roles, teams and HR settings are managed by HR and the CEO."
              : "You need admin access to manage profiles, project types and settings. Ask an admin if you need a change."}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }
  return children;
}
