"use client";

import Link from "next/link";
import { BriefcaseIcon, CalendarIcon } from "lucide-react";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { useClientProjects } from "@/lib/client";
import { cn } from "@/lib/utils";

const STATUS_TONE: Record<string, string> = {
  in_progress: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200",
  blocked: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200",
  ready_for_review: "bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200",
  completed: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200",
};

const STATUS_LABEL: Record<string, string> = {
  submitted: "Submitted",
  needs_info: "Needs info",
  assigned: "Assigned",
  in_progress: "In progress",
  blocked: "Blocked",
  ready_for_review: "In review",
  completed: "Completed",
};

export default function ClientPortalPage() {
  const { data, isPending, isError } = useClientProjects();
  const projects = data?.projects ?? [];

  return (
    <div className="space-y-6">
      <PageHeader
        title="My Projects"
        description="Track the progress of your projects and share feedback with our team."
      />

      {isPending && (
        <div className="grid gap-4 md:grid-cols-2">
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-40" />
          ))}
        </div>
      )}

      {isError && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>Couldn't load your projects</EmptyTitle>
            <EmptyDescription>Please try refreshing the page.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {!isPending && !isError && projects.length === 0 && (
        <Empty className="min-h-[40vh]">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <BriefcaseIcon />
            </EmptyMedia>
            <EmptyTitle>No projects yet</EmptyTitle>
            <EmptyDescription>Your projects will appear here once our team grants you access.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      <div className="grid gap-4 md:grid-cols-2">
        {projects.map((p) => (
          <Link key={p.id} href={`/client/${p.id}`}>
            <Card className="transition hover:shadow-md">
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between gap-2">
                  <CardTitle className="text-base">
                    #{p.number} {p.name}
                  </CardTitle>
                  <Badge className={cn(STATUS_TONE[p.status] ?? "bg-muted text-muted-foreground")}>
                    {STATUS_LABEL[p.status] ?? p.status}
                  </Badge>
                </div>
              </CardHeader>
              <CardContent>
                <div className="flex items-center gap-4 text-sm text-muted-foreground">
                  {p.due_date && (
                    <span className="flex items-center gap-1.5">
                      <CalendarIcon className="size-4" />
                      Due {p.due_date}
                    </span>
                  )}
                  <span className="capitalize">{p.priority} priority</span>
                </div>
              </CardContent>
            </Card>
          </Link>
        ))}
      </div>
    </div>
  );
}
