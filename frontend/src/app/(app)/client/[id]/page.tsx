import { Suspense } from "react";
import { ClientProjectView } from "@/components/client/project-view";
import { Skeleton } from "@/components/ui/skeleton";

export const metadata = { title: "Project" };

export default function ClientProjectPage({ params }: PageProps<"/client/[id]">) {
  return (
    <Suspense fallback={<Skeleton className="h-64" />}>
      <ClientProjectView params={params} />
    </Suspense>
  );
}
