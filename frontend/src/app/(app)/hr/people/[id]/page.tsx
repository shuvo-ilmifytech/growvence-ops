import { Suspense } from "react";
import { EmployeeProfile } from "@/components/hr/employee-profile";

export const metadata = { title: "Person" };

export default function PersonPage({ params }: PageProps<"/hr/people/[id]">) {
  return (
    <Suspense>
      <EmployeeProfile params={params} />
    </Suspense>
  );
}
