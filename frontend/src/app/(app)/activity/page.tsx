import { Suspense } from "react";
import { ActivityFeed } from "@/components/reports/activity-feed";

export const metadata = { title: "Activity" };

export default function ActivityPage() {
  return (
    <Suspense>
      <ActivityFeed />
    </Suspense>
  );
}
