"use client";

import Link from "next/link";
import { useMemo } from "react";
import { ArrowRightIcon, PlaneIcon } from "lucide-react";
import { useWhosOut } from "@/lib/hr";
import { formatDate } from "@/lib/format";
import { useHydrated } from "@/lib/session";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";

function day(d: Date) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/** Approved leave for the next week, so nobody is handed work while they're away. */
export function WhosOut({ days = 7 }: { days?: number }) {
  // Read today in the browser, so this card doesn't pin a date into the prerender.
  const hydrated = useHydrated();
  const { from, to } = useMemo(() => {
    if (!hydrated) return { from: "", to: "" };
    const today = new Date();
    return { from: day(today), to: day(new Date(today.getTime() + days * 864e5)) };
  }, [hydrated, days]);
  const { data, isPending } = useWhosOut(from, to);
  const out = data ?? [];

  return (
    <Card>
      <CardHeader>
        <CardTitle>Who&rsquo;s out</CardTitle>
        <CardDescription>{to ? `Approved leave between today and ${formatDate(to)}.` : "Approved leave over the next week."}</CardDescription>
        <CardAction>
          <Button variant="ghost" size="sm" asChild>
            <Link href="/hr/team">
              Attendance <ArrowRightIcon />
            </Link>
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        {isPending || !to ? (
          <div className="space-y-2">
            {Array.from({ length: 2 }, (_, i) => (
              <Skeleton key={i} className="h-10" />
            ))}
          </div>
        ) : out.length === 0 ? (
          <Empty className="border border-dashed py-8">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <PlaneIcon />
              </EmptyMedia>
              <EmptyTitle className="text-sm">Everyone&rsquo;s in</EmptyTitle>
              <EmptyDescription>No approved leave in the next week.</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <ul className="-mx-2 divide-y">
            {out.map((o) => (
              <li key={`${o.user_id}-${o.start_date}`}>
                <Link href={`/hr/people/${o.user_id}`} className="flex items-center gap-3 rounded-md px-2 py-2 hover:bg-muted/60">
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-medium">{o.user_name}</div>
                    <div className="truncate text-xs text-muted-foreground">
                      {o.leave_type_name}
                      {o.half_day && " · half day"}
                    </div>
                  </div>
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {o.start_date === o.end_date ? formatDate(o.start_date) : `${formatDate(o.start_date)} – ${formatDate(o.end_date)}`}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
