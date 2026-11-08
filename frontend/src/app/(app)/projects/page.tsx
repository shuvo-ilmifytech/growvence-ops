import { Suspense } from "react";
import { ProjectsList } from "@/components/projects/projects-list";

export const metadata = { title: "Projects" };

// Filters live in the URL (useSearchParams), which is request data: keep it behind Suspense.
export default function ProjectsPage() {
  return (
    <Suspense>
      <ProjectsList />
    </Suspense>
  );
}
