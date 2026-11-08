import { Suspense } from "react";
import { KbArticleView } from "@/components/kb/article-view";

export const metadata = { title: "Article" };

export default function KbArticlePage({ params }: PageProps<"/kb/[slug]">) {
  return (
    <Suspense>
      <KbArticleView params={params} />
    </Suspense>
  );
}
