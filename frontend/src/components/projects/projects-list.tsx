"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { ChevronLeftIcon, ChevronRightIcon, FolderKanbanIcon, PlusIcon, SearchIcon } from "lucide-react";
import { useProjects } from "@/lib/projects";
import { can, useMe } from "@/lib/session";
import { formatDate, initials, relativeTime } from "@/lib/format";
import { PRIORITIES, PRIORITY_LABELS, type ProjectStatus } from "@/lib/types";
import { PageHeader } from "@/components/page-header";
import { PriorityLabel, StatusBadge, StoreAccessBadge } from "@/components/project-badges";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";

const VIEWS: { value: string; label: string; statuses: ProjectStatus[] }[] = [
  { value: "active", label: "Active", statuses: ["new", "in_progress", "blocked", "ready_for_review", "ready_to_deliver"] },
  { value: "review", label: "In review", statuses: ["ready_for_review"] },
  { value: "deliver", label: "To deliver", statuses: ["ready_to_deliver"] },
  { value: "delivered", label: "Delivered", statuses: ["delivered"] },
  { value: "completed", label: "Completed", statuses: ["completed"] },
  { value: "cancelled", label: "Cancelled", statuses: ["cancelled"] },
  { value: "all", label: "All", statuses: [] },
];

const SORTS = [
  { value: "activity", label: "Recently updated" },
  { value: "due", label: "Due soonest" },
  { value: "priority", label: "Highest priority" },
  { value: "created", label: "Newest" },
];

const PAGE_SIZE = 25;

export function ProjectsList() {
  const { data: me } = useMe();
  const router = useRouter();
  const pathname = usePathname();
  const sp = useSearchParams();

  const view = sp.get("view") ?? "active";
  const priority = sp.get("priority") ?? "";
  const sort = sp.get("sort") ?? "activity";
  const overdue = sp.get("overdue") === "true";
  const page = Number(sp.get("page") ?? 1);
  const urlQ = sp.get("q") ?? "";
  const [q, setQ] = useState(urlQ);

  function setParams(patch: Record<string, string | null>) {
    const next = new URLSearchParams(sp);
    for (const [k, v] of Object.entries(patch)) {
      if (v === null || v === "") next.delete(k);
      else next.set(k, v);
    }
    if (!("page" in patch)) next.delete("page");
    router.replace(`${pathname}?${next}`, { scroll: false });
  }

  // Debounce search into the URL.
  useEffect(() => {
    if (q === urlQ) return;
    const t = setTimeout(() => setParams({ q }), 300);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q]);

  // An explicit ?status= (e.g. from a dashboard card) overrides the view tabs.
  const statusParam = sp.get("status");
  const statuses = statusParam ? statusParam.split(",") : (VIEWS.find((v) => v.value === view)?.statuses ?? []);
  const list = useProjects({
    status: statuses.join(","),
    q: urlQ,
    priority,
    overdue,
    sort,
    page,
    page_size: PAGE_SIZE,
  });

  const counts = list.data?.counts ?? {};
  const countFor = (s: ProjectStatus[]) =>
    (s.length ? s : (Object.keys(counts) as ProjectStatus[])).reduce((n, st) => n + (counts[st] ?? 0), 0);
  const views = VIEWS;
  const canCreate = can(me, "project.create");
  const pages = Math.max(1, Math.ceil((list.data?.total ?? 0) / PAGE_SIZE));

  return (
    <div className="space-y-6">
      <PageHeader
        title="Projects"
        description={
          me?.role === "member"
            ? "Projects assigned to you."
            : me?.role === "sales"
              ? "Projects you've created."
              : "Every project across the team."
        }
        actions={
          canCreate && (
            <Button asChild>
              <Link href="/projects/new">
                <PlusIcon /> New project
              </Link>
            </Button>
          )
        }
      />

      <Tabs value={statusParam ? "" : view} onValueChange={(v) => setParams({ view: v, status: null })}>
        <TabsList className="flex-wrap">
          {views.map((v) => (
            <TabsTrigger key={v.value} value={v.value}>
              {v.label}
              {list.data && <span className="ml-1.5 text-xs tabular-nums text-muted-foreground">{countFor(v.statuses)}</span>}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>

      <div className="flex flex-wrap items-center gap-3">
        <InputGroup className="w-full sm:w-72">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput placeholder="Search name, client, order ID or #" value={q} onChange={(e) => setQ(e.target.value)} />
        </InputGroup>
        <Select value={priority || "any"} onValueChange={(v) => setParams({ priority: v === "any" ? null : v })}>
          <SelectTrigger className="w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="any">Any priority</SelectItem>
            {PRIORITIES.map((p) => (
              <SelectItem key={p} value={p}>
                {PRIORITY_LABELS[p]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={sort} onValueChange={(v) => setParams({ sort: v === "activity" ? null : v })}>
          <SelectTrigger className="w-44">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {SORTS.map((s) => (
              <SelectItem key={s.value} value={s.value}>
                {s.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="flex items-center gap-2">
          <Switch id="overdue" checked={overdue} onCheckedChange={(v) => setParams({ overdue: v ? "true" : null })} />
          <Label htmlFor="overdue">Overdue only</Label>
        </div>
      </div>

      <Card className="py-0">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="w-16 pl-4">#</TableHead>
              <TableHead>Project</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Priority</TableHead>
              <TableHead>Assignee</TableHead>
              <TableHead>Due</TableHead>
              <TableHead className="pr-4 text-right">Updated</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody className={cn(list.isFetching && list.isPlaceholderData && "opacity-60")}>
            {list.isPending &&
              Array.from({ length: 6 }, (_, i) => (
                <TableRow key={i}>
                  <TableCell className="pl-4"><Skeleton className="h-4 w-8" /></TableCell>
                  <TableCell>
                    <Skeleton className="mb-1.5 h-4 w-48" />
                    <Skeleton className="h-3 w-32" />
                  </TableCell>
                  <TableCell><Skeleton className="h-5 w-24 rounded-full" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                  <TableCell><Skeleton className="h-6 w-28" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                  <TableCell />
                </TableRow>
              ))}
            {list.data?.projects.map((p) => (
              <TableRow key={p.id} className="cursor-pointer" onClick={() => router.push(`/projects/${p.id}`)}>
                <TableCell className="pl-4 font-mono text-xs text-muted-foreground">{p.number}</TableCell>
                <TableCell className="max-w-80">
                  <Link
                    href={`/projects/${p.id}`}
                    className="flex items-center gap-2 truncate font-medium hover:underline"
                    onClick={(e) => e.stopPropagation()}
                  >
                    {p.has_updates && <span className="size-2 shrink-0 rounded-full bg-primary" title="New updates since you last opened it" />}
                    <span className="truncate">{p.name ?? <span className="italic text-muted-foreground">Untitled</span>}</span>
                  </Link>
                  <div className="truncate text-xs text-muted-foreground">
                    {[p.client_name, p.order_id, p.profile_name].filter(Boolean).join(" · ") || "No client yet"}
                  </div>
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap items-center gap-1.5">
                    <StatusBadge status={p.status} />
                    {/* Missing store access is easy to spot in the list; given access needs no badge here. */}
                    {p.store_access !== "given" && p.status !== "cancelled" && p.status !== "completed" && (
                      <StoreAccessBadge access={p.store_access} short />
                    )}
                  </div>
                </TableCell>
                <TableCell>
                  <PriorityLabel priority={p.priority} />
                </TableCell>
                <TableCell>
                  {p.assigned_to_name ? (
                    <span className="flex items-center gap-2 text-sm">
                      <Avatar className="size-6">
                        <AvatarFallback className="bg-accent text-[10px] font-medium text-accent-foreground">
                          {initials(p.assigned_to_name)}
                        </AvatarFallback>
                      </Avatar>
                      {p.assigned_to_name}
                      {p.member_names.length > 1 && <span className="text-xs text-muted-foreground">+{p.member_names.length - 1}</span>}
                    </span>
                  ) : (
                    <span className="text-sm text-muted-foreground">Unassigned</span>
                  )}
                  {p.team_name && <div className="mt-0.5 text-xs text-muted-foreground">{p.team_name}</div>}
                </TableCell>
                <TableCell>
                  <span className={cn("text-sm", p.is_overdue ? "font-medium text-red-600" : "text-muted-foreground")}>
                    {formatDate(p.effective_due)}
                  </span>
                </TableCell>
                <TableCell className="pr-4 text-right text-sm text-muted-foreground">{relativeTime(p.last_activity_at)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>

        {list.isSuccess && list.data.projects.length === 0 && (
          <Empty className="border-t">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <FolderKanbanIcon />
              </EmptyMedia>
              <EmptyTitle>No projects here</EmptyTitle>
              <EmptyDescription>
                {urlQ || priority || overdue ? "Try clearing the filters." : "Nothing in this view yet."}
              </EmptyDescription>
            </EmptyHeader>
            {canCreate && !urlQ && (
              <EmptyContent>
                <Button asChild variant="outline">
                  <Link href="/projects/new">
                    <PlusIcon /> New project
                  </Link>
                </Button>
              </EmptyContent>
            )}
          </Empty>
        )}
        {list.isError && <p className="border-t p-4 text-sm text-destructive">{list.error.message}</p>}

        {list.data && list.data.total > PAGE_SIZE && (
          <div className="flex items-center justify-between border-t px-4 py-3 text-sm text-muted-foreground">
            <span>
              {(page - 1) * PAGE_SIZE + 1}–{Math.min(page * PAGE_SIZE, list.data.total)} of {list.data.total}
            </span>
            <div className="flex gap-2">
              <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setParams({ page: String(page - 1) })}>
                <ChevronLeftIcon /> Previous
              </Button>
              <Button variant="outline" size="sm" disabled={page >= pages} onClick={() => setParams({ page: String(page + 1) })}>
                Next <ChevronRightIcon />
              </Button>
            </div>
          </div>
        )}
      </Card>
    </div>
  );
}
