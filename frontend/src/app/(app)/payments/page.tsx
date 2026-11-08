import { Suspense } from "react";
import { PaymentsDashboard } from "@/components/payments/payments-dashboard";
import { Skeleton } from "@/components/ui/skeleton";

export const metadata = { title: "Payments" };

export default function PaymentsPage() {
  return (
    <Suspense fallback={<Skeleton className="h-64" />}>
      <PaymentsDashboard />
    </Suspense>
  );
}
