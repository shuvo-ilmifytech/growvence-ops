"use client";

import { useEffect, useState } from "react";
import { PencilIcon, PlusIcon, StarIcon, TrashIcon } from "lucide-react";
import { useCreateReview, useDeleteReview, useReviewStats, useReviews, useUpdateReview, type Review, type ReviewInput } from "@/lib/reviews";
import { useMe } from "@/lib/session";
import { useProjects } from "@/lib/projects";
import { useUserDirectory } from "@/lib/teams";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

function Stars({ value, className }: { value: number; className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-0.5", className)} aria-label={`${value} out of 5 stars`}>
      {[1, 2, 3, 4, 5].map((i) => (
        <StarIcon key={i} className={cn("size-4", i <= value ? "fill-amber-400 text-amber-400" : "text-muted-foreground/40")} />
      ))}
    </span>
  );
}

function StarInput({ value, onChange }: { value: number; onChange: (v: number) => void }) {
  return (
    <span className="inline-flex items-center gap-1">
      {[1, 2, 3, 4, 5].map((i) => (
        <button key={i} type="button" onClick={() => onChange(i)} aria-label={`${i} star${i > 1 ? "s" : ""}`}>
          <StarIcon className={cn("size-7 transition", i <= value ? "fill-amber-400 text-amber-400" : "text-muted-foreground/40 hover:text-amber-300")} />
        </button>
      ))}
    </span>
  );
}

function ReviewDialog({ open, onOpenChange, initial }: { open: boolean; onOpenChange: (v: boolean) => void; initial?: Review | null }) {
  const create = useCreateReview();
  const update = useUpdateReview();
  const { data: projects } = useProjects({});
  const { data: users } = useUserDirectory();
  const [form, setForm] = useState<ReviewInput>({
    order_id: "",
    rating: 5,
    review_text: "",
    reviewer_name: "",
    reviewed_at: "",
  });

  // Initialize form when dialog opens (client-side only, avoids prerender issues).
  useEffect(() => {
    if (open) {
      setForm({
        order_id: initial?.order_id ?? "",
        project_id: initial?.project_id ?? undefined,
        member_id: initial?.member_id ?? undefined,
        rating: initial?.rating ?? 5,
        review_text: initial?.review_text ?? "",
        reviewer_name: initial?.reviewer_name ?? "",
        reviewed_at: initial?.reviewed_at ?? new Date().toISOString().slice(0, 10),
      });
    }
  }, [open, initial]);

  function submit() {
    if (!form.order_id.trim()) return;
    if (initial) {
      update.mutate({ id: initial.id, input: form }, { onSuccess: () => onOpenChange(false) });
    } else {
      create.mutate(form, { onSuccess: () => onOpenChange(false) });
    }
  }

  const pending = create.isPending || update.isPending;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{initial ? "Edit review" : "Add Fiverr review"}</DialogTitle>
        </DialogHeader>
        <div className="grid gap-4 py-2">
          <div className="grid grid-cols-2 gap-4">
            <div className="grid gap-1.5">
              <Label>Order ID *</Label>
              <Input value={form.order_id} onChange={(e) => setForm({ ...form, order_id: e.target.value })} placeholder="e.g. FO-12345" />
            </div>
            <div className="grid gap-1.5">
              <Label>Date</Label>
              <Input type="date" value={form.reviewed_at} onChange={(e) => setForm({ ...form, reviewed_at: e.target.value })} />
            </div>
          </div>
          <div className="grid gap-1.5">
            <Label>Rating</Label>
            <StarInput value={form.rating} onChange={(v) => setForm({ ...form, rating: v })} />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div className="grid gap-1.5">
              <Label>Project</Label>
              <Select value={form.project_id ?? "none"} onValueChange={(v) => setForm({ ...form, project_id: v === "none" ? undefined : v })}>
                <SelectTrigger><SelectValue placeholder="Select project" /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">No project</SelectItem>
                  {(projects?.projects ?? []).map((p: { id: string; name: string | null }) => (
                    <SelectItem key={p.id} value={p.id}>{p.name ?? "Untitled"}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-1.5">
              <Label>Member (who did the work)</Label>
              <Select value={form.member_id ?? "none"} onValueChange={(v) => setForm({ ...form, member_id: v === "none" ? undefined : v })}>
                <SelectTrigger><SelectValue placeholder="Select member" /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">No member</SelectItem>
                  {(users ?? []).map((u: { id: string; name: string }) => (
                    <SelectItem key={u.id} value={u.id}>{u.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="grid gap-1.5">
            <Label>Client name</Label>
            <Input value={form.reviewer_name} onChange={(e) => setForm({ ...form, reviewer_name: e.target.value })} placeholder="Client's name" />
          </div>
          <div className="grid gap-1.5">
            <Label>Review text</Label>
            <Textarea value={form.review_text} onChange={(e) => setForm({ ...form, review_text: e.target.value })} placeholder="What the client said..." rows={3} />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button onClick={submit} disabled={pending || !form.order_id.trim()}>
            {pending ? "Saving..." : initial ? "Save changes" : "Add review"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function ReviewsPage() {
  const { data: me } = useMe();
  const [ratingFilter, setRatingFilter] = useState(0);
  const [q, setQ] = useState("");
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<Review | null>(null);
  const stats = useReviewStats();
  const list = useReviews({ rating: ratingFilter || undefined, q: q.trim() || undefined });
  const del = useDeleteReview();

  const canManage = me && (["sales", "tl"].includes(me.role) || me.is_admin || me.role === "ceo");
  const s = stats.data;
  const reviews = list.data?.reviews ?? [];

  function openAdd() {
    setEditing(null);
    setDialogOpen(true);
  }
  function openEdit(r: Review) {
    setEditing(r);
    setDialogOpen(true);
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Fiverr Reviews"
        description="Client ratings per order — track who earns the 5 stars."
        actions={canManage ? <Button onClick={openAdd}><PlusIcon /> Add review</Button> : undefined}
      />

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-muted-foreground">Average rating</CardTitle></CardHeader>
          <CardContent>
            {stats.isPending ? <Skeleton className="h-8 w-20" /> : (
              <div className="flex items-center gap-2">
                <span className="text-3xl font-bold">{(s?.avg_rating ?? 0).toFixed(1)}</span>
                <Stars value={Math.round(s?.avg_rating ?? 0)} />
              </div>
            )}
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-muted-foreground">Total reviews</CardTitle></CardHeader>
          <CardContent>
            {stats.isPending ? <Skeleton className="h-8 w-16" /> : <span className="text-3xl font-bold">{s?.total ?? 0}</span>}
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-muted-foreground">5-star rate</CardTitle></CardHeader>
          <CardContent>
            {stats.isPending ? <Skeleton className="h-8 w-16" /> : <span className="text-3xl font-bold">{(s?.five_star_pct ?? 0).toFixed(0)}%</span>}
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-muted-foreground">Star distribution</CardTitle></CardHeader>
          <CardContent className="space-y-1.5">
            {stats.isPending ? <Skeleton className="h-16 w-full" /> : [5, 4, 3, 2, 1].map((star) => {
              const count = s?.by_stars?.[String(star)] ?? 0;
              const pct = s?.total ? (count / s.total) * 100 : 0;
              return (
                <div key={star} className="flex items-center gap-2 text-xs">
                  <span className="w-3 text-muted-foreground">{star}</span>
                  <StarIcon className="size-3 fill-amber-400 text-amber-400" />
                  <div className="h-2 flex-1 overflow-hidden rounded-full bg-muted">
                    <div className="h-full rounded-full bg-amber-400" style={{ width: `${pct}%` }} />
                  </div>
                  <span className="w-8 text-right tabular-nums text-muted-foreground">{count}</span>
                </div>
              );
            })}
          </CardContent>
        </Card>
      </div>

      {/* Top members */}
      {s && s.top_members.length > 0 && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Top rated members</CardTitle></CardHeader>
          <CardContent>
            <div className="flex flex-wrap gap-4">
              {s.top_members.map((m) => (
                <div key={m.name} className="flex items-center gap-2 rounded-lg bg-muted/50 px-3 py-2">
                  <span className="text-sm font-medium">{m.name}</span>
                  <Stars value={Math.round(m.avg_rating)} className="[&_svg]:size-3" />
                  <span className="text-xs text-muted-foreground">({m.count})</span>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Filters */}
      <div className="flex flex-col gap-3 md:flex-row md:items-center">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search order, client or text..." className="md:max-w-xs" />
        <Select value={String(ratingFilter)} onValueChange={(v) => setRatingFilter(Number(v))}>
          <SelectTrigger className="w-full md:w-40"><SelectValue placeholder="All ratings" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="0">All ratings</SelectItem>
            {[5, 4, 3, 2, 1].map((r) => <SelectItem key={r} value={String(r)}>{r} stars</SelectItem>)}
          </SelectContent>
        </Select>
      </div>

      {/* Table */}
      <Card>
        <CardContent className="p-0">
          {list.isPending ? (
            <div className="space-y-2 p-4">{[1, 2, 3].map((i) => <Skeleton key={i} className="h-16 w-full" />)}</div>
          ) : reviews.length === 0 ? (
            <p className="p-8 text-center text-sm text-muted-foreground">No reviews yet. Add the first one!</p>
          ) : (
            <ul className="divide-y">
              {reviews.map((r) => (
                <li key={r.id} className="flex items-start gap-4 p-4">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <Stars value={r.rating} />
                      <span className="text-sm font-medium">{r.order_id}</span>
                      {r.project_name && <span className="text-xs text-muted-foreground">· {r.project_name}</span>}
                    </div>
                    {r.review_text && <p className="mt-1 text-sm text-muted-foreground">"{r.review_text}"</p>}
                    <p className="mt-1 text-xs text-muted-foreground">
                      {r.reviewer_name && <>{r.reviewer_name} · </>}
                      {r.member_name && <>by {r.member_name} · </>}
                      {r.reviewed_at}
                    </p>
                  </div>
                  {canManage && (
                    <div className="flex shrink-0 gap-1">
                      <Button variant="ghost" size="icon" onClick={() => openEdit(r)} aria-label="Edit"><PencilIcon className="size-4" /></Button>
                      <Button variant="ghost" size="icon" disabled={del.isPending} onClick={() => { if (confirm("Delete this review?")) del.mutate(r.id); }} aria-label="Delete"><TrashIcon className="size-4" /></Button>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <ReviewDialog open={dialogOpen} onOpenChange={setDialogOpen} initial={editing} />
    </div>
  );
}
