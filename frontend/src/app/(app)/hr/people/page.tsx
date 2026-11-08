"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { SearchIcon, UsersIcon } from "lucide-react";
import { initials } from "@/lib/format";
import { useDepartments, useDirectory } from "@/lib/hr";
import { PageHeader } from "@/components/page-header";
import { RoleBadge } from "@/components/role-badge";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

const ALL = "all";

export default function PeoplePage() {
  const people = useDirectory();
  const departments = useDepartments();
  const [search, setSearch] = useState("");
  const [dept, setDept] = useState(ALL);

  const rows = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (people.data ?? []).filter(
      (p) =>
        (dept === ALL || p.department_id === dept) &&
        (!q || [p.name, p.email, p.designation, p.employee_code ?? ""].some((v) => v.toLowerCase().includes(q))),
    );
  }, [people.data, search, dept]);

  return (
    <div className="space-y-6">
      <PageHeader title="People" description="Everyone at Growvence Tech: who they are, who they report to and who's out today." />

      <div className="flex flex-col gap-3 md:flex-row md:items-center">
        <InputGroup className="md:w-72">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput placeholder="Search name, email, designation" value={search} onChange={(e) => setSearch(e.target.value)} />
        </InputGroup>
        {(departments.data?.length ?? 0) > 0 && (
          <Select value={dept} onValueChange={setDept}>
            <SelectTrigger className="md:w-48">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>All departments</SelectItem>
              {departments.data!.map((d) => (
                <SelectItem key={d.id} value={d.id}>
                  {d.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </div>

      <Card className="py-0">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-4">Person</TableHead>
              <TableHead className="hidden md:table-cell">Department</TableHead>
              <TableHead className="hidden lg:table-cell">Reports to</TableHead>
              <TableHead className="hidden xl:table-cell">Shift</TableHead>
              <TableHead>Role</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {people.isPending &&
              Array.from({ length: 5 }, (_, i) => (
                <TableRow key={i}>
                  <TableCell className="pl-4" colSpan={5}>
                    <Skeleton className="h-9 w-64" />
                  </TableCell>
                </TableRow>
              ))}
            {rows.map((p) => (
              <TableRow key={p.user_id}>
                <TableCell className="pl-4">
                  <Link href={`/hr/people/${p.user_id}`} className="flex items-center gap-3">
                    <Avatar className="size-9">
                      <AvatarFallback className="bg-accent text-xs font-medium text-accent-foreground">{initials(p.name)}</AvatarFallback>
                    </Avatar>
                    <div className="min-w-0">
                      <div className="flex items-center gap-2 font-medium hover:underline">
                        {p.name}
                        {p.out_today && (
                          <Badge variant="outline" className="border-sky-300 text-sky-700 dark:text-sky-300">
                            Out today
                          </Badge>
                        )}
                      </div>
                      <div className="truncate text-xs text-muted-foreground">{p.designation || p.email}</div>
                    </div>
                  </Link>
                </TableCell>
                <TableCell className="hidden text-sm md:table-cell">{p.department_name ?? "—"}</TableCell>
                <TableCell className="hidden text-sm lg:table-cell">{p.manager_name ?? "—"}</TableCell>
                <TableCell className="hidden text-sm xl:table-cell">{p.shift_name ?? "Office hours"}</TableCell>
                <TableCell>
                  <RoleBadge role={p.role} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {people.isSuccess && rows.length === 0 && (
          <Empty className="border-t">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <UsersIcon />
              </EmptyMedia>
              <EmptyTitle>No one found</EmptyTitle>
              <EmptyDescription>Try a different search or department.</EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
      </Card>
    </div>
  );
}
