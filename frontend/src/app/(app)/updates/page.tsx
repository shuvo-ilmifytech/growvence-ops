import { Suspense } from "react";
import { UpdatesTable } from "@/components/updates/updates-table";

export const metadata = { title: "Updates" };

export default function UpdatesPage() {
  return (
    <Suspense>
      <UpdatesTable />
    </Suspense>
  );
}
