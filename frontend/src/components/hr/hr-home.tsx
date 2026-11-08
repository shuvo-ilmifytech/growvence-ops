"use client";

import Link from "next/link";
import { ArrowRightIcon, CalendarCheck2Icon, ContactIcon, IdCardIcon, InboxIcon, PlaneIcon, type LucideIcon } from "lucide-react";
import { formatDate } from "@/lib/format";
import { useApprovals, useDirectory, useHrMe, useWhosOut } from "@/lib/hr";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

/** Home for the HR role: people, approvals and who's out (HR has no project access). */
export function HrHome({ name }: { name: string }) {
  const me = useHrMe();
  const today = me.data?.today ?? "";
  const approvals = useApprovals();
  const people = useDirectory();
  const out = useWhosOut(today, today);
  const pending = approvals.data ? approvals.data.leave.length + approvals.data.corrections.length : undefined;

  return (
    <div className="space-y-8">
      <div className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Hello, {name.split(" ")[0]}</h1>
        <p className="text-sm text-muted-foreground">Look after people, attendance and leave.</p>
      </div>

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <Stat href="/hr/approvals" label="Waiting for approval" icon={InboxIcon} value={pending} tone="text-amber-600" />
        <Stat href="/hr/team" label="Out today" icon={PlaneIcon} value={out.data?.length} tone="text-sky-600" />
        <Stat href="/hr/people" label="People" icon={ContactIcon} value={people.data?.length} />
        <Stat href="/hr" label="My HR" icon={CalendarCheck2Icon} value={me.data?.today_record ? "In" : me.data ? "—" : undefined} />
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <PlaneIcon className="size-4 text-primary" /> Out today
          </CardTitle>
          <CardDescription>{today && formatDate(today)}</CardDescription>
          <CardAction>
            <Link href="/hr/team" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
              <IdCardIcon className="size-4" /> Team attendance <ArrowRightIcon className="size-4" />
            </Link>
          </CardAction>
        </CardHeader>
        <CardContent>
          {!out.data ? (
            <Skeleton className="h-10 w-full" />
          ) : out.data.length === 0 ? (
            <p className="text-sm text-muted-foreground">Everyone&apos;s in.</p>
          ) : (
            <ul className="space-y-1.5 text-sm">
              {out.data.map((o) => (
                <li key={o.user_id + o.start_date} className="flex justify-between gap-2">
                  <Link href={`/hr/people/${o.user_id}`} className="font-medium hover:underline">
                    {o.user_name}
                  </Link>
                  <span className="text-muted-foreground">
                    {o.leave_type_name}
                    {o.half_day && " (half day)"}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function Stat({ href, label, icon: Icon, value, tone }: { href: string; label: string; icon: LucideIcon; value: number | string | undefined; tone?: string }) {
  return (
    <Link href={href} className="group">
      <Card className="h-full gap-2 transition group-hover:border-primary/40 group-hover:shadow-sm">
        <CardHeader>
          <CardDescription>{label}</CardDescription>
          <CardAction>
            <Icon className={`size-4 text-muted-foreground ${tone ?? ""}`} />
          </CardAction>
        </CardHeader>
        <CardContent>
          {value === undefined ? <Skeleton className="h-9 w-10" /> : <div className="text-3xl font-semibold tabular-nums">{value}</div>}
        </CardContent>
      </Card>
    </Link>
  );
}
