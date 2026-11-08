import { Suspense } from "react";
import { ClientDetail } from "@/components/clients/client-detail";
import { Skeleton } from "@/components/ui/skeleton";

export const metadata = { title: "Client" };

export default function ClientPage({ params }: PageProps<"/clients/[id]">) {
  return (
    <Suspense fallback={<Skeleton className="h-64" />}>
      <ClientDetail params={params} />
    </Suspense>
  );
}
