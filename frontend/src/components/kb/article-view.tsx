"use client";

import { use, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import ReactMarkdown from "react-markdown";
import { ArrowLeftIcon, PencilIcon, TrashIcon } from "lucide-react";
import { PageHeader } from "@/components/page-header";
import { ArticleDialog } from "@/components/kb/article-dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useDeleteKbArticle, useKbArticle, useKbArticles, canEditKb } from "@/lib/kb";
import { useMe } from "@/lib/session";
import { relativeTime } from "@/lib/format";

export function KbArticleView({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = use(params);
  const router = useRouter();
  const { data: me } = useMe();
  const article = useKbArticle(slug);
  const categories = useKbArticles().data?.categories ?? [];
  const del = useDeleteKbArticle();
  const [editOpen, setEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);

  const editable = canEditKb(me?.role, me?.is_admin);
  const a = article.data;

  function onDelete() {
    if (!a) return;
    del.mutate(a.id, {
      onSuccess: () => {
        setDeleteOpen(false);
        router.push("/kb");
      },
    });
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <Button variant="ghost" size="sm" asChild>
          <Link href="/kb">
            <ArrowLeftIcon /> Knowledge base
          </Link>
        </Button>
        {editable && a && (
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => setEditOpen(true)}>
              <PencilIcon /> Edit
            </Button>
            <Button variant="outline" size="sm" onClick={() => setDeleteOpen(true)}>
              <TrashIcon /> Delete
            </Button>
          </div>
        )}
      </div>

      {article.isPending ? (
        <div className="space-y-3">
          <Skeleton className="h-8 w-2/3" />
          <Skeleton className="h-4 w-1/3" />
          <Skeleton className="h-64 w-full" />
        </div>
      ) : !a ? (
        <p className="text-sm text-muted-foreground">Article not found.</p>
      ) : (
        <>
          <PageHeader
            title={a.title}
            description={`${a.category}${a.author_name ? ` · by ${a.author_name}` : ""} · updated ${relativeTime(a.updated_at)}`}
          />
          <article className="prose prose-sm max-w-none dark:prose-invert">
            <ReactMarkdown>{a.content}</ReactMarkdown>
          </article>
        </>
      )}

      {a && (
        <ArticleDialog
          open={editOpen}
          onOpenChange={setEditOpen}
          article={a}
          categories={categories}
        />
      )}

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete this article?</AlertDialogTitle>
            <AlertDialogDescription>
              “{a?.title}” will be permanently deleted. This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={onDelete} disabled={del.isPending}>
              {del.isPending ? "Deleting…" : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
