"use client";

import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useCreateKbArticle, useUpdateKbArticle, type KbArticle } from "@/lib/kb";

const DEFAULT_CATEGORIES = ["general", "sop", "training", "process", "policy", "faq"];

export function ArticleDialog({
  open,
  onOpenChange,
  article,
  categories,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  article?: KbArticle | null;
  categories: string[];
}) {
  const [title, setTitle] = useState("");
  const [category, setCategory] = useState("general");
  const [customCategory, setCustomCategory] = useState("");
  const [content, setContent] = useState("");
  const create = useCreateKbArticle();
  const update = useUpdateKbArticle();
  const pending = create.isPending || update.isPending;

  const allCategories = [...new Set([...DEFAULT_CATEGORIES, ...categories])].sort();

  useEffect(() => {
    if (open) {
      setTitle(article?.title ?? "");
      setCategory(article?.category ?? "general");
      setCustomCategory("");
      setContent(article?.content ?? "");
      create.reset();
      update.reset();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, article?.id]);

  const finalCategory = category === "__custom" ? customCategory.trim() : category;

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!title.trim() || !content.trim() || !finalCategory) return;
    const input = { title: title.trim(), content: content.trim(), category: finalCategory };
    if (article) {
      update.mutate(
        { id: article.id, ...input },
        { onSuccess: () => onOpenChange(false) },
      );
    } else {
      create.mutate(input, { onSuccess: () => onOpenChange(false) });
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>{article ? "Edit article" : "New article"}</DialogTitle>
          <DialogDescription>
            Write in Markdown. Use headings, lists, and code blocks to keep SOPs scannable.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-[1fr_200px]">
            <div className="space-y-2">
              <Label htmlFor="kb-title">Title</Label>
              <Input
                id="kb-title"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="e.g. How to handle a Fiverr revision request"
                required
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="kb-category">Category</Label>
              <Select value={category} onValueChange={setCategory}>
                <SelectTrigger id="kb-category">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {allCategories.map((c) => (
                    <SelectItem key={c} value={c}>
                      {c}
                    </SelectItem>
                  ))}
                  <SelectItem value="__custom">New category…</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
          {category === "__custom" && (
            <div className="space-y-2">
              <Label htmlFor="kb-custom-category">New category name</Label>
              <Input
                id="kb-custom-category"
                value={customCategory}
                onChange={(e) => setCustomCategory(e.target.value)}
                placeholder="e.g. onboarding"
                required
              />
            </div>
          )}
          <div className="space-y-2">
            <Label htmlFor="kb-content">Content (Markdown)</Label>
            <Textarea
              id="kb-content"
              value={content}
              onChange={(e) => setContent(e.target.value)}
              placeholder={"# Heading\n\nWrite the process step by step...\n\n- Step one\n- Step two"}
              className="min-h-64 font-mono text-sm"
              required
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={pending || !title.trim() || !content.trim() || !finalCategory}>
              {pending ? "Saving…" : article ? "Save changes" : "Create article"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
