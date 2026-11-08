import { Suspense } from "react";
import { ReportsView } from "@/components/reports/reports-view";

export const metadata = { title: "Reports" };

export default function ReportsPage() {
  return (
    <Suspense>
      <ReportsView />
    </Suspense>
  );
}
