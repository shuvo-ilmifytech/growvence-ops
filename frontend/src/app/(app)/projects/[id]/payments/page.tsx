import { Suspense } from "react";
import { ProjectPayments } from "@/components/projects/project-payments";
import { Skeleton } from "@/components/ui/skeleton";

export const metadata = { title: "Project payments" };

export default function ProjectPaymentsPage({ params }: PageProps<"/projects/[id]/payments">) {
  return (
    <Suspense fallback={<Skeleton className="h-64" />}>
      <ProjectPayments params={params} />
    </Suspense>
  );
}
