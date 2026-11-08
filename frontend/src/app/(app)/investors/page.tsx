import { Suspense } from "react";
import { InvestorsDashboard } from "@/components/investors/investors-dashboard";
import { InvestorExpensesSection } from "@/components/investors/investor-expenses-section";
import { Skeleton } from "@/components/ui/skeleton";

export const metadata = { title: "Investors" };

export default function InvestorsPage() {
  return (
    <Suspense fallback={<Skeleton className="h-64" />}>
      <div className="space-y-6">
        <InvestorsDashboard />
        <InvestorExpensesSection />
      </div>
    </Suspense>
  );
}
