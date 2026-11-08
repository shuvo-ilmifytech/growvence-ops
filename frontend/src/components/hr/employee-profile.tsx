"use client";

import Link from "next/link";
import { use, useState } from "react";
import {
  ArrowLeftIcon,
  BriefcaseIcon,
  Building2Icon,
  CalendarIcon,
  ContactIcon,
  HashIcon,
  LockIcon,
  MailIcon,
  MessageSquareIcon,
  PencilIcon,
  PhoneIcon,
  TimerIcon,
  UserRoundIcon,
  type LucideIcon,
} from "lucide-react";
import { formatDate, initials } from "@/lib/format";
import { useMe } from "@/lib/session";
import {
  days,
  EMPLOYMENT_LABELS,
  monthOf,
  remaining,
  useEmployee,
  useHrMe,
  useSheet,
  type EmployeeView,
} from "@/lib/hr";
import { RoleBadge } from "@/components/role-badge";
import { CallButton } from "@/components/calls/call-button";
import { LeaveStatusBadge } from "@/components/hr/hr-badges";
import { EmployeeDialog, AllocationDialog, PersonalDialog } from "@/components/hr/employee-dialogs";
import { MonthCalendar, MonthNav, SummaryLine } from "@/components/hr/month-calendar";
import { leaveDates } from "@/components/hr/my-hr";
import { SalaryCard } from "@/components/hr/salary-card";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export function EmployeeProfile({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const view = useEmployee(id);
  const { data: me } = useMe();
  const [dialog, setDialog] = useState<"edit" | "personal" | "allocations" | null>(null);
  const hrToday = useHrMe().data?.today;

  if (view.isError) {
    return (
      <Empty className="min-h-[50vh]">
        <EmptyHeader>
          <EmptyTitle>Person not found</EmptyTitle>
          <EmptyDescription>{view.error.message}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }
  if (!view.data) return <Skeleton className="h-96 w-full" />;
  const v = view.data;
  const e = v.employee;
  const isSelf = me?.id === e.user_id;

  return (
    <div className="space-y-6">
      <Button variant="ghost" size="sm" asChild className="-ml-2">
        <Link href="/hr/people">
          <ArrowLeftIcon /> People
        </Link>
      </Button>

      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-4">
          <Avatar className="size-14">
            <AvatarFallback className="bg-accent text-lg font-medium text-accent-foreground">{initials(e.name)}</AvatarFallback>
          </Avatar>
          <div className="space-y-1">
            <h1 className="flex items-center gap-2 text-2xl font-semibold tracking-tight">
              {e.name}
              {!e.is_active && <span className="text-sm font-normal text-muted-foreground">(inactive)</span>}
            </h1>
            <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
              {e.designation && <span>{e.designation}</span>}
              <RoleBadge role={e.role} />
            </div>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          {!isSelf && e.is_active && (
            <>
              <Button variant="outline" asChild>
                <Link href={`/messages?with=${e.user_id}`}>
                  <MessageSquareIcon /> Message
                </Link>
              </Button>
              <CallButton userId={e.user_id} name={e.name} />
            </>
          )}
          {isSelf && (
            <Button variant="outline" onClick={() => setDialog("personal")}>
              <PencilIcon /> Edit my contact details
            </Button>
          )}
          {v.can_edit && (
            <>
              <Button variant="outline" onClick={() => setDialog("allocations")}>
                Leave entitlements
              </Button>
              <Button onClick={() => setDialog("edit")}>
                <PencilIcon /> Edit HR record
              </Button>
            </>
          )}
        </div>
      </div>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>Details</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2">
              <Item icon={MailIcon} label="Email" value={e.email} />
              <Item icon={PhoneIcon} label="Phone" value={e.phone} />
              <Item icon={HashIcon} label="Employee ID" value={e.employee_code} />
              <Item icon={Building2Icon} label="Department" value={e.department_name} />
              <Item
                icon={UserRoundIcon}
                label="Reports to"
                value={e.manager_id && <Link className="hover:underline" href={`/hr/people/${e.manager_id}`}>{e.manager_name}</Link>}
              />
              <Item icon={BriefcaseIcon} label="Employment" value={EMPLOYMENT_LABELS[e.employment_type]} />
              <Item icon={TimerIcon} label="Shift" value={e.shift_name ?? "Default office hours"} />
              <Item icon={CalendarIcon} label="Joined" value={e.join_date && formatDate(e.join_date)} />
            </dl>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <LockIcon className="size-4 text-muted-foreground" /> Private
            </CardTitle>
            <CardDescription>Visible to {isSelf ? "you" : e.name}, HR and the CEO.</CardDescription>
          </CardHeader>
          <CardContent>
            {e.restricted ? (
              <p className="text-sm text-muted-foreground">You don&apos;t have access to these details.</p>
            ) : (
              <dl className="grid gap-4">
                <Item icon={CalendarIcon} label="Date of birth" value={e.date_of_birth && formatDate(e.date_of_birth)} />
                <Item icon={HashIcon} label="National ID" value={e.national_id} />
                <Item icon={ContactIcon} label="Address" value={e.address} />
                <Item
                  icon={PhoneIcon}
                  label="Emergency contact"
                  value={e.emergency_contact_name && `${e.emergency_contact_name}${e.emergency_contact_phone ? ` · ${e.emergency_contact_phone}` : ""}`}
                />
              </dl>
            )}
          </CardContent>
        </Card>
      </div>

      {v.reports.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Direct reports</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-wrap gap-2">
            {v.reports.map((r) => (
              <Link key={r.user_id} href={`/hr/people/${r.user_id}`} className="flex items-center gap-2 rounded-lg border px-3 py-2 text-sm hover:bg-muted">
                <Avatar className="size-6">
                  <AvatarFallback className="bg-accent text-[10px]">{initials(r.name)}</AvatarFallback>
                </Avatar>
                <span className="font-medium">{r.name}</span>
                {r.designation && <span className="text-muted-foreground">· {r.designation}</span>}
              </Link>
            ))}
          </CardContent>
        </Card>
      )}

      {v.can_edit && !isSelf && hrToday && <SalaryCard userId={e.user_id} name={e.name} today={hrToday} />}

      {v.can_see_hr && <HrSection view={v} />}

      <EmployeeDialog open={dialog === "edit"} onOpenChange={(o) => !o && setDialog(null)} employee={e} />
      <PersonalDialog open={dialog === "personal"} onOpenChange={(o) => !o && setDialog(null)} employee={e} />
      <AllocationDialog open={dialog === "allocations"} onOpenChange={(o) => !o && setDialog(null)} employee={e} balances={v.balances} />
    </div>
  );
}

function Item({ icon: Icon, label, value }: { icon: LucideIcon; label: string; value: React.ReactNode }) {
  return (
    <div className="flex gap-3">
      <Icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
      <div className="min-w-0">
        <dt className="text-xs text-muted-foreground">{label}</dt>
        <dd className="text-sm break-words">{value || "—"}</dd>
      </div>
    </div>
  );
}

function HrSection({ view }: { view: EmployeeView }) {
  const hrMe = useHrMe();
  const [month, setMonth] = useState<string | null>(null);
  const shown = month ?? monthOf(hrMe.data?.today ?? "");
  const sheet = useSheet({ month: shown, user_id: view.employee.user_id });
  const person = sheet.data?.people[0];

  return (
    <>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {view.balances.map((b) => (
          <Card key={b.leave_type_id} className="gap-1 py-4">
            <CardContent className="px-4">
              <div className="text-sm font-medium">{b.leave_type_name}</div>
              <div className="text-xl font-semibold tabular-nums">
                {b.is_limited ? (
                  <>
                    {days(remaining(b))}
                    <span className="text-sm font-normal text-muted-foreground"> / {days(b.entitled)} left</span>
                  </>
                ) : (
                  <>
                    {days(b.approved)}
                    <span className="text-sm font-normal text-muted-foreground"> taken</span>
                  </>
                )}
              </div>
              {b.is_override && <p className="text-xs text-muted-foreground">Custom entitlement</p>}
            </CardContent>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader className="flex flex-wrap items-center justify-between gap-2">
          <CardTitle>Attendance</CardTitle>
          {shown && <MonthNav month={shown} onChange={setMonth} />}
        </CardHeader>
        <CardContent className="space-y-4">
          {sheet.data && person ? (
            <>
              <SummaryLine person={person} />
              <MonthCalendar sheet={sheet.data} person={person} />
            </>
          ) : (
            <Skeleton className="h-72 w-full" />
          )}
        </CardContent>
      </Card>

      <Card className="pb-0">
        <CardHeader>
          <CardTitle>Leave history</CardTitle>
        </CardHeader>
        {view.leave.length === 0 ? (
          <p className="border-t px-6 py-4 text-sm text-muted-foreground">No leave requests yet.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="pl-6">Dates</TableHead>
                <TableHead>Type</TableHead>
                <TableHead className="text-right">Days</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="hidden pr-6 md:table-cell">Decided by</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {view.leave.map((l) => (
                <TableRow key={l.id}>
                  <TableCell className="pl-6 font-medium">{leaveDates(l)}</TableCell>
                  <TableCell>{l.leave_type_name}</TableCell>
                  <TableCell className="text-right tabular-nums">{days(l.days)}</TableCell>
                  <TableCell>
                    <LeaveStatusBadge status={l.status} />
                  </TableCell>
                  <TableCell className="hidden pr-6 text-sm text-muted-foreground md:table-cell">
                    {[l.manager_reviewed_by_name, l.reviewed_by_name].filter(Boolean).join(" → ") || "—"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>
    </>
  );
}
