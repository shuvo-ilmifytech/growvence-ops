"use client";

import Link from "next/link";
import { use } from "react";
import { ArrowLeftIcon } from "lucide-react";
import { useProject } from "@/lib/projects";
import { PageHeader } from "@/components/page-header";
import { ProjectForm } from "@/components/projects/project-form";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";

export function EditProject({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const detail = useProject(id);

  if (detail.isPending) return <Skeleton className="h-96 w-full" />;
  if (detail.isError) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>Project not found</EmptyTitle>
          <EmptyDescription>{detail.error.message}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }
  const p = detail.data.project;
  if (!detail.data.permissions.can_edit) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>You can&apos;t edit this project</EmptyTitle>
          <EmptyDescription>Only the Sales owner can edit details, until the project is completed or cancelled.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }
  return (
    <div className="space-y-6">
      <Link href={`/projects/${id}`} className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeftIcon className="size-4" /> Back to project
      </Link>
      <PageHeader
        title={`Edit #${p.number}`}
        description="The Team Leader and the people working on it will see what changed."
      />
      {/* key: remount with fresh values if the project changes underneath */}
      <ProjectForm key={p.lock_version} initial={detail.data} />
    </div>
  );
}
