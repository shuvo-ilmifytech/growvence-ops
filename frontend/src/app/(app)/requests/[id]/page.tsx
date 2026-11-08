import { Suspense } from "react";
import { RequestView } from "@/components/requests/request-view";

export const metadata = { title: "Client request" };

export default function RequestPage({ params }: PageProps<"/requests/[id]">) {
  return (
    <Suspense>
      <RequestView params={params} />
    </Suspense>
  );
}
