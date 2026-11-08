import { Suspense } from "react";
import { InvestorDetail } from "@/components/investors/investor-detail";
import { Skeleton } from "@/components/ui/skeleton";

export const metadata = { title: "Investor" };

export default function InvestorPage({ params }: PageProps<"/investors/[id]">) {
  return (
    <Suspense fallback={<Skeleton className="h-64" />}>
      <InvestorDetail params={params} />
    </Suspense>
  );
}
