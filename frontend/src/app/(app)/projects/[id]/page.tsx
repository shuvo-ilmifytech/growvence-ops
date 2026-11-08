import { Suspense } from "react";
import { ProjectView } from "@/components/projects/project-view";

export const metadata = { title: "Project" };

export default function ProjectPage({ params }: PageProps<"/projects/[id]">) {
  return (
    <Suspense>
      <ProjectView params={params} />
    </Suspense>
  );
}
