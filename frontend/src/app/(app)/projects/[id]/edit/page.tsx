import { Suspense } from "react";
import { EditProject } from "@/components/projects/edit-project";

export const metadata = { title: "Edit project" };

export default function EditProjectPage({ params }: PageProps<"/projects/[id]/edit">) {
  return (
    <Suspense>
      <EditProject params={params} />
    </Suspense>
  );
}
