import { Suspense } from "react";
import { InvestorHome } from "@/components/investors/investor-home";
import { Skeleton } from "@/components/ui/skeleton";

export const metadata = { title: "My Share" };

export default function InvestorHomePage() {
  return (
    <Suspense fallback={<Skeleton className="h-64" />}>
      <InvestorHome />
    </Suspense>
  );
}
