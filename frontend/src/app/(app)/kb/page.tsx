"use client";

import { useState } from "react";
import Link from "next/link";
import { BookOpenIcon, PlusIcon, SearchIcon } from "lucide-react";
import { PageHeader } from "@/components/page-header";
import { ArticleDialog } from "@/components/kb/article-dialog";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { useKbArticles, canEditKb } from "@/lib/kb";
import { useMe } from "@/lib/session";
import { relativeTime } from "@/lib/format";

const ALL = "all";

export default function KbPage() {
  const { data: me } = useMe();
  const [q, setQ] = useState("");
  const [debouncedQ, setDebouncedQ] = useState("");
  const [category, setCategory] = useState(ALL);
  const [dialogOpen, setDialogOpen] = useState(false);

  const list = useKbArticles({
    category: category === ALL ? undefined : category,
    search: debouncedQ.trim() || undefined,
  });

  const editable = canEditKb(me?.role, me?.is_admin);
  const categories = list.data?.categories ?? [];

  return (
    <div className="space-y-6">
      <PageHeader
        title="Knowledge base"
        description="SOPs, processes and training material — searchable by the whole team."
        actions={
          editable && (
            <Button onClick={() => setDialogOpen(true)}>
              <PlusIcon /> New article
            </Button>
          )
        }
      />

      <div className="flex flex-col gap-3 md:flex-row md:items-center">
        <InputGroup className="md:max-w-xs">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput
            value={q}
            onChange={(e) => {
              setQ(e.target.value);
              const v = e.target.value;
              clearTimeout((window as unknown as { __kbT?: number }).__kbT);
              (window as unknown as { __kbT?: number }).__kbT = window.setTimeout(
                () => setDebouncedQ(v),
                300,
              );
            }}
            placeholder="Search articles…"
            aria-label="Search articles"
          />
        </InputGroup>
        <Select value={category} onValueChange={setCategory}>
          <SelectTrigger className="w-full md:w-48" aria-label="Category">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>All categories</SelectItem>
            {categories.map((c) => (
              <SelectItem key={c} value={c}>
                {c}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {list.isPending ? (
        <div className="space-y-3">
          {Array.from({ length: 5 }, (_, i) => (
            <Skeleton key={i} className="h-20 w-full" />
          ))}
        </div>
      ) : (list.data?.articles.length ?? 0) === 0 ? (
        <Empty className="min-h-[40vh]">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <BookOpenIcon />
            </EmptyMedia>
            <EmptyTitle>No articles yet</EmptyTitle>
            <EmptyDescription>
              {editable
                ? "Create the first SOP or training article for the team."
                : "Nothing published here yet — check back soon."}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <ul className="divide-y rounded-lg border">
          {list.data!.articles.map((a) => (
            <li key={a.id}>
              <Link
                href={`/kb/${encodeURIComponent(a.slug)}`}
                className="flex items-center gap-4 px-4 py-4 transition hover:bg-muted/60"
              >
                <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                  <BookOpenIcon className="size-5" />
                </div>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">{a.title}</p>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    {a.category}
                    {a.author_name ? ` · by ${a.author_name}` : ""} · updated{" "}
                    {relativeTime(a.updated_at)}
                  </p>
                </div>
              </Link>
            </li>
          ))}
        </ul>
      )}

      <ArticleDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        categories={categories}
      />
    </div>
  );
}
