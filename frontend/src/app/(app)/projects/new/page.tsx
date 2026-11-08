"use client";

import Link from "next/link";
import { ArrowLeftIcon } from "lucide-react";
import { can, useMe } from "@/lib/session";
import { PageHeader } from "@/components/page-header";
import { ProjectForm } from "@/components/projects/project-form";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";

export default function NewProjectPage() {
  const { data: me } = useMe();
  if (me && !can(me, "project.create")) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>Only Sales can create projects</EmptyTitle>
          <EmptyDescription>Projects start with Sales and come to the Team Leader once submitted.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }
  return (
    <div className="space-y-6">
      <Link href="/projects" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeftIcon className="size-4" /> Projects
      </Link>
      <PageHeader title="New project" description="Save a draft any time. Submit when the required fields are filled in." />
      <ProjectForm />
    </div>
  );
}
