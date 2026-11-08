import { Suspense } from "react";
import { AnalyticsView } from "@/components/analytics/analytics-view";

export const metadata = { title: "Analytics" };

export default function AnalyticsPage() {
  return (
    <Suspense>
      <AnalyticsView />
    </Suspense>
  );
}
